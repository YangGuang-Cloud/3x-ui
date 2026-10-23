#!/usr/bin/env python3
"""Exercise real candidate isolation and the missing-geodata failure on loopback."""
import http.server
import importlib.util
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import threading
import uuid


def field(number, body):
    if len(body) >= 128:
        raise ValueError('Fixture only supports short protobuf fields')
    return bytes([number*8+2, len(body)])+body


def verify(binary):
    spec = importlib.util.spec_from_file_location('installer', Path(__file__).with_name('install_release.py'))
    installer = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(installer)
    with tempfile.TemporaryDirectory(prefix='release-isolation-test-') as name:
        root = Path(name)
        shared, candidate, live, staging, probe = [root/p for p in ('shared', 'candidate', 'live', 'staging', 'probe')]
        for directory in (shared, candidate, live, staging, probe): directory.mkdir(mode=0o700)
        core = candidate/'xray-linux-amd64'
        shutil.copy2(binary, core)
        core.chmod(0o700)
        # GeoIPList(entry=GeoIP(code=private, cidr=127.0.0.0/8)).
        geodata = field(1, field(1, b'PRIVATE')+field(2, field(1, b'\x7f\x00\x00\x00')+b'\x10\x08'))
        (shared/'geoip.dat').write_bytes(geodata)
        cert, key = shared/'cert.pem', shared/'key.pem'
        subprocess.run(['openssl', 'req', '-x509', '-newkey', 'rsa:2048', '-nodes', '-keyout', str(key), '-out', str(cert), '-days', '1', '-subj', '/CN=localhost'], check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        identity = str(uuid.uuid4())
        client = {'id': identity, 'email': 'fixture-only'}
        ports = [installer.loopback_port() for _ in range(3)]
        original = {
            'log': {'loglevel': 'warning'},
            'api': {'tag': 'api', 'services': ['StatsService']}, 'stats': {},
            'metrics': {'tag': 'metrics', 'listen': '127.0.0.1:'+str(installer.loopback_port())},
            'inbounds': [
                {'tag': 'api', 'listen': '127.0.0.1', 'port': ports[0], 'protocol': 'tunnel', 'settings': {'address': '127.0.0.1'}},
                {'tag': 'direct', 'listen': '127.0.0.1', 'port': ports[1], 'protocol': 'vless', 'settings': {'decryption': 'none', 'clients': [dict(client, flow='xtls-rprx-vision')]}, 'streamSettings': {'network': 'tcp', 'security': 'tls', 'tlsSettings': {'certificates': [{'certificateFile': str(cert), 'keyFile': str(key)}]}}},
                {'tag': 'edge', 'listen': '127.0.0.1', 'port': ports[2], 'protocol': 'vless', 'settings': {'decryption': 'none', 'clients': [client]}, 'streamSettings': {'network': 'ws', 'security': 'none', 'wsSettings': {'path': '/fixture'}}},
            ],
            'outbounds': [{'tag': 'direct', 'protocol': 'freedom', 'settings': {'finalRules': [{'action': 'allow', 'ip': ['127.0.0.1/32']}]}}],
            'routing': {'rules': [{'type': 'field', 'inboundTag': ['api'], 'outboundTag': 'api'}, {'type': 'field', 'ip': ['geoip:private'], 'outboundTag': 'direct'}]},
        }
        config = shared/'config.json'
        config.write_text(json.dumps(original)); config.chmod(0o600)
        rates = Path(str(config)+'.rates.json'); rates.write_text('{}')
        before = config.read_bytes()
        environment = installer.core_environment(shared, rates)
        class Handler(http.server.BaseHTTPRequestHandler):
            def do_GET(self):
                self.send_response(200); self.end_headers(); self.wfile.write(b'verified')
            def log_message(self, *_): pass
        httpd = http.server.ThreadingHTTPServer(('127.0.0.1', 0), Handler)
        threading.Thread(target=httpd.serve_forever, daemon=True).start()
        urls = ('http://127.0.0.1:'+str(httpd.server_address[1])+'/',)
        try:
            with installer.temporary_core(core, original, live, 'known-good', environment) as process:
                # Starts a separate probe client; known-good listeners stay alive.
                installer.verify_forwarding(core, original, probe, environment, urls)
                installer.preflight(candidate, config, shared, staging, urls)
                assert process.poll() is None, 'Validation stopped the known-good process'
                assert config.read_bytes() == before, 'Validation changed the live configuration'
                (shared/'geoip.dat').unlink()
                try:
                    installer.preflight(candidate, config, shared, staging, urls)
                except subprocess.CalledProcessError:
                    pass
                else:
                    raise AssertionError('Missing production geodata did not block candidate')
                # Existing process has already loaded geodata and must still forward.
                installer.verify_forwarding(core, original, probe, environment, urls)
                assert process.poll() is None
                print('PREFLIGHT_ISOLATION_OK: TLS Vision + WS + API + metrics + geodata')
                print('MISSING_GEODATA_BLOCKED: known-good traffic remained available')
        except BaseException:
            for path in root.rglob('*.stderr'):
                print(path.name, path.read_text()[-2500:], file=sys.stderr)
            raise
        finally:
            httpd.shutdown(); httpd.server_close()


if __name__ == '__main__':
    os.umask(0o077)
    verify(Path(sys.argv[1]).resolve(strict=True))
