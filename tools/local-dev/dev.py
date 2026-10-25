#!/usr/bin/env python3
"""Isolated local main development panel. Never calls SSH, Git push or deployment."""
import argparse, http.cookiejar, json, os, pathlib, platform, secrets
import signal, socket, sqlite3, subprocess, time, urllib.request, uuid

ROOT = pathlib.Path(__file__).resolve().parents[2]
STATE = ROOT / '.cache/local-dev'
URL = 'http://127.0.0.1:18090'
PORTS = (18090, 18190, 18191, 18192)
TOOLCHAIN = pathlib.Path('/private/tmp/xui-toolchain')


def environment():
    env = {k: v for k, v in os.environ.items() if not k.startswith(('XUI_', 'XRAY_', 'NODE_TOKEN_'))}
    bins = [TOOLCHAIN/'go/bin', TOOLCHAIN/'node-v26.12.0-darwin-arm64/bin']
    env['PATH'] = os.pathsep.join(map(str, bins)) + os.pathsep + env.get('PATH', '')
    if TOOLCHAIN.exists():
        env.update(GOPATH=str(TOOLCHAIN/'gopath'), GOCACHE=str(TOOLCHAIN/'gocache'))
    env.update(XUI_DB_TYPE='sqlite', XUI_DB_DSN='', XUI_DB_FOLDER=str(STATE/'db'),
               XUI_BIN_FOLDER=str(STATE/'bin'), XUI_LOG_FOLDER=str(STATE/'logs'),
               XUI_MAIN_FOLDER=str(STATE), XUI_PORT='18090', XUI_PPROF='false',
               XUI_TUNNEL_HEALTH_MONITOR='false', NODE_TOKEN_ENCRYPTION='off')
    return env


def run(args, cwd=ROOT, **kwargs):
    return subprocess.run(list(map(str, args)), cwd=cwd, env=environment(), check=True, **kwargs)


def branch_guard():
    branch = subprocess.check_output(['git', 'branch', '--show-current'], cwd=ROOT, text=True).strip()
    if branch != 'main':
        raise RuntimeError('Local environment requires main; current branch: ' + branch)


def pid():
    try:
        number = int((STATE/'panel.pid').read_text())
        command = subprocess.check_output(['ps', '-p', str(number), '-o', 'command='], text=True).strip()
        return number if command.startswith(str(STATE/'panel') + ' run') else None
    except (FileNotFoundError, ValueError, subprocess.CalledProcessError):
        return None


def stop():
    number = pid()
    if number:
        os.kill(number, signal.SIGTERM)
        for _ in range(100):
            if not pid():
                break
            time.sleep(.1)
        else:
            raise RuntimeError('Local panel did not exit; refusing to overwrite binaries')
    (STATE/'panel.pid').unlink(missing_ok=True)
    print('Local panel stopped; test data preserved.')


