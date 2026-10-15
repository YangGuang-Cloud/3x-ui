"""Switch the verified staging instance to the original port, with rollback."""
import hashlib
import json
import os
import shutil
import socket
import sqlite3
import ssl
import subprocess
import time
import urllib.request
from pathlib import Path


def merge_counter(current, staged, baseline):
    if min(current, staged, baseline) < 0 or min(current, staged) < baseline:
        raise ValueError('Ambiguous counter reset; manual review required')
    return current + staged - baseline


def verify_unchanged(baseline, current):
    if {k: v for k, v in baseline.items() if k not in ('up', 'down')} != {
            k: v for k, v in current.items() if k not in ('up', 'down')}:
        raise ValueError('Live configuration changed; repeat staging validation')


def rows(path, table):
    with sqlite3.connect(Path(path).resolve().as_uri() + '?mode=ro', uri=True) as c:
        c.row_factory = sqlite3.Row
        return [dict(row) for row in c.execute('SELECT * FROM ' + table)]


def snapshot(source, target):
    with sqlite3.connect(Path(source).resolve().as_uri() + '?mode=ro', uri=True) as src:
        with sqlite3.connect(target) as dst:
            src.backup(dst)
            if dst.execute('PRAGMA integrity_check').fetchone()[0] != 'ok':
                raise RuntimeError('Backup integrity check failed')


def command(*args):
    return subprocess.run(args, check=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)


def probe(core, data, port, work):
    clients = json.loads(data['settings'])['clients']
    now = int(time.time() * 1000)
    usage = {r['email']: r for r in rows('/var/lib/3x-ui/x-ui.db', 'client_traffics')}
    eligible = [c for c in clients if c.get('enable', True) and (c.get('totalGB', 0) == 0 or
        usage[c['email']]['up'] + usage[c['email']]['down'] < c['totalGB']) and (
        c.get('expiryTime', 0) == 0 or c.get('expiryTime', 0) > now)]
    if not eligible:
        raise RuntimeError('No eligible proxy test client')
    client = eligible[0]
    tls = json.loads(data['stream_settings'])['tlsSettings']
    cert = Path(tls['certificates'][0]['certificateFile'])
    pem = cert.read_text()
    leaf = pem[pem.index('-----BEGIN CERTIFICATE-----'):pem.index('-----END CERTIFICATE-----') + 25]
    digest = hashlib.sha256(ssl.PEM_cert_to_DER_cert(leaf)).hexdigest()
    decoded = ssl._ssl._test_decode_cert(str(cert))
    names = [v for k, v in decoded.get('subjectAltName', []) if k == 'DNS']
    name = tls.get('serverName') or (names[0].replace('*.', 'test.') if names else 'localhost')
    profile = {'log': {'loglevel': 'error'}, 'inbounds': [
        {'listen': '127.0.0.1', 'port': 18085, 'protocol': 'socks', 'settings': {'auth': 'noauth'}}],
        'outbounds': [{'protocol': 'vless', 'settings': {'vnext': [
            {'address': '127.0.0.1', 'port': port, 'users': [
                {'id': client['id'], 'encryption': 'none', 'flow': client.get('flow', '')}]}]},
            'streamSettings': {'network': 'tcp', 'security': 'tls',
                               'tlsSettings': {'serverName': name, 'pinnedPeerCertSha256': digest}}}]}
    path = work / 'probe.json'
    path.write_text(json.dumps(profile))
    with open(work / 'probe.log', 'wb') as log:
        process = subprocess.Popen([str(core), 'run', '-c', str(path)], stdout=log, stderr=log)
        try:
            for _ in range(40):
                try:
                    socket.create_connection(('127.0.0.1', 18085), timeout=.2).close()
                    break
                except OSError:
                    if process.poll() is not None:
                        raise RuntimeError('Probe exited')
                    time.sleep(.2)
            command('curl', '--fail', '--silent', '--show-error', '--max-time', '20',
                    '--socks5-hostname', '127.0.0.1:18085', 'https://example.com/', '-o', '/dev/null')
        finally:
            process.terminate()
            try:
                process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait()
            path.unlink(missing_ok=True)


