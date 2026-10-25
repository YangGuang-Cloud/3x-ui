#!/usr/bin/env python3
"""Smoke test the actual isolated panel/core. Does not contact production."""
import http.server, json, socket, struct, threading, time, uuid
import dev

BODY = b'local-development-test\n' * 50000
class Handler(http.server.BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass
    def do_GET(self):
        self.send_response(200)
        self.send_header('Content-Length', str(len(BODY)))
        self.end_headers()
        self.wfile.write(BODY)

def main():
    request = dev.api()
    inbound = next(i for i in request('/panel/api/inbounds/list')['obj'] if i['port'] == 18190)
    client = next(c for c in (json.loads(inbound['settings']) if isinstance(inbound['settings'], str) else inbound['settings'])['clients'] if c['email'] == 'local-test')
    path = '/panel/api/clients/local-test/bandwidth'
    original = request(path)['obj']
    assert original['supported'], original
    server = http.server.ThreadingHTTPServer(('127.0.0.1', 0), Handler)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    try:
        request(path, {'uploadMbps': 2, 'downloadMbps': 2})
        time.sleep(2)
        value = request(path)['obj']
        assert value['uploadMbps'] == value['downloadMbps'] == 2
        started = time.monotonic()
        with socket.create_connection(('127.0.0.1', 18190), timeout=15) as sock:
            # Plain VLESS over loopback; no public server or system proxy is involved.
            sock.sendall(b'\x00' + uuid.UUID(client['id']).bytes + b'\x00\x01' +
                         struct.pack('!H', server.server_port) + b'\x01\x7f\x00\x00\x01' +
                         b'GET / HTTP/1.0\r\nHost: localhost\r\n\r\n')
            result = b''
            while chunk := sock.recv(65536):
                result += chunk
                if b"\r\n\r\n" in result and len(result.split(b"\r\n\r\n", 1)[1]) >= len(BODY):
                    break
        elapsed = time.monotonic() - started
        assert result[:2] == b'\x00\x00'
        assert result[2:].split(b'\r\n\r\n', 1)[1] == BODY
        mbps = len(BODY)*8/elapsed/1e6
        assert 1.4 < mbps < 2.6, mbps
        print(f'PASS login, test client, limit save/read, actual VLESS transfer: {mbps:.2f} Mbps (configured 2)')
    finally:
        request(path, {k: original[k] for k in ('uploadMbps', 'downloadMbps')})
        request('/logout', {})
        server.shutdown()
        server.server_close()

if __name__ == '__main__':
    main()