def build():
    branch_guard()
    # Build to temporary filenames; never replace a running executable.
    run(['npm', 'run', 'build'], cwd=ROOT/'frontend')
    run(['go', 'build', '-o', STATE/'panel.next', '.'])
    run(['python3', 'tools/core-rate-limit/build.py', '--output', STATE/'core.next'])
    stop()
    os.replace(STATE/'panel.next', STATE/'panel')
    os.replace(STATE/'core.next', STATE/'bin'/core_name())
    (STATE/'build.json').write_text(json.dumps({'commit': subprocess.check_output(
        ['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip(), 'builtAt': time.time()}))


def core_name():
    return 'xray-' + {'Darwin': 'darwin', 'Linux': 'linux'}[platform.system()] + '-' + {
        'arm64': 'arm64', 'aarch64': 'arm64', 'x86_64': 'amd64'}[platform.machine()]


def initialize():
    dbpath = STATE/'db/x-ui.db'
    if dbpath.exists():
        return
    credentials = {'url': URL, 'username': 'LocalAdmin', 'password': secrets.token_urlsafe(12)}
    (STATE/'credentials.json').write_text(json.dumps(credentials, indent=2))
    run([STATE/'panel', 'setting', '-username', credentials['username'], '-password', credentials['password'],
         '-port', '18090', '-listenIP', '127.0.0.1'], cwd=STATE,
        stdout=subprocess.DEVNULL)
    template = json.loads((ROOT/'internal/web/service/config.json').read_text())
    template['inbounds'][0]['port'] = 18191
    template['metrics']['listen'] = '127.0.0.1:18192'
    # Local fixtures deliberately permit loopback; no production geo assets/config are copied.
    template['outbounds'][0]['settings'] = {'finalRules': [{'action': 'allow', 'ip': ['127.0.0.1/32']}]}
    template['routing']['rules'] = template['routing']['rules'][:1]
    settings = {'webListen': '127.0.0.1', 'webPort': '18090', 'webBasePath': '/',
                'subEnable': 'false', 'subJsonEnable': 'false', 'tgBotEnable': 'false',
                'sessionMaxAge': '2592000', 'xrayTemplateConfig': json.dumps(template)}
    with sqlite3.connect(dbpath) as db:
        for key, value in settings.items():
            db.execute('DELETE FROM settings WHERE key=?', (key,))
            db.execute('INSERT INTO settings(key,value) VALUES(?,?)', (key, value))


def api():
    jar = http.cookiejar.CookieJar()
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), urllib.request.HTTPCookieProcessor(jar))
    def request(path, body=None):
        headers = {}
        data = None
        if body is not None:
            token = json.load(opener.open(URL+'/csrf-token'))['obj']
            headers = {'Content-Type': 'application/json', 'X-CSRF-Token': token, 'Origin': URL}
            data = json.dumps(body).encode()
        result = json.load(opener.open(urllib.request.Request(URL+path, data=data, headers=headers), timeout=10))
        if result.get('success') is False:
            raise RuntimeError(str(result.get('msg')))
        return result
    credentials = json.loads((STATE/'credentials.json').read_text())
    request('/login', {k: credentials[k] for k in ('username', 'password')})
    return request


def seed():
    request = api()
    if not request('/panel/api/inbounds/list')['obj']:
        client = {'id': str(uuid.uuid4()), 'email': 'local-test', 'enable': True,
                  'subId': secrets.token_hex(8), 'totalGB': 0, 'expiryTime': 0, 'flow': ''}
        request('/panel/api/inbounds/add', {'remark': '本地测试 · main', 'enable': True,
                'listen': '127.0.0.1', 'port': 18190, 'protocol': 'vless',
                'settings': json.dumps({'clients': [client], 'decryption': 'none'}),
                'streamSettings': json.dumps({'network': 'tcp', 'security': 'none'}),
                'sniffing': json.dumps({'enabled': False})})
    request('/logout', {})


def start():
    branch_guard()
    if pid():
        print(URL + ' already running')
        return
    if not (STATE/'panel').exists():
        raise RuntimeError('Run build first')
    for port in PORTS:
        with socket.socket() as sock:
            sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
            sock.bind(('127.0.0.1', port))
    initialize()
    with (STATE/'logs/panel.log').open('ab') as log:
        process = subprocess.Popen([str(STATE/'panel'), 'run'], cwd=STATE, env=environment(),
                                   stdin=subprocess.DEVNULL, stdout=log, stderr=log, start_new_session=True)
    (STATE/'panel.pid').write_text(str(process.pid))
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    for _ in range(100):
        if process.poll() is not None:
            raise RuntimeError('Panel exited: see .cache/local-dev/logs/panel.log')
        try:
            opener.open(URL, timeout=1).close()
            with socket.create_connection(('127.0.0.1', 18191), timeout=1):
                pass
            seed()
            print('Local main panel ready: ' + URL)
            return
        except (OSError, urllib.error.URLError):
            time.sleep(.2)
    stop()
    raise RuntimeError('Local startup timed out')


def main():
    os.umask(0o077)
    for folder in ('db', 'bin', 'logs'):
        (STATE/folder).mkdir(parents=True, exist_ok=True)
    parser = argparse.ArgumentParser()
    parser.add_argument('action', choices=['build', 'start', 'stop', 'restart', 'status', 'credentials'])
    action = parser.parse_args().action
    if action == 'restart':
        stop(); start()
    elif action == 'status':
        print({'url': URL, 'pid': pid(), 'data': str(STATE)})
    elif action == 'credentials':
        print((STATE/'credentials.json').read_text())
    else:
        globals()[action]()

if __name__ == '__main__':
    main()
