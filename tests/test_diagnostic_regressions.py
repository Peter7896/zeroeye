"""Temporary fixture bytes test metadata contracts, not encrypted build evidence."""
from __future__ import annotations

import json
import tempfile
import unittest
from pathlib import Path, PureWindowsPath
from unittest import mock

import build
import test_diagnostic as diagnostic


class DiagnosticRegressionTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.directory = self.root / "diagnostic"
        self.directory.mkdir()
        self.metadata = self.directory / "build-ABC.json"
        self.logd = self.directory / "build-ABC.logd"
        self.logd.write_bytes(b"unit-test fixture only; not encrypted output")
        self.report = {
            "commit": "ABC", "diagnostic_logd": "diagnostic/build-ABC.logd",
            "modules": [{"name": "market", "status": "PASS", "artifact": "market/bin/app"}],
        }
        for patcher in (
            mock.patch.object(build.subprocess, "run", side_effect=AssertionError("unexpected process")),
            mock.patch.object(build.Path, "home", return_value=self.root / "fixture-home"),
            mock.patch.object(build.getpass, "getuser", return_value="fixture-user"),
            mock.patch.object(build.platform, "node", return_value="fixture-host"),
            mock.patch.dict("os.environ", {"USER": "fixture-user", "USERNAME": "fixture-user"}),
            mock.patch.object(diagnostic, "REPO_ROOT", self.root),
        ):
            patcher.start()
            self.addCleanup(patcher.stop)

    def validate(self):
        self.metadata.write_text(json.dumps(self.report), encoding="utf-8")
        return diagnostic.validate_diagnostic_directory(self.directory)

    def test_missing_reference_cannot_be_replaced_by_a_note(self):
        self.report.pop("diagnostic_logd")
        self.report["note"] = "build-ABC.logd is mentioned but not referenced"
        with self.assertRaises(AssertionError):
            self.validate()

    def test_wrong_reference_cannot_be_replaced_by_a_note(self):
        self.report["diagnostic_logd"] = "diagnostic/build-OTHER.logd"
        self.report["note"] = "build-ABC.logd is mentioned but not referenced"
        with self.assertRaises(AssertionError):
            self.validate()

    def test_traversal_reference_is_rejected(self):
        for reference in ("../outside/build-ABC.logd", "diagnostic/../outside/build-ABC.logd"):
            with self.subTest(reference=reference):
                self.report["diagnostic_logd"] = reference
                with self.assertRaises(AssertionError):
                    self.validate()

    def test_symlink_reference_must_stay_inside_diagnostic(self):
        target = self.root / "outside.logd"
        target.write_bytes(b"unit-test fixture")
        self.logd.unlink()
        try:
            self.logd.symlink_to(target)
        except OSError as exc:
            self.skipTest(f"symlinks unavailable: {exc}")
        with self.assertRaises(AssertionError):
            self.validate()

    def test_valid_ordered_chunks_are_accepted(self):
        self.logd.unlink()
        names = ["build-ABC-part001.logd", "build-ABC-part002.logd"]
        for name in names:
            (self.directory / name).write_bytes(b"unit-test fixture")
        self.report["diagnostic_logd"] = ["diagnostic/" + name for name in names]
        self.report["chunked"] = True
        json_files, logd_files = self.validate()
        self.assertEqual(json_files, [self.metadata])
        self.assertEqual([p.name for p in logd_files], names)

    def test_empty_logd_is_rejected(self):
        self.logd.write_bytes(b"")
        with self.assertRaises(AssertionError):
            self.validate()

    def test_metadata_commit_must_match_filenames(self):
        self.report["commit"] = "OTHER"
        with self.assertRaises(AssertionError):
            self.validate()

    def test_module_artifact_must_not_traverse(self):
        self.report["modules"][0]["artifact"] = "bin/../../outside"
        with self.assertRaises(AssertionError):
            self.validate()

    def test_real_report_redacts_nonempty_success_and_failure_results(self):
        for root, home, temp, binary, expected in (
            (Path("/fixture-home/fixture-user/repo"), Path("/fixture-home/fixture-user"),
             "/fixture-temp", "/fixture-home/fixture-user/repo/bin/app", "bin/app"),
            (PureWindowsPath(r"C:\Users\fixture-user\repo"), PureWindowsPath(r"C:\Users\fixture-user"),
             r"C:\FixtureTemp", r"c:\users\fixture-user\repo\bin\app.exe", "bin/app.exe"),
        ):
            with self.subTest(root=str(root)), mock.patch.object(build, "ROOT", root), \
                 mock.patch.object(build.Path, "home", return_value=home), \
                 mock.patch.object(tempfile, "gettempdir", return_value=temp):
                output = f"{root}/src {home}/other {temp}/work user=fixture-user host=fixture-host"
                report = build.build_diagnostic_report(
                    [("pass", True, 1.23456, output, binary), ("fail", False, 2, output, None)],
                    "ABC", logd_error=output,
                )
                self.assertEqual((report["total_modules"], report["passed"], report["failed"]), (2, 1, 1))
                self.assertEqual(report["modules"][0]["artifact"], expected)
                self.assertEqual(report["modules"][0]["elapsed_seconds"], 1.235)
                self.assertIsNone(report["modules"][1]["artifact"])
                for value in [m["output"] for m in report["modules"]] + [report["diagnostic_logd_error"]]:
                    for token in (str(root), str(home), temp, "fixture-user", "fixture-host"):
                        self.assertNotIn(token.casefold(), value.casefold())
                    for label in ("<REPO>", "<HOME>", "<TEMP>", "<USER>", "<HOST>"):
                        self.assertIn(label, value)

    def test_real_report_omits_outside_artifacts(self):
        with mock.patch.object(build, "ROOT", self.root):
            for artifact in ("/outside/bin/app", "../outside/app", r"C:\outside\app", r"C:relative\app"):
                with self.subTest(artifact=artifact):
                    report = build.build_diagnostic_report([("module", False, 0, "failed", artifact)], "ABC")
                    self.assertIsNone(report["modules"][0]["artifact"])

    def test_real_report_normalizes_windows_chunk_references(self):
        root = PureWindowsPath(r"C:\repo")
        with mock.patch.object(build, "ROOT", root), \
             mock.patch.object(build, "DIAGNOSTIC_DIR", root / "diagnostic"):
            report = build.build_diagnostic_report(
                [("module", True, 1, "ok", None)], "ABC",
                [r"diagnostic\build-ABC-part001.logd", r"diagnostic\build-ABC-part002.logd"],
                password="fixture-password", chunked=True,
            )
        self.assertEqual(report["diagnostic_logd"], ["diagnostic/build-ABC-part001.logd", "diagnostic/build-ABC-part002.logd"])
        self.assertNotIn("\\", report["decrypt_command"])
        self.assertNotIn("\\", report["pr_note"])

    def test_real_report_serialization_matches_bundle_contract(self):
        with mock.patch.object(build, "ROOT", self.root), \
             mock.patch.object(build, "DIAGNOSTIC_DIR", self.directory):
            report = build.build_diagnostic_report(
                [("pass", True, 1, "built " + str(self.root / "src"), str(self.root / "bin/app")),
                 ("fail", False, 2, "failed as fixture-user", None)],
                "ABC", ["diagnostic/build-ABC.logd"],
            )
            build.write_diagnostic_report(self.metadata, report)
        self.assertEqual(diagnostic.validate_diagnostic_directory(self.directory), ([self.metadata], [self.logd]))
        saved = json.loads(self.metadata.read_text(encoding="utf-8"))
        self.assertEqual((saved["passed"], saved["failed"]), (1, 1))
        self.assertEqual(saved["modules"][0]["output"], "built <REPO>/src")
        self.assertEqual(saved["modules"][1]["output"], "failed as <USER>")

    def test_real_generation_records_missing_encryptly_without_running_it(self):
        self.logd.unlink()
        with mock.patch.object(build, "ROOT", self.root), \
             mock.patch.object(build, "DIAGNOSTIC_DIR", self.directory), \
             mock.patch.object(build, "current_commit_id", return_value="ABC"), \
             mock.patch.object(build, "get_encryptly_bin", return_value=None):
            self.assertFalse(build.generate_logd([
                ("pass", True, 1, "built " + str(self.root / "src"), str(self.root / "bin/app")),
                ("fail", False, 2, "failed as fixture-user", None),
            ]))
        saved = json.loads(self.metadata.read_text(encoding="utf-8"))
        self.assertIsNone(saved["diagnostic_logd"])
        self.assertIn("encryptly binary not found", saved["diagnostic_logd_error"])
        self.assertEqual((saved["passed"], saved["failed"]), (1, 1))
        self.assertNotIn(str(self.root), self.metadata.read_text(encoding="utf-8"))
        self.assertFalse(self.logd.exists())
        with self.assertRaisesRegex(AssertionError, "Missing diagnostic .logd"):
            diagnostic.validate_diagnostic_directory(self.directory)

    def test_windows_identity_case_variants_are_redacted(self):
        with mock.patch.object(build, "ROOT", PureWindowsPath(r"C:\Users\Alice\repo")), \
             mock.patch.object(build.Path, "home", return_value=PureWindowsPath(r"C:\Users\Alice")), \
             mock.patch.object(build.getpass, "getuser", return_value="Alice"), \
             mock.patch.object(build.platform, "node", return_value="WorkStation"):
            output = r"user=ALICE host=WORKSTATION file=c:\users\ALICE\repo\src"
            report = build.build_diagnostic_report([("pass", True, 1, output, None)], "ABC", logd_error=output)
        for text in (report["modules"][0]["output"], report["diagnostic_logd_error"]):
            self.assertEqual(text, r"user=<USER> host=<HOST> file=<REPO>\src")

    def test_windows_identity_case_variants_are_rejected_by_validator(self):
        with mock.patch.object(build.platform, "system", return_value="Windows"), \
             mock.patch.object(build.getpass, "getuser", return_value="Alice"), \
             mock.patch.object(build.platform, "node", return_value="WorkStation"):
            for output in ("user=ALICE", "host=WORKSTATION"):
                with self.subTest(output=output):
                    self.report["modules"][0]["output"] = output
                    with self.assertRaisesRegex(AssertionError, "leaks local value"):
                        self.validate()

    def outside_artifact_link(self):
        outside = self.root.parent / (self.root.name + "-outside")
        outside.mkdir()
        self.addCleanup(outside.rmdir)
        artifact = outside / "app"
        artifact.write_bytes(b"unit-test fixture")
        self.addCleanup(artifact.unlink)
        link = self.root / "link"
        try:
            link.symlink_to(outside, target_is_directory=True)
        except OSError as exc:
            self.skipTest(f"symlinks unavailable: {exc}")
        return link / "app"

    def test_real_report_omits_symlink_escaped_module_artifact(self):
        artifact = self.outside_artifact_link()
        with mock.patch.object(build, "ROOT", self.root):
            report = build.build_diagnostic_report([("pass", True, 1, "ok", str(artifact))], "ABC")
        self.assertIsNone(report["modules"][0]["artifact"])

    def test_validator_rejects_symlink_escaped_module_artifact(self):
        self.outside_artifact_link()
        self.report["modules"][0]["artifact"] = "link/app"
        with self.assertRaisesRegex(AssertionError, "module artifact resolves outside repository"):
            self.validate()


if __name__ == "__main__":
    unittest.main()
