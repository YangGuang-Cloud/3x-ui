#!/usr/bin/python3 -I
"""Root-owned deployment entry point for the managed 3x-ui installation."""
import copy
import fcntl
import hashlib
import json
import os
import re
import shutil
import socket
import sqlite3
import ssl
import subprocess
import sys
import tarfile
import tempfile
import time
import urllib.request
from contextlib import contextmanager
from pathlib import Path


CORE_VERSION = '26.10.10'
PROBE_URLS = ('https://example.com/', 'https://www.cloudflare.com/cdn-cgi/trace')


def loopback_port():
    with socket.socket() as listener:
        listener.bind(('127.0.0.1', 0))
        return listener.getsockname()[1]


def isolated_config(config, directory):
    """Clone the supported production topology without touching its listeners."""
    supported = {'log', 'routing', 'inbounds', 'outbounds', 'policy', 'api', 'stats', 'metrics', 'dns', 'xuiClientRateLimits'}
    if set(config) - supported:
        raise RuntimeError('Unreviewed core configuration features; isolated validation required')
    result = copy.deepcopy(config)
    result['log'] = {'loglevel': 'warning', 'access': 'none', 'error': str(directory/'core.log')}
    api_tag = config.get('api', {}).get('tag')
    for inbound in result.get('inbounds', []):
        if inbound.get('protocol') != 'vless' and not (api_tag and inbound.get('tag') == api_tag):
            raise RuntimeError('Unreviewed inbound; cannot guarantee isolated listeners')
        if inbound.get('streamSettings', {}).get('sockopt', {}).get('tproxy') not in (None, '', 'off'):
            raise RuntimeError('Transparent proxy requires separate staging')
        inbound['listen'], inbound['port'] = '127.0.0.1', loopback_port()
        # Do not redirect arbitrary DNS tunnel listeners implicitly.
    if 'listen' in result.get('dns', {}):
        raise RuntimeError('DNS listener requires separate staging')
    if result.get('metrics'):
        result['metrics']['listen'] = '127.0.0.1:'+str(loopback_port())
    return result


def probe_client(config):
    """Use actual VLESS credentials and pinned production certificates privately."""
    result = {'log': {'loglevel': 'none'}, 'inbounds': [], 'outbounds': [], 'routing': {'rules': []}}
    ports = []
    for inbound in config.get('inbounds', []):
        if inbound.get('protocol') != 'vless':
            continue
        stream = inbound.get('streamSettings', {})
        network, security = stream.get('network', 'tcp'), stream.get('security', 'none')
        if network not in ('tcp', 'raw', 'ws') or security not in ('tls', 'none'):
            raise RuntimeError('Transport requires a reviewed staging probe')
        clients = inbound.get('settings', {}).get('clients', [])
        if not clients:
            raise RuntimeError('Cannot verify inbound without a client')
        client = clients[0]
        outbound_stream = {'network': network, 'security': security}
        if network == 'ws':
            outbound_stream['wsSettings'] = copy.deepcopy(stream.get('wsSettings', {}))
        if security == 'tls':
            certificates = stream.get('tlsSettings', {}).get('certificates', [])
            if not certificates:
                raise RuntimeError('TLS probe requires a production certificate')
            cert = certificates[0]
            pem = Path(cert['certificateFile']).read_text() if cert.get('certificateFile') else '\n'.join(cert.get('certificate', []))
            match = re.search(r'-----BEGIN CERTIFICATE-----.*?-----END CERTIFICATE-----', pem, re.S)
            if not match:
                raise RuntimeError('Cannot load TLS certificate for probe')
            fingerprint = hashlib.sha256(ssl.PEM_cert_to_DER_cert(match.group(0))).hexdigest()
            outbound_stream['tlsSettings'] = {'pinnedPeerCertSha256': fingerprint}
        tag, port = 'verify-'+str(len(ports)), loopback_port()
        ports.append(port)
        result['inbounds'].append({'tag': tag, 'listen': '127.0.0.1', 'port': port, 'protocol': 'socks', 'settings': {'auth': 'noauth'}})
        result['outbounds'].append({'tag': tag, 'protocol': 'vless', 'settings': {'vnext': [{'address': '127.0.0.1', 'port': inbound['port'], 'users': [{'id': client['id'], 'encryption': 'none', 'flow': client.get('flow', '')}]}]}, 'streamSettings': outbound_stream})
        result['routing']['rules'].append({'type': 'field', 'inboundTag': [tag], 'outboundTag': tag})
    if not ports:
        raise RuntimeError('No VLESS inbound was verified')
    return result, ports


