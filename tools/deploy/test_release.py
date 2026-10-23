import importlib.util
import io
import json
import sqlite3
import tarfile
import tempfile
import unittest
from unittest import mock
from pathlib import Path


def module():
    spec = importlib.util.spec_from_file_location('installer', Path(__file__).with_name('install_release.py'))
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


class DeploymentTests(unittest.TestCase):
    def test_failed_stop_still_attempts_recovery(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder).resolve()
            old, new = root/'old', root/'new'
            old.mkdir(); new.mkdir()
            runtime = root/'runtime'; runtime.symlink_to(old)
            events = []
            def service(action):
                events.append(action)
                if len(events) == 1:
                    raise RuntimeError('Stop timed out after stopping the service')
            with self.assertRaisesRegex(RuntimeError, 'Stop timed out'):
                module().activate(new, runtime, root/'unused.db', root/'unused-backup.db', service,
                                  lambda: False, lambda: True)
            self.assertEqual(events, ['stop', 'stop', 'start'])
            self.assertEqual(runtime.resolve(), old)

    def test_preflight_failure_cannot_stop_or_switch_production(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder).resolve()
            old, new = root/'old', root/'new'
            old.mkdir(); new.mkdir()
            runtime = root/'runtime'; runtime.symlink_to(old)
            events = []
            def reject():
                raise RuntimeError('Candidate not verified')
            with self.assertRaisesRegex(RuntimeError, 'not verified'):
                module().activate(new, runtime, root/'unused.db', root/'backup.db', events.append,
                                  lambda: True, lambda: True, reject)
            self.assertEqual(events, [])
            self.assertEqual(runtime.resolve(), old)
            self.assertFalse((root/'backup.db').exists())

    def test_rollback_must_verify_forwarding(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder).resolve()
            old, new = root/'old', root/'new'
            old.mkdir(); new.mkdir()
            runtime = root/'runtime'; runtime.symlink_to(old)
            db = root/'x-ui.db'
            with sqlite3.connect(db) as c: c.execute('CREATE TABLE nodes(id INTEGER)')
            checked = []
            def rollback():
                checked.append(runtime.resolve())
                return False
            with self.assertRaisesRegex(RuntimeError, 'ROLLBACK FAILED'):
                module().activate(new, runtime, db, root/'backup.db', lambda action: None,
                                  lambda: False, rollback)
            self.assertEqual(checked, [old])

    def test_shadow_configuration_only_binds_private_new_ports(self):
        original = {'api': {'tag': 'api'}, 'metrics': {'listen': '127.0.0.1:9999'},
                    'inbounds': [{'tag': 'api', 'protocol': 'tunnel', 'listen': '127.0.0.1', 'port': 10001},
                                 {'protocol': 'vless', 'listen': '0.0.0.0', 'port': 2087}]}
        installer = module()
        with mock.patch.object(installer, 'loopback_port', side_effect=[30001, 30002, 30003]):
            shadow = installer.isolated_config(original, Path('/private-staging'))
        self.assertEqual(original['inbounds'][1]['listen'], '0.0.0.0')
        self.assertEqual(original['inbounds'][1]['port'], 2087)
        self.assertEqual([i['listen'] for i in shadow['inbounds']], ['127.0.0.1']*2)
        self.assertEqual([i['port'] for i in shadow['inbounds']], [30001, 30002])
        self.assertEqual(shadow['metrics']['listen'], '127.0.0.1:30003')

    def test_unknown_listeners_and_transports_fail_closed(self):
        installer = module()
        for config in [{'inbounds': [{'protocol': 'socks'}]}, {'reverse': {}}, {'dns': {'listen': '0.0.0.0:53'}}]:
            with self.subTest(config=config), self.assertRaises(RuntimeError):
                installer.isolated_config(config, Path('/private-staging'))
        with self.assertRaisesRegex(RuntimeError, 'Transport'):
            installer.probe_client({'inbounds': [{'protocol': 'vless', 'streamSettings': {'network': 'xhttp'}}]})

    def test_failed_config_preflight_never_starts_a_core(self):
        installer = module()
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            config = root/'config.json'; config.write_text('{}')
            with mock.patch.object(installer.subprocess, 'run', side_effect=installer.subprocess.CalledProcessError(23, ['xray'])), mock.patch.object(installer.subprocess, 'Popen') as start:
                with self.assertRaises(installer.subprocess.CalledProcessError):
                    installer.preflight(root, config, root, root)
                start.assert_not_called()
            self.assertEqual(config.read_text(), '{}')

    def test_relocated_core_and_rollback_keep_shared_geodata(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder).resolve()
            old, new, shared = root/'old', root/'new', root/'shared'
            for directory in (old, new, shared): directory.mkdir()
            (shared/'geoip.dat').write_bytes(b'geodata-v1')
            (shared/'xray-linux-amd64').write_bytes(b'original-core')
            runtime = root/'runtime'; runtime.symlink_to(old)
            installer = module()
            installer.prepare_asset_links(new, shared)
            installer.prepare_asset_links(old, shared)
            for release in (old, new):
                self.assertEqual((release/'geoip.dat').read_bytes(), b'geodata-v1')
            (shared/'geoip.dat').write_bytes(b'geodata-v2')
            installer.prepare_asset_links(old, shared)
            for release in (old, new):
                self.assertEqual((release/'geoip.dat').read_bytes(), b'geodata-v2')

    def test_children_include_processes_started_by_worker_threads(self):
        with tempfile.TemporaryDirectory() as folder:
            proc = Path(folder)
            task = proc/'100/task'
            for tid, children in [('100', ''), ('101', '200 201'), ('102', '201')]:
                (task/tid).mkdir(parents=True)
                (task/tid/'children').write_text(children)
            self.assertEqual(module().child_pids('100', proc), {'200', '201'})

    def test_failed_health_restores_sqlite_schema_data_and_runtime(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder).resolve()
            old, new = root/'old', root/'new'
            old.mkdir(); new.mkdir()
            runtime = root/'runtime'
            runtime.symlink_to(old)
            db = root/'x-ui.db'
            with sqlite3.connect(db) as c:
                c.execute('CREATE TABLE nodes(name TEXT)')
                c.execute("INSERT INTO nodes VALUES('preserved')")
            starts = []
            def service(action):
                if action == 'start':
                    starts.append(runtime.resolve())
                    if runtime.resolve() == new:
                        with sqlite3.connect(db) as c:
                            c.execute('ALTER TABLE nodes ADD COLUMN migrated INTEGER')
                            c.execute("UPDATE nodes SET name='changed'")
            with self.assertRaisesRegex(RuntimeError, 'health'):
                module().activate(new, runtime, db, root/'backup.db', service, lambda: False, lambda: True)
            self.assertEqual(runtime.resolve(), old)
            self.assertEqual(starts, [new, old])
            with sqlite3.connect(db) as c:
                self.assertEqual(c.execute('SELECT * FROM nodes').fetchall(), [('preserved',)])
                self.assertEqual([r[1] for r in c.execute('PRAGMA table_info(nodes)')], ['name'])

    def test_success_keeps_release_and_backup(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder).resolve()
            old, new = root/'old', root/'new'
            old.mkdir(); new.mkdir()
            runtime = root/'runtime'; runtime.symlink_to(old)
            db = root/'x-ui.db'
            with sqlite3.connect(db) as c: c.execute('CREATE TABLE nodes(id INTEGER)')
            events = []
            module().activate(new, runtime, db, root/'backup.db', events.append, lambda: True, lambda: True)
            self.assertEqual(runtime.resolve(), new)
            self.assertEqual(events, ['stop', 'start'])
            with sqlite3.connect(root/'backup.db') as c:
                self.assertEqual(c.execute('SELECT name FROM sqlite_master WHERE type="table"').fetchall(), [('nodes',)])

    def test_core_tracks_runtime_and_rolls_back_with_panel(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder).resolve()
            old, new = root/'old', root/'new'
            old.mkdir(); new.mkdir()
            shared = root/'shared-core'; (old/'xray-linux-amd64').write_bytes(b'original-core')
            (new/'xray-linux-amd64').write_bytes(b'new-core')
            runtime = root/'runtime'; runtime.symlink_to(old)
            installer = module()
            shared.symlink_to(runtime/'xray-linux-amd64')
            self.assertEqual(shared.read_bytes(), b'original-core')
            db = root/'x-ui.db'
            with sqlite3.connect(db) as c: c.execute('CREATE TABLE nodes(id INTEGER)')
            observed = []
            def service(action):
                if action == 'start': observed.append(shared.read_bytes())
            with self.assertRaisesRegex(RuntimeError, 'health'):
                installer.activate(new, runtime, db, root/'backup.db', service, lambda: False, lambda: True)
            self.assertEqual(observed, [b'new-core', b'original-core'])
            self.assertEqual(shared.read_bytes(), b'original-core')

    def test_archive_cannot_escape_or_include_database(self):
        for name in ['../escape', 'x-ui.db']:
            with self.subTest(name=name), tempfile.TemporaryDirectory() as folder:
                root = Path(folder).resolve()
                archive = io.BytesIO()
                with tarfile.open(fileobj=archive, mode='w:gz') as bundle:
                    item = tarfile.TarInfo(name); item.size = 1
                    bundle.addfile(item, io.BytesIO(b'x'))
                archive.seek(0)
                with self.assertRaisesRegex(ValueError, 'member'):
                    module().extract_release(archive, root/'release', 'a'*40)
                self.assertFalse((root/'escape').exists())
                self.assertFalse((root/'release/x-ui.db').exists())


if __name__ == '__main__':
    unittest.main()
