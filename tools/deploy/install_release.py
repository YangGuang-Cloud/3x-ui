#!/usr/bin/python3 -I
"""Root-owned deployment entry point for the managed 3x-ui installation."""
import fcntl
import json
import os
import re
import shutil
import socket
import sqlite3
import subprocess
import sys
import tarfile
import tempfile
import time
import urllib.request
from pathlib import Path


CORE_VERSION = '26.10.10'


def extract_release(archive, destination, commit):
    destination.mkdir(mode=0o755, parents=True, exist_ok=True)
    with tarfile.open(fileobj=archive, mode='r:gz') as bundle:
        members = bundle.getmembers()
        if len(members) != 2 or {m.name for m in members} != {'x-ui', 'manifest.json'}:
            raise ValueError('Unexpected archive member')
        for member in members:
            limit = 256 * 1024 * 1024 if member.name == 'x-ui' else 4096
            if not member.isfile() or not 0 < member.size <= limit:
                raise ValueError('Invalid archive member type or size')
            with bundle.extractfile(member) as source, open(destination/member.name, 'xb') as target:
                shutil.copyfileobj(source, target)
            (destination/member.name).chmod(0o755 if member.name == 'x-ui' else 0o644)
    manifest = json.loads((destination/'manifest.json').read_text())
    if manifest.get('commit') != commit or manifest.get('coreVersion') != CORE_VERSION:
        raise ValueError('Release manifest mismatch')
    with open(destination/'x-ui', 'rb') as binary:
        header = binary.read(20)
    if len(header) != 20 or header[:5] != b'\x7fELF\x02' or header[18:20] != b'\x3e\x00':
        raise ValueError('Expected Linux amd64 executable')


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


def activate(candidate, runtime, database, backup, service, check):
    previous = runtime.resolve(strict=True)
    stopped = False
    switched = False
    try:
        service('stop')
        stopped = True
        snapshot(database, backup)
        switch(runtime, candidate)
        switched = True
        service('start')
        if not check():
            raise RuntimeError('Release health check failed')
    except BaseException:
        if stopped:
            service('stop')
            if switched:
                switch(runtime, previous)
                for suffix in ('-wal', '-shm'):
                    Path(str(database)+suffix).unlink(missing_ok=True)
                restored = database.with_name('.restore-x-ui.db')
                shutil.copy2(backup, restored)
                os.replace(restored, database)
            service('start')
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
        core = Path('/opt/3x-ui/shared/bin/xray-linux-amd64')
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
            backup_dir = Path('/var/backups/3x-ui')
            backup_dir.mkdir(mode=0o700, exist_ok=True)
            backup = backup_dir/(commit+'-'+str(time.time_ns())+'.db')
            def service(action):
                subprocess.run(['systemctl', action, '3x-ui'], check=True, timeout=35)
            def check():
                for _ in range(30):
                    if healthy(candidate, expected):
                        return True
                    time.sleep(1)
                return False
            activate(candidate, runtime, Path('/var/lib/3x-ui/x-ui.db'), backup, service, check)
            incoming.unlink(missing_ok=True)
            print('Deployed '+commit, flush=True)
        except BaseException:
            if runtime.resolve() != candidate:
                shutil.rmtree(candidate)
            raise


if __name__ == '__main__':
    main()
