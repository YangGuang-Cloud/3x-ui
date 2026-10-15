import importlib.util
import unittest
from pathlib import Path


def module():
    spec = importlib.util.spec_from_file_location('cutover', Path(__file__).with_name('cutover.py'))
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


class CounterMergeTests(unittest.TestCase):
    def test_counts_staging_delta_once(self):
        self.assertEqual(module().merge_counter(130, 170, 100), 200)
        self.assertEqual(module().merge_counter(130, 100, 100), 130)

    def test_refuses_reset_or_negative_counters(self):
        for args in [(99, 170, 100), (130, 99, 100), (0, 0, -1)]:
            with self.assertRaisesRegex(ValueError, 'counter'):
                module().merge_counter(*args)

    def test_refuses_live_config_changes_but_allows_traffic_updates(self):
        mod = module()
        original = {'id': 1, 'settings': 'identity', 'port': 443, 'up': 100, 'down': 200}
        mod.verify_unchanged(original, dict(original, up=150))
        with self.assertRaisesRegex(ValueError, 'configuration'):
            mod.verify_unchanged(original, dict(original, settings='changed-identity'))


if __name__ == '__main__':
    unittest.main()
