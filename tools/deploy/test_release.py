import importlib.util
import io
import json
import sqlite3
import tarfile
import tempfile
import unittest
from pathlib import Path


def module():
    spec = importlib.util.spec_from_file_location('installer', Path(__file__).with_name('install_release.py'))
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


class DeploymentTests(unittest.TestCase):
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
                module().activate(new, runtime, db, root/'backup.db', service, lambda: False)
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
            module().activate(new, runtime, db, root/'backup.db', events.append, lambda: True)
            self.assertEqual(runtime.resolve(), new)
            self.assertEqual(events, ['stop', 'start'])
            with sqlite3.connect(root/'backup.db') as c:
                self.assertEqual(c.execute('SELECT name FROM sqlite_master WHERE type="table"').fetchall(), [('nodes',)])

    def test_core_tracks_runtime_and_rolls_back_with_panel(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder).resolve()
            old, new = root/'old', root/'new'
            old.mkdir(); new.mkdir()
            shared = root/'shared-core'; shared.write_bytes(b'original-core')
            (new/'xray-linux-amd64').write_bytes(b'new-core')
            runtime = root/'runtime'; runtime.symlink_to(old)
            installer = module()
            installer.prepare_core_link(runtime, shared)
            self.assertEqual(shared.read_bytes(), b'original-core')
            db = root/'x-ui.db'
            with sqlite3.connect(db) as c: c.execute('CREATE TABLE nodes(id INTEGER)')
            observed = []
            def service(action):
                if action == 'start': observed.append(shared.read_bytes())
            with self.assertRaisesRegex(RuntimeError, 'health'):
                installer.activate(new, runtime, db, root/'backup.db', service, lambda: False)
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
