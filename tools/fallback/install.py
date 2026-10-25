#!/usr/bin/python3 -I
"""One-time, additive install. Refuses to overwrite an existing fallback."""
import hashlib
import json
import os
import pwd
import secrets
import shutil
import socket
import subprocess
import uuid
from pathlib import Path
from urllib.parse import urlencode, quote


def main():
    if os.geteuid() != 0:
        raise SystemExit('Root required')
    os.umask(0o077)
    root, cfg = Path('/opt/xray-fallback'), Path('/etc/xray-fallback')
    unit = Path('/etc/systemd/system/xray-fallback.service')
    if root.exists() or cfg.exists() or unit.exists():
        raise SystemExit('Fallback already exists; refusing to overwrite')
    with socket.socket() as s: s.bind(('0.0.0.0', 24444))
    source = Path('/opt/3x-ui/shared/bin/xray-linux-amd64').resolve(strict=True)
    root.mkdir(mode=0o755); root.chmod(0o755); cfg.mkdir(mode=0o700)
    core = root/'xray'
    shutil.copyfile(source, core); core.chmod(0o755)
    keys = dict(line.split(': ', 1) for line in subprocess.check_output([str(core), 'x25519']).decode().strip().splitlines())
    identity, sid = str(uuid.uuid4()), secrets.token_hex(8)
    public = keys['Password (PublicKey)']
    server = {'log': {'loglevel': 'warning', 'access': 'none'},
              'inbounds': [{'listen': '0.0.0.0', 'port': 24444, 'protocol': 'vless',
                'settings': {'clients': [{'id': identity}], 'decryption': 'none'},
                'streamSettings': {'network': 'tcp', 'security': 'reality', 'realitySettings': {
                    'show': False, 'target': 'www.apple.com:443', 'serverNames': ['www.apple.com'],
                    'privateKey': keys['PrivateKey'], 'shortIds': [sid]}}}],
              'outbounds': [{'protocol': 'freedom', 'tag': 'direct'}, {'protocol': 'blackhole', 'tag': 'block'}],
              'routing': {'rules': [{'type': 'field', 'ip': ['0.0.0.0/8', '10.0.0.0/8', '127.0.0.0/8', '169.254.0.0/16', '172.16.0.0/12', '192.168.0.0/16', '224.0.0.0/4', '::1/128', 'fc00::/7', 'fe80::/10'], 'outboundTag': 'block'}]}}
    client = {'log': {'loglevel': 'warning'}, 'inbounds': [{'listen': '127.0.0.1', 'port': 18290, 'protocol': 'socks', 'settings': {'auth': 'noauth'}}],
              'outbounds': [{'protocol': 'vless', 'settings': {'vnext': [{'address': '65.49.221.8', 'port': 24444, 'users': [{'id': identity, 'encryption': 'none'}]}]},
                'streamSettings': {'network': 'tcp', 'security': 'reality', 'realitySettings': {'serverName': 'www.apple.com', 'fingerprint': 'chrome', 'password': public, 'shortId': sid}}}]}
    for name, data in [('config.json', server), ('client.json', client)]:
        (cfg/name).write_text(json.dumps(data, indent=2)); (cfg/name).chmod(0o600)
    params = urlencode({'encryption': 'none', 'security': 'reality', 'sni': 'www.apple.com', 'fp': 'chrome', 'pbk': public, 'sid': sid, 'type': 'tcp'})
    (cfg/'client-url.txt').write_text('vless://'+identity+'@65.49.221.8:24444?'+params+'#'+quote('Sorvo-独立备用')+'\n')
    (cfg/'manifest.json').write_text(json.dumps({'source': str(source), 'sha256': hashlib.sha256(core.read_bytes()).hexdigest()}, indent=2))
    subprocess.run([str(core), 'run', '-test', '-c', str(cfg/'config.json')], check=True)
    subprocess.run([str(core), 'run', '-test', '-c', str(cfg/'client.json')], check=True)
    try:
        account = pwd.getpwnam('xray-fallback')
    except KeyError:
        subprocess.run(['useradd', '--system', '--no-create-home', '--user-group', '--shell', '/usr/sbin/nologin', 'xray-fallback'], check=True)
        account = pwd.getpwnam('xray-fallback')
    os.chown(cfg, 0, account.pw_gid); cfg.chmod(0o750)
    os.chown(cfg/'config.json', 0, account.pw_gid); (cfg/'config.json').chmod(0o640)
    unit.write_text('''[Unit]
Description=Independent emergency VPN fallback
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=xray-fallback
Group=xray-fallback
Environment=GOMEMLIMIT=96MiB
Environment=GOGC=50
ExecStart=/opt/xray-fallback/xray run -c /etc/xray-fallback/config.json
WorkingDirectory=/opt/xray-fallback
Restart=on-failure
RestartSec=3
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
PrivateDevices=true
RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX
MemoryMax=192M
TasksMax=128
LimitNOFILE=8192
UMask=0077

[Install]
WantedBy=multi-user.target
''')
    unit.chmod(0o644)
    print('Fallback installed; validate systemd compatibility before starting.')


if __name__ == '__main__':
    main()
