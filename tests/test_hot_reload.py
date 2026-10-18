import asyncio
import subprocess
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from hot_reload import Reloader


ROOT = Path(__file__).parents[1]
VALID = """upstreams:\n  app: 127.0.0.1:3000\nhot_reload:\n  debounce: 0.01s\n  poll_interval: 0.01s\nservers:\n  - name: test.local\n    routes:\n      - {path: /, upstream: app}\n"""


class ReloadTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.base = Path(self.temp.name)
        self.config = self.base / "config.yaml"
        self.config.write_text(VALID)
        self.active = self.base / "nginx.conf"
        self.reloader = Reloader(self.config, ROOT, self.active, self.base / "state")

    def tearDown(self): self.temp.cleanup()

    def nginx(self, args, **_):
        return subprocess.CompletedProcess(args, 0, "", "")

    @patch("hot_reload.subprocess.run")
    def test_valid_change_installs_and_reloads(self, run):
        run.side_effect = self.nginx
        self.assertTrue(self.reloader.apply(initial=True))
        self.config.write_text(VALID.replace("3000", "3001"))
        self.assertTrue(self.reloader.apply())
        self.assertIn("3001", self.active.read_text())
        self.assertTrue(any(call.args[0] == ["nginx", "-s", "reload"] for call in run.call_args_list))

    @patch("hot_reload.subprocess.run")
    def test_invalid_yaml_keeps_active_file_and_watcher_can_recover(self, run):
        run.side_effect = self.nginx; self.reloader.apply(initial=True); before = self.active.read_text()
        self.config.write_text("upstreams: [broken")
        with self.assertRaises(Exception): self.reloader.apply()
        self.assertEqual(before, self.active.read_text())
        self.config.write_text(VALID.replace("3000", "3002")); self.reloader.apply()
        self.assertIn("3002", self.active.read_text())

    @patch("hot_reload.subprocess.run")
    def test_invalid_nginx_candidate_never_replaces_active(self, run):
        def result(args, **_):
            return subprocess.CompletedProcess(args, 1 if "-t" in args else 0, "", "bad syntax")
        run.side_effect = result; self.active.write_text("known-good")
        with self.assertRaises(RuntimeError): self.reloader.apply()
        self.assertEqual("known-good", self.active.read_text())

    @patch("hot_reload.subprocess.run")
    def test_identical_generated_config_does_not_reload(self, run):
        run.side_effect = self.nginx; self.reloader.apply(initial=True); run.reset_mock()
        self.assertFalse(self.reloader.apply())
        run.assert_not_called()

    @patch("hot_reload.subprocess.run")
    def test_reload_failure_restores_prior_config(self, run):
        run.side_effect = self.nginx; self.reloader.apply(initial=True); before = self.active.read_text()
        self.config.write_text(VALID.replace("3000", "3004"))
        run.side_effect = [self.nginx(["nginx", "-t"]), subprocess.CompletedProcess(["nginx", "-s", "reload"], 1, "", "signal failed"), self.nginx(["nginx", "-s", "reload"])]
        with self.assertRaises(RuntimeError): self.reloader.apply()
        self.assertEqual(before, self.active.read_text())

    @patch("hot_reload.subprocess.run")
    def test_atomic_replacement_hash_is_detected(self, run):
        run.side_effect = self.nginx
        async def case():
            watcher = asyncio.create_task(self.reloader.watch())
            await asyncio.sleep(.02)
            replacement = self.base / "replacement"; replacement.write_text(VALID.replace("3000", "3010")); replacement.replace(self.config)
            await asyncio.sleep(.15); watcher.cancel()
            await asyncio.gather(watcher, return_exceptions=True)
        asyncio.run(case())
        self.assertIn("3010", self.active.read_text())