@contextmanager
def temporary_core(core, config, directory, name, environment):
    path = directory/(name+'.json')
    path.write_text(json.dumps(config))
    path.chmod(0o600)
    with open(directory/(name+'.stderr'), 'wb') as log:
        process = subprocess.Popen([str(core), 'run', '-c', str(path)], env=environment, stdout=log, stderr=log)
        try:
            yield process
        finally:
            if process.poll() is None:
                process.terminate()
                try:
                    process.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait(timeout=5)


def core_environment(shared_bin, rates_path):
    environment = dict(os.environ, XRAY_LOCATION_ASSET=str(shared_bin), XUI_CLIENT_RATE_LIMITS_FILE=str(rates_path))
    return environment


def verify_forwarding(core, config, directory, environment, urls=PROBE_URLS):
    client, ports = probe_client(config)
    with temporary_core(core, client, directory, 'probe-client', environment) as process:
        for port in ports:
            deadline = time.monotonic()+10
            while True:
                if process.poll() is not None:
                    raise RuntimeError('Probe client failed to start; see private staging logs')
                try:
                    with socket.create_connection(('127.0.0.1', port), timeout=.2):
                        break
                except OSError:
                    if time.monotonic() >= deadline:
                        raise RuntimeError('Probe client did not listen')
                    time.sleep(.1)
            for url in urls:
                checked = subprocess.run(['curl', '--noproxy', '', '--socks5-hostname', '127.0.0.1:'+str(port), '--connect-timeout', '5', '--max-time', '12', '--silent', '--fail', '--output', '/dev/null', url], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
                if checked.returncode == 0:
                    break
            else:
                raise RuntimeError('Actual proxy request failed; release must not be accepted')
    return len(ports)


def preflight(candidate, config_path, shared_bin, directory, urls=PROBE_URLS):
    """No service stop, runtime switch or production configuration writes here."""
    body = config_path.read_bytes()
    original = json.loads(body)
    rates = Path(str(config_path)+'.rates.json')
    staged_rates = directory/'rates.json'
    rates_body = rates.read_bytes() if rates.exists() else None
    staged_rates.write_bytes(rates_body if rates_body is not None else b'{}')
    staged_rates.chmod(0o600)
    environment = core_environment(shared_bin, staged_rates)
    core = candidate/'xray-linux-amd64'
    with open(directory/'config-test.log', 'wb') as log:
        subprocess.run([str(core), 'run', '-test', '-c', str(config_path)], env=environment, stdout=log, stderr=log, timeout=30, check=True)
    isolated = isolated_config(original, directory)
    with temporary_core(core, isolated, directory, 'candidate-server', environment) as process:
        for inbound in isolated['inbounds']:
            deadline = time.monotonic()+10
            while True:
                if process.poll() is not None:
                    raise RuntimeError('Isolated candidate failed to start; production was not stopped')
                try:
                    with socket.create_connection(('127.0.0.1', inbound['port']), timeout=.2):
                        break
                except OSError:
                    if time.monotonic() >= deadline:
                        raise RuntimeError('Isolated candidate failed to listen')
                    time.sleep(.1)
        count = verify_forwarding(core, isolated, directory, environment, urls)
    if config_path.read_bytes() != body or (rates.read_bytes() if rates.exists() else None) != rates_body:
        raise RuntimeError('Live configuration changed during validation; retry before cutover')
    print('Isolated production-config validation passed: '+str(count)+' VLESS inbounds', flush=True)
    return original


def extract_release(archive, destination, commit):
    destination.mkdir(mode=0o755, parents=True, exist_ok=True)
    with tarfile.open(fileobj=archive, mode='r:gz') as bundle:
        members = bundle.getmembers()
        names = {m.name for m in members}
        if len(names) != len(members) or names not in ({'x-ui', 'manifest.json'}, {'x-ui', 'manifest.json', 'xray-linux-amd64'}):
            raise ValueError('Unexpected archive member')
        for member in members:
            limit = 256 * 1024 * 1024 if member.name in ('x-ui', 'xray-linux-amd64') else 4096
            if not member.isfile() or not 0 < member.size <= limit:
                raise ValueError('Invalid archive member type or size')
            with bundle.extractfile(member) as source, open(destination/member.name, 'xb') as target:
                shutil.copyfileobj(source, target)
            (destination/member.name).chmod(0o755 if member.name in ('x-ui', 'xray-linux-amd64') else 0o644)
    manifest = json.loads((destination/'manifest.json').read_text())
    if manifest.get('commit') != commit or manifest.get('coreVersion') != CORE_VERSION:
        raise ValueError('Release manifest mismatch')
    for name in ('x-ui', 'xray-linux-amd64'):
        if name == 'xray-linux-amd64' and name not in names:
            continue
        with open(destination/name, 'rb') as binary:
            header = binary.read(20)
        if len(header) != 20 or header[:5] != b'\x7fELF\x02' or header[18:20] != b'\x3e\x00':
            raise ValueError('Expected Linux amd64 executable')



def prepare_asset_links(release, shared_bin):
    """A relocated core must still find shared geodata, including on rollback."""
    for asset in shared_bin.glob('*.dat'):
        target = release/asset.name
        if not target.exists() and not target.is_symlink():
            target.symlink_to(asset)


def snapshot(source, target):
    with sqlite3.connect(Path(source).resolve().as_uri()+'?mode=ro', uri=True) as src:
        with sqlite3.connect(target) as dst:
            src.backup(dst)
            if dst.execute('PRAGMA integrity_check').fetchone()[0] != 'ok':
                raise RuntimeError('Database backup integrity failed')
    target.chmod(0o600)


def switch(runtime, target):
    link = runtime.parent/'.runtime-next'
    link.unlink(missing_ok=True)
    link.symlink_to(target, target_is_directory=True)
    os.replace(link, runtime)


def activate(candidate, runtime, database, backup, service, check, rollback_check, pre_stop=None):
    previous = runtime.resolve(strict=True)
    if pre_stop is not None:
        pre_stop()
    stopped = False
    switched = False
    try:
        stopped = True
        service('stop')
        snapshot(database, backup)
        switch(runtime, candidate)
        switched = True
        service('start')
        if not check():
            raise RuntimeError('Release health check failed')
    except BaseException as failure:
        if stopped:
            try:
                service('stop')
                if switched:
                    switch(runtime, previous)
                    for suffix in ('-wal', '-shm'):
                        Path(str(database)+suffix).unlink(missing_ok=True)
                    restored = database.with_name('.restore-x-ui.db')
                    shutil.copy2(backup, restored)
                    os.replace(restored, database)
                service('start')
                if not rollback_check():
                    raise RuntimeError('Previous release did not recover proxy traffic')
                print('Rollback verified: previous release forwards traffic', flush=True)
            except BaseException as rollback_error:
                raise RuntimeError('ROLLBACK FAILED; manual recovery required: '+str(rollback_error)) from failure
        raise


def child_pids(pid, proc=Path('/proc')):
    children = set()
    for task in (proc/pid/'task').iterdir():
        try:
            children.update((task/'children').read_text().split())
        except FileNotFoundError:
            continue
    return children


def healthy(candidate, expected_version):
    try:
        pid = subprocess.check_output(['systemctl', 'show', '3x-ui', '-p', 'MainPID', '--value'], timeout=3).decode().strip()
        if not pid.isdigit() or int(pid) <= 0 or Path('/proc/'+pid+'/exe').resolve() != candidate/'x-ui':
            return False
        version = subprocess.check_output([str(candidate/'x-ui'), '-v'], timeout=5).decode().strip()
        if version != expected_version:
            return False
        children = child_pids(pid)
        core = Path('/opt/3x-ui/shared/bin/xray-linux-amd64').resolve()
        if not any(Path('/proc/'+child+'/exe').resolve() == core for child in children):
            return False
        opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
        with opener.open('http://127.0.0.1:18083/', timeout=3) as response:
            if response.status != 200:
                return False
        with socket.create_connection(('127.0.0.1', 2087), timeout=3):
            return True
    except (OSError, ValueError, subprocess.SubprocessError):
        return False


def accepted_live(release, version, shared_bin, directory):
    for _ in range(30):
        if healthy(release, version):
            try:
                config = shared_bin/'config.json'
                rates = Path(str(config)+'.rates.json')
                environment = core_environment(shared_bin, rates)
                verify_forwarding(release/'xray-linux-amd64', json.loads(config.read_text()), directory, environment)
                return True
            except (OSError, ValueError, RuntimeError, subprocess.SubprocessError):
                return False
        time.sleep(1)
    return False


def main():
    if os.geteuid() != 0 or len(sys.argv) != 2 or not re.fullmatch('[a-f0-9]{40}', sys.argv[1]):
        raise SystemExit('Usage: sudo 3x-ui-deploy <40-character commit SHA>')
    os.umask(0o077)
    commit = sys.argv[1]
    runtime = Path('/opt/3x-ui/runtime')
    if not runtime.is_symlink() or not Path('/var/lib/3x-ui/x-ui.db').is_file():
        raise SystemExit('Managed installation required')
    with open('/run/lock/3x-ui-deploy.lock', 'w') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        releases = Path('/opt/3x-ui/releases')
        candidate = Path(tempfile.mkdtemp(prefix=commit+'-', dir=releases))
        candidate.chmod(0o755)
        incoming = Path('/var/lib/xui-deploy/incoming')/(commit+'.tar.gz')
        try:
            core = subprocess.check_output(['/opt/3x-ui/shared/bin/xray-linux-amd64', 'version'], timeout=5).decode().splitlines()[0]
            if not core.startswith('Xray '+CORE_VERSION+' '):
                raise RuntimeError('Pinned core version mismatch; upgrade it separately')
            with tempfile.TemporaryFile() as saved:
                with os.fdopen(os.open(incoming, os.O_RDONLY | os.O_NOFOLLOW), 'rb') as upload:
                    shutil.copyfileobj(upload, saved)
                saved.seek(0)
                extract_release(saved, candidate, commit)
            expected = 'dev+'+commit[:8]
            if subprocess.check_output([str(candidate/'x-ui'), '-v'], timeout=5).decode().strip() != expected:
                raise RuntimeError('Executable build stamp mismatch')
            bundled_core = candidate/'xray-linux-amd64'
            if not bundled_core.is_file():
                raise RuntimeError('Release must bundle the bandwidth-aware core')
            core_version = subprocess.check_output([str(bundled_core), 'version'], timeout=5).decode()
            if not core_version.startswith('Xray '+CORE_VERSION+' ') or 'SorvoRateV1' not in core_version:
                raise RuntimeError('Bundled core version or bandwidth capability mismatch')
            shared_core = Path('/opt/3x-ui/shared/bin/xray-linux-amd64')
            previous = runtime.resolve(strict=True)
            # Deployment must not rewrite the last known-good installation.
            # Converting a legacy shared executable is a separate migration.
            if not shared_core.is_symlink() or shared_core.resolve() != previous/'xray-linux-amd64':
                raise RuntimeError('Managed core link required; migrate legacy layout separately')
            prepare_asset_links(candidate, shared_core.parent)
            previous_version = subprocess.check_output([str(previous/'x-ui'), '-v'], timeout=5).decode().strip()
            backup_dir = Path('/var/backups/3x-ui')
            backup_dir.mkdir(mode=0o700, exist_ok=True)
            backup = backup_dir/(commit+'-'+str(time.time_ns())+'.db')
            def service(action):
                subprocess.run(['systemctl', action, '3x-ui'], check=True, timeout=35)
            # All candidate processes are private loopback processes. The live
            # service, runtime link and database remain untouched during preflight.
            with tempfile.TemporaryDirectory(prefix='3x-ui-preflight-') as folder:
                directory = Path(folder)
                shared_bin = shared_core.parent
                config_path = shared_bin/'config.json'
                rates_path = Path(str(config_path)+'.rates.json')
                def signature():
                    return (runtime.resolve(), shared_core.resolve(), config_path.read_bytes(), rates_path.read_bytes() if rates_path.exists() else None)
                before = signature()
                old_check = lambda: accepted_live(previous, previous_version, shared_bin, directory)
                if not old_check():
                    raise RuntimeError('Live baseline is unhealthy; recover it before deploying')
                try:
                    preflight(candidate, config_path, shared_bin, directory)
                except BaseException:
                    # Preserve private diagnostics, never credentials in CI output.
                    diagnostics = backup_dir/('failed-preflight-'+commit+'-'+str(time.time_ns()))
                    shutil.copytree(directory, diagnostics)
                    diagnostics.chmod(0o700)
                    print('Candidate rejected before cutover; private diagnostics: '+str(diagnostics), flush=True)
                    raise
                def pre_stop():
                    if signature() != before:
                        raise RuntimeError('Live state changed after validation; production was not stopped')
                activate(candidate, runtime, Path('/var/lib/3x-ui/x-ui.db'), backup, service,
                         lambda: accepted_live(candidate, expected, shared_bin, directory), old_check, pre_stop)
            incoming.unlink(missing_ok=True)
            print('Deployed '+commit, flush=True)
        except BaseException:
            if runtime.resolve() != candidate:
                shutil.rmtree(candidate)
            raise


if __name__ == '__main__':
    main()
