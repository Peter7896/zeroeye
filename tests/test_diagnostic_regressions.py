"""Reference fixtures use fake bytes; producer tests use the audited safe packer."""
import json
import tempfile
import unittest
from contextlib import ExitStack
from pathlib import Path, PureWindowsPath
from unittest import mock

import build
from tools.verify_diagnostic_bundle import validate_diagnostic_bundle


class DiagnosticRegressionTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.diagnostic = self.root / 'diagnostic'
        self.diagnostic.mkdir()
        self.metadata = self.diagnostic / 'build-12345678.json'
        self.logd = self.diagnostic / 'build-12345678.logd'
        self.logd.write_bytes(b'fake bundle for reference validation only')

    def report(self, **extra):
        report = {'commit': '12345678', 'diagnostic_logd': 'diagnostic/build-12345678.logd', 'modules': []}
        report.update(extra)
        self.metadata.write_text(json.dumps(report), encoding='utf-8')
        return validate_diagnostic_bundle(self.metadata, self.root)

    def test_non_object_and_invalid_json_fail_clearly(self):
        for content in ('[]', 'null', '42', '{', '\ud800'):
            with self.subTest(content=repr(content)):
                self.metadata.write_bytes(content.encode('utf-8', errors='surrogatepass'))
                self.assertTrue(validate_diagnostic_bundle(self.metadata, self.root))

    def test_invalid_reference_types(self):
        for value in (None, '', [], [None], [1], {}, ['valid', None]):
            with self.subTest(value=value):
                self.assertTrue(self.report(diagnostic_logd=value))

    def test_commit_must_match_json_and_logd_filenames(self):
        for commit in (None, '', 'deadbeef', 12345678):
            with self.subTest(commit=commit):
                self.assertTrue(any('does not match its commit' in e for e in self.report(commit=commit)))

    def test_module_artifact_must_be_relative_posix(self):
        for value in ('/outside/bin/app', 'C:/outside/app', r'C:\outside\app', '../outside/app', 'bin/../app', '.', 1):
            with self.subTest(value=value):
                self.assertTrue(self.report(modules=[{'artifact': value}]))
        self.assertEqual(self.report(modules=[{'artifact': 'market/bin/app.exe'}]), [])

    def test_invalid_chunk_names_and_missing_indices(self):
        for names in (['build-12345678-partbanana.logd'], ['build-12345678-part000.logd'],
                      ['build-12345678-part002.logd'], ['build-12345678-part0001.logd'],
                      ['build-12345678-part001.logd', 'build-12345678-part003.logd']):
            with self.subTest(names=names):
                for name in names:
                    (self.diagnostic / name).write_bytes(b'fixture')
                self.assertTrue(self.report(diagnostic_logd=['diagnostic/' + n for n in names]))

    def test_duplicate_and_mixed_references(self):
        chunk = self.diagnostic / 'build-12345678-part001.logd'
        chunk.write_bytes(b'fixture')
        single = 'diagnostic/' + self.logd.name
        part = 'diagnostic/' + chunk.name
        self.assertTrue(self.report(diagnostic_logd=[single, single]))
        self.assertTrue(self.report(diagnostic_logd=[single, part]))
        self.assertTrue(self.report(diagnostic_logd=[part, part]))

    def test_empty_or_directory_artifact_rejected(self):
        self.logd.write_bytes(b'')
        self.assertTrue(self.report())
        self.logd.unlink()
        self.logd.mkdir()
        self.assertTrue(self.report())

    def test_symlink_outside_repository_rejected(self):
        with tempfile.TemporaryDirectory() as outside:
            target = Path(outside) / 'file.logd'
            target.write_bytes(b'fixture')
            self.logd.unlink()
            try:
                self.logd.symlink_to(target)
            except OSError as exc:
                self.skipTest(f'symlinks unavailable: {exc}')
            self.assertTrue(self.report())

    def test_generation_redacts_home_repo_temp_user_and_errors(self):
        for root, home, temp, artifact, expected in (
            (Path('/home/al/repo'), Path('/home/al'), '/tmp', '/home/al/repo/bin/app', 'bin/app'),
            (PureWindowsPath(r'C:\Users\al\repo'), PureWindowsPath(r'C:\Users\al'), r'C:\Temp',
             r'c:\users\al\repo\bin\app.exe', 'bin/app.exe'),
        ):
            with self.subTest(root=str(root)), mock.patch.object(build, 'ROOT', root), \
                 mock.patch.object(build.Path, 'home', return_value=home), \
                 mock.patch.object(build.tempfile, 'gettempdir', return_value=temp), \
                 mock.patch.object(build.getpass, 'getuser', return_value='al'), \
                 mock.patch.object(build.platform, 'node', return_value='test-host'):
                output = f'{root}/src {home}/other {temp}/work user=al host=test-host'
                report = build.build_diagnostic_report([('module', False, 1, output, artifact)], '12345678',
                                                       logd_error=output)
                module = report['modules'][0]
                self.assertEqual(module['artifact'], expected)
                for text in (module['output'], report['diagnostic_logd_error']):
                    self.assertIn('<REPO>', text)
                    self.assertIn('<HOME>', text)
                    self.assertIn('<TEMP>', text)
                    self.assertIn('<USER>', text)
                    self.assertIn('<HOST>', text)
                    self.assertNotIn(str(root), text)
                    self.assertNotIn(str(home), text)
                self.assertEqual(build._metadata_redact('normal validation'), 'normal validation')
                self.assertIsNone(build._metadata_artifact('/outside/app'))
                self.assertIsNone(build._metadata_artifact(r'\outside\app.exe'))
                self.assertIsNone(build._metadata_artifact(r'C:relative\app.exe'))

    def test_validator_catches_short_username_without_echoing_it(self):
        with mock.patch('tools.verify_diagnostic_bundle.getpass.getuser', return_value='xy'):
            errors = self.report(output='user=xy')
        self.assertTrue(any('leaks local value' in e for e in errors))
        self.assertFalse(any('xy' in e for e in errors))

    def test_generate_logd_contract_with_safe_pack(self):
        from tools.safe_logd import unpack_diagnostic_texts
        fake_home = self.root / 'home'
        fake_home.mkdir()
        self.logd.unlink()  # Preserve the producer's no-overwrite evidence rule.
        state = {'commit': '12345678' + 'a' * 32, 'tree': 'b' * 40, 'dirty': False}
        with ExitStack() as stack:
            stack.enter_context(mock.patch.object(build, 'ROOT', self.root))
            stack.enter_context(mock.patch.object(build, 'DIAGNOSTIC_DIR', self.diagnostic))
            stack.enter_context(mock.patch.object(build.Path, 'home', return_value=fake_home))
            stack.enter_context(mock.patch.object(build, 'collect_system_info', return_value='fixture'))
            stack.enter_context(mock.patch.object(build.subprocess, 'run', side_effect=AssertionError('unexpected process')))
            self.assertTrue(build.generate_logd(
                [('module', True, 1, 'source ' + str(self.root / 'src'), str(self.root / 'bin/app'))],
                provenance={'before': state.copy(), 'after': state.copy()}))
            self.assertEqual(validate_diagnostic_bundle(self.metadata, self.root), [])
        report = json.loads(self.metadata.read_text())
        self.assertEqual(report['modules'][0]['artifact'], 'bin/app')
        self.assertEqual(report['diagnostic_logd'], 'diagnostic/build-12345678.logd')
        self.assertEqual(report['format'], 'safe-logd-v1')
        texts = unpack_diagnostic_texts(self.logd, report['password'])
        self.assertIn('source <REPO>/src', texts['build.log'])
        self.assertEqual(set(texts), {'build.log', 'build-summary.txt', 'system-info.txt'})
        self.assertFalse((fake_home / '.cache/tent-of-trials/logd-workspace').exists())

    def test_missing_safe_packer_records_clear_failure_without_execution(self):
        self.logd.unlink()
        state = {'commit': '12345678' + 'a' * 32, 'tree': 'b' * 40, 'dirty': False}
        with mock.patch.object(build, 'ROOT', self.root), \
             mock.patch.object(build, 'DIAGNOSTIC_DIR', self.diagnostic), \
             mock.patch('tools.safe_logd.pack_diagnostic_texts', side_effect=ImportError('fixture unavailable safe packer')), \
             mock.patch.object(build.subprocess, 'run', side_effect=AssertionError('unexpected process')):
            self.assertFalse(build.generate_logd([], provenance={'before': state.copy(), 'after': state.copy()}))
        report = json.loads(self.metadata.read_text())
        self.assertIsNone(report['diagnostic_logd'])
        self.assertIn('unavailable safe packer', report['diagnostic_logd_error'])
        self.assertTrue(validate_diagnostic_bundle(self.metadata, self.root))


if __name__ == '__main__':
    unittest.main()
