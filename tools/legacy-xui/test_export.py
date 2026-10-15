import importlib.util
import json
import sqlite3
import tempfile
import unittest
from pathlib import Path


class ExportTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.path = Path(self.temp.name) / 'old.db'
        c = sqlite3.connect(self.path)
        c.executescript('''
          CREATE TABLE inbounds(id INTEGER, user_id INTEGER, up INTEGER, down INTEGER, total INTEGER,
            remark TEXT, enable INTEGER, expiry_time INTEGER, autoreset INTEGER, ip_alert INTEGER,
            ip_limit INTEGER, listen TEXT, port INTEGER, protocol TEXT, settings TEXT,
            stream_settings TEXT, tag TEXT, sniffing TEXT);
          CREATE TABLE client_infos(id INTEGER, inbound_id INTEGER, enable INTEGER, email TEXT,
            up INTEGER, down INTEGER, total INTEGER, expiry_time INTEGER);
        ''')
        self.identity = 'f7a94bba-b35a-4708-93ef-fb6c48897c50'
        self.tls = {'network': 'tcp', 'security': 'tls', 'tlsSettings': {'serverName': 'example.com',
                    'certificates': [{'certificateFile': '/test/cert.pem', 'keyFile': '/test/key.pem'}]}}
        c.execute('INSERT INTO inbounds VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)',
                  (1, 1, 10, 20, 500, 'old', 1, 0, 0, 0, 0, '', 14443, 'vless',
                   json.dumps({'clients': [{'id': self.identity, 'email': 'test-user', 'flow': ''}],
                               'decryption': 'none'}), json.dumps(self.tls), 'inbound-443', '{}'))
        c.execute('INSERT INTO client_infos VALUES(1,1,0,?,?,?,?,?)', ('test-user', 10, 20, 500, 1900000000000))
        c.commit()
        c.close()

    def module(self):
        spec = importlib.util.spec_from_file_location('legacy_export', Path(__file__).with_name('export.py'))
        mod = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(mod)
        return mod

    def test_preserves_identity_tls_and_limits_without_writing_source(self):
        before = self.path.read_bytes()
        payloads, report = self.module().convert(self.path, 24443)
        self.assertEqual(self.path.read_bytes(), before)
        payload = payloads[0]
        client = json.loads(payload['settings'])['clients'][0]
        self.assertEqual((payload['port'], payload['id']), (24443, 0))
        self.assertEqual(client['id'], self.identity)
        self.assertFalse(client['enable'])
        self.assertEqual((client['totalGB'], client['expiryTime']), (500, 1900000000000))
        self.assertEqual(json.loads(payload['streamSettings']), self.tls)
        self.assertEqual((payload['clientStats'][0]['up'], payload['clientStats'][0]['down']), (10, 20))
        self.assertNotIn(self.identity, json.dumps(report))
        self.assertNotIn('test-user', json.dumps(report))

    def test_refuses_original_port_and_unsupported_protocol(self):
        mod = self.module()
        with self.assertRaisesRegex(ValueError, 'different'):
            mod.convert(self.path, 14443)
        c = sqlite3.connect(self.path)
        c.execute("UPDATE inbounds SET protocol='vmess'")
        c.commit()
        c.close()
        with self.assertRaisesRegex(ValueError, 'VLESS'):
            mod.convert(self.path, 24443)

    def test_refuses_unmapped_limits_and_orphan_client_traffic(self):
        mod = self.module()
        c = sqlite3.connect(self.path)
        c.execute('UPDATE inbounds SET ip_limit=2')
        c.commit()
        with self.assertRaisesRegex(ValueError, 'manual'):
            mod.convert(self.path, 24443)
        c.execute('UPDATE inbounds SET ip_limit=0')
        c.execute("UPDATE client_infos SET email='orphan'")
        c.commit()
        c.close()
        with self.assertRaisesRegex(ValueError, 'unmatched'):
            mod.convert(self.path, 24443)


if __name__ == '__main__':
    unittest.main()
