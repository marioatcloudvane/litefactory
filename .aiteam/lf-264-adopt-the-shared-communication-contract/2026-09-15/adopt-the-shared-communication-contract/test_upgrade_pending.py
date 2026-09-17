"""Offline operator-script checks: temporary files and a fake factory only."""
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

SCRIPTS = Path(__file__).resolve().parent
FAKE = '''#!/usr/bin/env python3
import json, os, sys
from pathlib import Path
with open(os.environ['FAKE_CALLS'], 'a') as stream:
    stream.write(json.dumps(sys.argv[1:]) + '\\n')
action = sys.argv[-1][2:]
print(os.environ.get('FAKE_PREVIEW', 'revision 123: unfinished scope') if action == 'preview' else 'applied')
print('diagnostic ' + action, file=sys.stderr)
sys.exit(int(os.environ.get('FAKE_' + action.upper() + '_EXIT', '0')))
'''


class UpgradePendingTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='upgrade tests ')
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.exe = self.root / 'fake factory'
        self.exe.write_text(FAKE)
        self.exe.chmod(0o700)
        self.calls = self.root / 'calls.jsonl'
        self.output = self.root / 'evidence with spaces'
        self.env = dict(os.environ, FAKE_CALLS=str(self.calls),
                        LF_HOME=str(self.root / 'runtime'))

    def run_script(self, issue='230', extra=(), env=None):
        return subprocess.run(
            ['sh', str(SCRIPTS / ('upgrade-' + issue + '.sh')),
             '--executable', str(self.exe), '--output-dir', str(self.output), *extra],
            env=env or self.env, capture_output=True, text=True, timeout=10)

    def invoked(self):
        return [json.loads(line) for line in self.calls.read_text().splitlines()] if self.calls.exists() else []

    def preview(self, issue='230'):
        before = set(self.output.glob('*/preview.json'))
        result = self.run_script(issue)
        self.assertEqual(result.returncode, 0, result.stderr)
        return (set(self.output.glob('*/preview.json')) - before).pop()

    def test_each_wrapper_previews_only_its_fixed_target(self):
        for issue in ('230', '259'):
            with self.subTest(issue=issue):
                path = self.preview(issue)
                record = json.loads(path.read_text())
                self.assertEqual(record['task'], 'github:marioatcloudvane/litefactory#' + issue)
                self.assertEqual(record['exit_code'], 0)
                self.assertEqual(record['stdout'], 'revision 123: unfinished scope\n')
                self.assertEqual(record['stderr'], 'diagnostic preview\n')
                self.assertEqual(self.invoked()[-1], ['story-contract', 'upgrade', record['task'], '--preview'])
                self.assertFalse((path.parent / 'apply.json').exists())
                self.assertEqual(path.stat().st_mode & 0o077, 0)

    def test_target_and_cli_bypass_options_reject_before_call(self):
        for extra in [('259',), ('--issue', '259'), ('--force',), ('--apply',),
                      ('--reviewed-preview', 'missing.json'), ('--timeout', '0')]:
            with self.subTest(extra=extra):
                self.assertNotEqual(self.run_script(extra=extra).returncode, 0)
                self.assertEqual(self.invoked(), [])

    def test_runtime_evidence_directory_rejected(self):
        result = self.run_script(extra=('--output-dir', str(Path(self.env['LF_HOME']) / 'evidence')))
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(Path(self.env['LF_HOME']).exists())
        self.assertEqual(self.invoked(), [])

    def test_reviewed_match_previews_then_applies_and_retains_observations(self):
        path = self.preview()
        original = path.read_bytes()
        result = self.run_script(extra=('--apply', '--reviewed-preview', str(path)))
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual([call[-1] for call in self.invoked()], ['--preview', '--preview', '--apply'])
        applied, = self.output.glob('*/apply.json')
        record = json.loads(applied.read_text())
        self.assertEqual(record['stdout'], 'applied\n')
        self.assertEqual(record['exit_code'], 0)
        self.assertEqual(path.read_bytes(), original)
        self.assertIn('Publish and approve', result.stdout)

    def test_changed_preview_never_applies(self):
        path = self.preview()
        result = self.run_script(extra=('--apply', '--reviewed-preview', str(path)),
                                 env=dict(self.env, FAKE_PREVIEW='revision 456: changed scope'))
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('Preview changed', result.stderr)
        self.assertEqual([call[-1] for call in self.invoked()], ['--preview', '--preview'])
        self.assertFalse(list(self.output.glob('*/apply.json')))

    def test_mismatched_or_unsuccessful_reviewed_records_reject_before_call(self):
        path = self.preview()
        original = json.loads(path.read_text())
        for key, value in [('task', 'github:marioatcloudvane/litefactory#259'),
                           ('executable_sha256', 'wrong'), ('lf_home', '/wrong'),
                           ('status', 'running'), ('exit_code', 1), ('stdout', ''),
                           ('action', 'apply')]:
            with self.subTest(field=key):
                modified = self.root / 'modified.json'
                modified.write_text(json.dumps(dict(original, **{key: value})))
                result = self.run_script(extra=('--apply', '--reviewed-preview', str(modified)))
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(len(self.invoked()), 1)

    def test_binary_change_invalidates_reviewed_preview(self):
        path = self.preview()
        self.exe.write_text(FAKE + '\n# changed executable\n')
        result = self.run_script(extra=('--apply', '--reviewed-preview', str(path)))
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(len(self.invoked()), 1)

    def test_factory_preview_refusal_retains_diagnostic_and_does_not_apply(self):
        result = self.run_script(env=dict(self.env, FAKE_PREVIEW_EXIT='7'))
        self.assertNotEqual(result.returncode, 0)
        path, = self.output.glob('*/preview.json')
        self.assertEqual(json.loads(path.read_text())['exit_code'], 7)
        self.assertIn('diagnostic preview', result.stderr)
        self.assertEqual([call[-1] for call in self.invoked()], ['--preview'])

    def test_apply_failure_retained_and_explicit_retry_uses_same_target(self):
        path = self.preview('259')
        args = ('--apply', '--reviewed-preview', str(path))
        failed = self.run_script('259', args, dict(self.env, FAKE_APPLY_EXIT='9'))
        self.assertNotEqual(failed.returncode, 0)
        failed_path, = self.output.glob('*/apply.json')
        failed_bytes = failed_path.read_bytes()
        self.assertEqual(json.loads(failed_bytes)['exit_code'], 9)
        retried = self.run_script('259', args)
        self.assertEqual(retried.returncode, 0, retried.stderr)
        self.assertEqual(failed_path.read_bytes(), failed_bytes)
        self.assertEqual(len(list(self.output.glob('*/apply.json'))), 2)
        self.assertTrue(all(call[2] == 'github:marioatcloudvane/litefactory#259' for call in self.invoked()))
        self.assertEqual([call[-1] for call in self.invoked()],
                         ['--preview', '--preview', '--apply', '--preview', '--apply'])

    def test_malformed_reviewed_json_refuses_without_cli_call(self):
        path = self.root / 'bad.json'
        path.write_text('{not JSON')
        result = self.run_script(extra=('--apply', '--reviewed-preview', str(path)))
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.invoked(), [])


if __name__ == '__main__':
    unittest.main(verbosity=2)