def main():
    if os.geteuid() != 0:
        raise SystemExit('Run on the verified server as root')
    os.umask(0o077)
    stage = Path('/opt/3x-ui-test')
    baseline = Path('/root/x-ui-backups/20261015T025825Z/x-ui.db')
    source = Path('/etc/x-ui/x-ui.db')
    destination = Path('/opt/3x-ui')
    unit = Path('/etc/systemd/system/3x-ui.service')
    if destination.exists() or unit.exists():
        raise SystemExit('Production destination already exists; inspect before retrying')
    base = rows(baseline, 'inbounds')[0]
    current = rows(source, 'inbounds')[0]
    verify_unchanged(base, current)
    staged = rows(stage / 'state/x-ui.db', 'inbounds')[0]
    if staged['port'] != 24443:
        raise SystemExit('Unexpected staging configuration')
    work = Path('/root/x-ui-backups') / ('cutover-' + time.strftime('%Y%m%dT%H%M%SZ', time.gmtime()))
    work.mkdir(mode=0o700)
    snapshot(source, work / 'legacy-before.db')
    snapshot(stage / 'state/x-ui.db', work / 'staging-before.db')
    shutil.copy2('/etc/systemd/system/x-ui.service', work / 'legacy.service')
    destination.mkdir(mode=0o755)
    release = destination / 'releases/upstream-v3.10.2'
    release.mkdir(parents=True)
    shutil.copy2(stage / 'candidate/x-ui', release / 'x-ui')
    shared = destination / 'shared/bin'
    shutil.copytree(stage / 'candidate/bin', shared)
    (destination / 'runtime').symlink_to(release, target_is_directory=True)
    state = Path('/var/lib/3x-ui')
    logs = Path('/var/log/3x-ui')
    state.mkdir(mode=0o700, exist_ok=False)
    logs.mkdir(mode=0o700, exist_ok=False)
    etc = Path('/etc/3x-ui')
    etc.mkdir(mode=0o700, exist_ok=False)
    shutil.copy2(stage / 'admin.json', etc / 'admin.json')
    env = {'XUI_DB_FOLDER': str(state), 'XUI_LOG_FOLDER': str(logs),
           'XUI_BIN_FOLDER': str(shared), 'XUI_INIT_WEB_BASE_PATH': '/', 'XUI_PORT': '18083',
           'XUI_DEBUG': 'false', 'XUI_ENABLE_FAIL2BAN': 'false'}
    (etc / 'runtime.env').write_text(''.join(k + '=' + v + '\n' for k, v in env.items()))
    unit.write_text('[Unit]\nDescription=Managed 3x-ui\nAfter=network-online.target\nWants=network-online.target\n'
                    '[Service]\nType=simple\nUser=root\nUMask=0077\n'
                    'WorkingDirectory=/opt/3x-ui/runtime\nEnvironmentFile=/etc/3x-ui/runtime.env\n'
                    'ExecStart=/opt/3x-ui/runtime/x-ui run\nRestart=on-failure\nRestartSec=3\n'
                    'TimeoutStopSec=20\nKillMode=control-group\nNoNewPrivileges=true\n'
                    'ProtectSystem=full\nLimitNOFILE=65536\n[Install]\nWantedBy=multi-user.target\n')
    unit.chmod(0o644)
    command('systemd-analyze', 'verify', str(unit))
    command('systemctl', 'daemon-reload')
    original_enabled = subprocess.run(['systemctl', 'is-enabled', 'x-ui'], stdout=subprocess.PIPE).stdout.strip() == b'enabled'
    preflight = json.loads((stage / 'candidate/bin/config.json').read_text())
    old_values = {r['key']: r['value'] for r in rows(source, 'settings')}
    old_template = json.loads(old_values.get('xrayTemplateConfig', '{}'))
    for key in ('routing', 'outbounds', 'policy', 'dns'):
        if key in old_template:
            preflight[key] = old_template[key]
    for inbound in preflight['inbounds']:
        if inbound.get('tag') == staged['tag']:
            inbound['port'] = current['port']
    preflight_path = work / 'core-preflight.json'
    preflight_path.write_text(json.dumps(preflight))
    command(str(shared / 'xray-linux-amd64'), 'run', '-test', '-c', str(preflight_path))
    stopped = False
    try:
        command('systemctl', 'stop', 'x-ui')
        stopped = True
        command('systemctl', 'stop', '3x-ui-test')
        snapshot(source, work / 'legacy-frozen.db')
        snapshot(stage / 'state/x-ui.db', work / 'staging-frozen.db')
        current = rows(work / 'legacy-frozen.db', 'inbounds')[0]
        verify_unchanged(base, current)
        snapshot(work / 'staging-frozen.db', state / 'x-ui.db')
        old_stats = {r['email']: r for r in rows(work / 'legacy-frozen.db', 'client_infos')}
        base_stats = {r['email']: r for r in rows(baseline, 'client_infos')}
        stage_stats = {r['email']: r for r in rows(work / 'staging-frozen.db', 'client_traffics')}
        if not (old_stats.keys() == base_stats.keys() == stage_stats.keys()):
            raise RuntimeError('Client set changed')
        with sqlite3.connect(state / 'x-ui.db') as c:
            for email, original in old_stats.items():
                verify_unchanged(base_stats[email], original)
                up = merge_counter(original['up'], stage_stats[email]['up'], base_stats[email]['up'])
                down = merge_counter(original['down'], stage_stats[email]['down'], base_stats[email]['down'])
                c.execute('UPDATE client_traffics SET up=?,down=? WHERE email=?', (up, down, email))
            latest_stage = rows(work / 'staging-frozen.db', 'inbounds')[0]
            up = merge_counter(current['up'], latest_stage['up'], base['up'])
            down = merge_counter(current['down'], latest_stage['down'], base['down'])
            c.execute('UPDATE inbounds SET port=?,up=?,down=? WHERE id=?', (current['port'], up, down, staged['id']))
            old_settings = {r['key']: r['value'] for r in rows(work / 'legacy-frozen.db', 'settings')}
            old_template = json.loads(old_settings.get('xrayTemplateConfig', '{}'))
            template = json.loads(c.execute("SELECT value FROM settings WHERE key='xrayTemplateConfig'").fetchone()[0])
            for key in ('routing', 'outbounds', 'policy', 'dns'):
                if key in old_template:
                    template[key] = old_template[key]
            c.execute("UPDATE settings SET value=? WHERE key='xrayTemplateConfig'", (json.dumps(template),))
            c.commit()
        command('systemctl', 'start', '3x-ui')
        opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
        for attempt in range(30):
            try:
                with opener.open('http://127.0.0.1:18083/', timeout=2) as response:
                    if response.status != 200:
                        raise RuntimeError('Panel health check failed')
                socket.create_connection(('127.0.0.1', current['port']), timeout=2).close()
                break
            except (OSError, RuntimeError):
                if attempt == 29:
                    raise RuntimeError('Production startup failed') from None
                time.sleep(1)
        probe(shared / 'xray-linux-amd64', staged, current['port'], work)
        command('systemctl', 'enable', '3x-ui')
        command('systemctl', 'disable', 'x-ui')
        (work / 'result.json').write_text(json.dumps({'success': True, 'originalPort': current['port'],
            'panelVersion': '3.10.2', 'coreVersion': '26.10.10', 'stageDeltaMerged': True,
            'rollbackLegacyDatabase': str(work / 'legacy-frozen.db')}))
        print('CUTOVER_OK productionPort=' + str(current['port']) + ' unit=3x-ui.service')
    except BaseException:
        subprocess.run(['systemctl', 'stop', '3x-ui'], stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        subprocess.run(['systemctl', 'disable', '3x-ui'], stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        if original_enabled:
            subprocess.run(['systemctl', 'enable', 'x-ui'], stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        if stopped:
            subprocess.run(['systemctl', 'start', 'x-ui'], check=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
            restart = subprocess.run(['systemctl', 'start', '3x-ui-test'], stdout=subprocess.PIPE, stderr=subprocess.PIPE)
            if restart.returncode:
                start = ['systemd-run', '--unit=3x-ui-test', '--collect',
                         '--property=WorkingDirectory=' + str(stage / 'candidate')]
                stage_env = dict(env, XUI_DB_FOLDER=str(stage / 'state'),
                                 XUI_LOG_FOLDER=str(stage / 'logs'), XUI_BIN_FOLDER=str(stage / 'candidate/bin'))
                start += ['--setenv=' + key + '=' + value for key, value in stage_env.items()]
                subprocess.run(start + [str(stage / 'candidate/x-ui'), 'run'],
                               stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        print('CUTOVER_FAILED legacy service restored; inspect private cutover log')
        raise


if __name__ == '__main__':
    main()
