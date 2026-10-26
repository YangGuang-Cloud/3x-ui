#!/usr/bin/env python3
"""Actual loopback VLESS quota/rollover test. Snapshots and restores local data only."""
import http.server
import json
from pathlib import Path
import socket
import sqlite3
import struct
import tempfile
import threading
import time
import uuid

import dev

BODY = b'q' * (2 * 1024 * 1024)

class Handler(http.server.BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass
    def do_GET(self):
        self.send_response(200)
        self.send_header('Content-Length', str(len(BODY)))
        self.end_headers()
        try:
            self.wfile.write(BODY)
        except (BrokenPipeError, ConnectionResetError):
            pass

def transfer(client_id, port):
    result = b''
    try:
        with socket.create_connection(('127.0.0.1', 18190), timeout=3) as sock:
            sock.settimeout(3)
            sock.sendall(b'\0' + uuid.UUID(client_id).bytes + b'\0\1' + struct.pack('!H', port) +
                         b'\1\x7f\0\0\1GET / HTTP/1.0\r\nHost: localhost\r\n\r\n')
            while chunk := sock.recv(65536):
                result += chunk
    except (OSError, TimeoutError):
        return False
    return result.startswith(b'\0\0') and result.split(b'\r\n\r\n', 1)[-1] == BODY

def wait_gate(dbpath, enabled):
    for _ in range(30):
        with sqlite3.connect(dbpath) as db:
            value = db.execute("SELECT enable FROM client_traffics WHERE email='local-test'").fetchone()[0]
        if bool(value) == enabled:
            return
        time.sleep(1)
    with sqlite3.connect(dbpath) as db:
        print('quota debug clients:',db.execute("SELECT id,total_gb,enable FROM clients WHERE email='local-test'").fetchall(), flush=True)
        print('quota debug counters:',db.execute("SELECT up,down,total,enable FROM client_traffics WHERE email='local-test'").fetchall(), flush=True)
        print('quota debug cycle:',db.execute('SELECT id,start,next_reset FROM bandwagon_cycles').fetchall(), flush=True)
        print('quota debug ledger:',db.execute('SELECT client_id,minute,up,down FROM bandwagon_traffics').fetchall(), flush=True)
        print('quota debug blocks:',db.execute('SELECT * FROM bandwagon_quota_blocks').fetchall(), flush=True)
    raise AssertionError('runtime quota gate did not converge')

def main():
    dev.branch_guard()
    path = dev.STATE/'db/x-ui.db'
    cfg = json.loads((dev.STATE/'db/bandwagon.json').read_text())
    assert cfg['enabled'], 'Local provider fixture must be enabled'
    server = http.server.ThreadingHTTPServer(('127.0.0.1', 0), Handler)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    dev.stop()
    with tempfile.TemporaryDirectory(prefix='xui-local-quota-') as temp:
        backup = Path(temp)/'before.db'
        with sqlite3.connect(path) as source, sqlite3.connect(backup) as target:
            source.backup(target)
        try:
            now = int(time.time())//60*60
            with sqlite3.connect(path) as db:
                cid, identity = db.execute("SELECT id,uuid FROM clients WHERE email='local-test'").fetchone()
                db.execute('DELETE FROM bandwagon_quota_blocks')
                db.execute('DELETE FROM bandwagon_traffics')
                db.execute('DELETE FROM bandwagon_cycles')
                db.execute('INSERT INTO bandwagon_cycles(ve_id,next_reset,start,partial,quota,used,multiplier,updated_at_unix) VALUES(?,?,?,?,?,?,?,?)', (cfg['veid'], now+3600, now-600, 1, 1000000000, 0, 1, now))
                db.execute("UPDATE clients SET total_gb=?,enable=1,expiry_time=0 WHERE id=?", (1024*1024,cid))
                db.execute("UPDATE client_traffics SET total=?,up=?,down=0,enable=1,expiry_time=0 WHERE email='local-test'", (1024*1024,2*1024**3))
                for iid, raw in db.execute('SELECT id,settings FROM inbounds WHERE port=18190').fetchall():
                    settings = json.loads(raw)
                    for client in settings['clients']:
                        if client['email']=='local-test':
                            client.update(totalGB=1024*1024, enable=True, expiryTime=0)
                    db.execute('UPDATE inbounds SET settings=? WHERE id=?',(json.dumps(settings),iid))
                db.execute('DELETE FROM client_bandwidths WHERE client_id=?',(cid,))
            dev.start()
            time.sleep(12)  # Allow the core collector to establish its initial baseline.
            assert transfer(identity,server.server_port), 'old lifetime use incorrectly blocked fresh cycle'
            wait_gate(path,False)
            assert not transfer(identity,server.server_port), 'exhausted cycle still forwards'
            request=dev.api()
            page=request('/panel/api/clients/list/paged')['obj']
            row=next(c for c in page['items'] if c['email']=='local-test')
            assert row['quotaUsed']>=1024*1024 and row['traffic']['up']+row['traffic']['down']>2*1024**3
            request('/logout',{})
            dev.stop()
            # Simulate a provider-confirmed boundary using only local fixture rows.
            boundary=int(time.time())//60*60
            with sqlite3.connect(path) as db:
                db.execute('UPDATE bandwagon_traffics SET minute=?',(boundary-60,))
                db.execute('UPDATE bandwagon_cycles SET next_reset=?',(boundary,))
                db.execute('INSERT INTO bandwagon_cycles(ve_id,next_reset,start,partial,quota,used,multiplier,updated_at_unix) VALUES(?,?,?,?,?,?,?,?)',(cfg['veid'],boundary+30*86400,boundary,0,1000000000,0,1,int(time.time())))
                old_usage=db.execute("SELECT up+down FROM client_traffics WHERE email='local-test'").fetchone()[0]
            dev.start()
            wait_gate(path,True)
            assert transfer(identity,server.server_port), 'new cycle did not restore actual forwarding'
            with sqlite3.connect(path) as db:
                assert db.execute("SELECT up+down FROM client_traffics WHERE email='local-test'").fetchone()[0]>=old_usage
                assert db.execute('SELECT count(*) FROM bandwagon_cycles').fetchone()[0]==2
            print('PASS actual VLESS: lifetime counters ignored, cycle quota blocks, new cycle restores, history preserved')
        finally:
            dev.stop()
            # Restore through SQLite so stale WAL files cannot replay fixture data.
            with sqlite3.connect(backup) as source, sqlite3.connect(path) as target:
                source.backup(target)
            dev.start()
            server.shutdown()
            server.server_close()

if __name__=='__main__':
    main()
