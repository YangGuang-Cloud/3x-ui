#!/usr/bin/python3 -I
"""Check the independent fallback using real HTTPS, never restart a service."""
import json
import os
import socket
import subprocess
import tempfile
import time
from pathlib import Path


def main():
    subprocess.run(['systemctl', 'is-active', '--quiet', 'xray-fallback'], check=True)
    core = Path('/opt/xray-fallback/xray')
    pid = subprocess.check_output(['systemctl', 'show', 'xray-fallback', '-p', 'MainPID', '--value']).decode().strip()
    if Path('/proc/'+pid+'/exe').resolve() != core or core.is_symlink():
        raise RuntimeError('Fallback executable is not independent')
    config = json.loads(Path('/etc/xray-fallback/client.json').read_text())
    config['outbounds'][0]['settings']['vnext'][0]['address'] = '127.0.0.1'
    with socket.socket() as s:
        s.bind(('127.0.0.1', 0))
        port = s.getsockname()[1]
    config['inbounds'][0]['port'] = port
    with tempfile.TemporaryDirectory(prefix='fallback-check-') as folder:
        path = Path(folder)/'client.json'
        path.write_text(json.dumps(config)); path.chmod(0o600)
        env = dict(os.environ)
        env.pop('XUI_CLIENT_RATE_LIMITS_FILE', None)
        with open(Path(folder)/'core.log', 'wb') as log:
            p = subprocess.Popen([str(core), 'run', '-c', str(path)], env=env, stdout=log, stderr=log)
            try:
                for _ in range(50):
                    if p.poll() is not None:
                        raise RuntimeError('Fallback probe client failed')
                    try:
                        with socket.create_connection(('127.0.0.1', port), timeout=.1): break
                    except OSError: time.sleep(.1)
                for url in ('https://example.com/', 'https://www.cloudflare.com/cdn-cgi/trace'):
                    r = subprocess.run(['curl', '--noproxy', '', '--socks5-hostname', '127.0.0.1:'+str(port), '--connect-timeout', '5', '--max-time', '15', '--silent', '--fail', '--output', '/dev/null', url])
                    if r.returncode == 0:
                        print('Independent fallback: real HTTPS forwarding passed')
                        return
                raise RuntimeError('Fallback cannot forward HTTPS; do not deploy')
            finally:
                p.terminate()
                try: p.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    p.kill(); p.wait()


if __name__ == '__main__':
    main()
