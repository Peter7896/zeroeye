"""Synthetic fixtures test the real packer; they are not project build evidence."""
import copy
import hashlib
import io
import json
from pathlib import Path, PureWindowsPath
import subprocess
import sys
import tempfile
import unittest
from contextlib import ExitStack, redirect_stdout
from unittest.mock import patch

import build
from tools.safe_logd import FORMAT, REQUIRED_FILES, unpack_diagnostic_texts


class BuildDiagnosticTests(unittest.TestCase):
    def setUp(self):
        self.stack = ExitStack()
        self.addCleanup(self.stack.close)
        self.tmp = Path(self.stack.enter_context(tempfile.TemporaryDirectory()))
        self.root = self.tmp / "repo"
        self.root.mkdir()
        self.diag = self.root / "diagnostic"
        self.home = self.tmp / "fixture-home"
        self.stack.enter_context(patch.object(build, "ROOT", self.root))
        self.stack.enter_context(patch.object(build, "DIAGNOSTIC_DIR", self.diag))
        self.stack.enter_context(patch.object(build.Path, "home", return_value=self.home))
        self.stack.enter_context(patch.object(build.getpass, "getuser", return_value="fixture-user"))
        self.stack.enter_context(patch.object(build.platform, "node", return_value="fixture-host"))
        self.stack.enter_context(patch.object(build.tempfile, "gettempdir", return_value=str(self.tmp)))
        self.stack.enter_context(redirect_stdout(io.StringIO()))
        self.state = {"commit": "12345678" + "a" * 32, "tree": "b" * 40, "dirty": False}
        self.provenance = {"before": self.state.copy(), "after": self.state.copy()}
        self.metadata = self.diag / "build-12345678.json"
        self.archive = self.diag / "build-12345678.logd"

    def generate(self, results, provenance=None):
        return build.generate_logd(results, provenance=provenance or self.provenance)

    def report_and_texts(self):
        report = json.loads(self.metadata.read_text())
        return report, unpack_diagnostic_texts(self.archive, report["password"])

    def test_real_bundle_redacts_all_three_texts_and_preserves_failures(self):
        identifiers = f"{self.root}/src {self.home}/cache {self.tmp}/other fixture-user fixture-host"
        results = [("backend", True, 0.1, "fixture success " + identifiers,
                    str(self.root / "backend/target/debug/server")),
                   ("frontend", False, 0.2, "fixture failure " + identifiers, None)]
        with patch.object(build, "collect_system_info", return_value="fixture runtime " + identifiers):
            self.assertTrue(self.generate(results))
        report, texts = self.report_and_texts()
        self.assertEqual(set(texts), set(REQUIRED_FILES))
        self.assertEqual(report["format"], FORMAT)
        self.assertEqual((report["passed"], report["failed"], report["total_modules"]), (1, 1, 2))
        self.assertEqual(report["modules"][0]["artifact"], "backend/target/debug/server")
        self.assertIn("frontend: FAIL", texts["build-summary.txt"])
        self.assertIn("fixture failure", texts["build.log"])
        self.assertIn(self.state["commit"], texts["build-summary.txt"])
        self.assertIn(self.state["tree"], texts["build-summary.txt"])
        self.assertEqual(report["provenance"], self.provenance)
        self.assertEqual(report["source_commit"], self.state["commit"])
        self.assertEqual(report["source_tree"], self.state["tree"])
        for name, text in texts.items():
            self.assertEqual(report["files"][name], {"bytes": len(text.encode()),
                             "sha256": hashlib.sha256(text.encode()).hexdigest()})
        self.assertEqual(report["artifact_sha256"]["diagnostic/build-12345678.logd"],
                         hashlib.sha256(self.archive.read_bytes()).hexdigest())
        published = json.dumps(report) + "".join(texts.values())
        for private in (str(self.root), str(self.home), str(self.tmp), "fixture-user", "fixture-host"):
            self.assertNotIn(private, published)
        for label in ("<REPO>", "<HOME>", "<TEMP>", "<USER>", "<HOST>"):
            self.assertIn(label, texts["build.log"])
        self.assertIn("tools/safe_logd.py unpack", report["decrypt_command"])

    def test_over_one_megabyte_log_roundtrips_through_actual_cli(self):
        # More than the CLI's metadata cap, but below the explicit archive cap.
        output = "fixture-日志\n" * 110000
        self.assertGreater(len(output.encode()), 1024 * 1024)
        self.assertTrue(self.generate([("fixture", True, 0.5, output, None)]))
        report, texts = self.report_and_texts()
        self.assertLess(self.metadata.stat().st_size, 1024 * 1024)
        module = report["modules"][0]
        self.assertLessEqual(len(module["output"].encode()), 16 * 1024)
        self.assertTrue(module["output_truncated"])
        self.assertEqual(module["output_byte_count"], len(output.encode()))
        self.assertEqual(module["full_output"], "build.log")
        self.assertIn(output, texts["build.log"])
        destination = self.tmp / "decoded"
        tool = Path(__file__).resolve().parents[1] / "tools/safe_logd.py"
        result = subprocess.run([sys.executable, str(tool), "unpack", str(self.archive),
                                 str(destination), "--metadata", str(self.metadata)],
                                capture_output=True, text=True, timeout=30)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual((destination / "build.log").read_text(), texts["build.log"])

    def test_preview_utf8_boundary_and_no_phantom_full_output(self):
        report = build.build_diagnostic_report([("fixture", False, 1, "甲" * 10000, None)], "12345678")
        module = report["modules"][0]
        self.assertLessEqual(len(module["output"].encode()), 16 * 1024)
        self.assertNotIn("\ufffd", module["output"])
        self.assertIsNone(module["full_output"])
        self.assertIsNone(report["decrypt_command"])

    def test_oversized_log_fails_honestly_with_bounded_json(self):
        output = "x" * (8 * 1024 * 1024 + 1)
        self.assertFalse(self.generate([("fixture", False, 1, output, None)]))
        report = json.loads(self.metadata.read_text())
        self.assertIsNone(report["diagnostic_logd"])
        self.assertIsNone(report["password"])
        self.assertIsNone(report["modules"][0]["full_output"])
        self.assertIn("packaging failed", report["diagnostic_logd_error"])
        self.assertFalse(self.archive.exists())
        self.assertLess(self.metadata.stat().st_size, 1024 * 1024)

    def test_unknown_dirty_or_changed_source_refused_before_artifacts(self):
        cases = [("before", "commit", None), ("before", "tree", "abc"),
                 ("before", "dirty", True), ("after", "dirty", True),
                 ("before", "dirty", None), ("after", "dirty", None),
                 ("after", "commit", "c" * 40), ("after", "tree", "d" * 40)]
        for stage, field, value in cases:
            with self.subTest(stage=stage, field=field, value=value):
                provenance = copy.deepcopy(self.provenance)
                provenance[stage][field] = value
                self.assertFalse(self.generate([], provenance))
                self.assertFalse(self.diag.exists())

    def test_source_state_detects_untracked_files_and_unknown_git(self):
        outputs = [self.state["commit"], self.state["tree"], "?? new_source.go\n"]
        mocked = [subprocess.CompletedProcess([], 0, value, "") for value in outputs]
        with patch.object(build.subprocess, "run", side_effect=mocked) as run:
            state = build.source_state()
        self.assertTrue(state["dirty"])
        self.assertIn("--untracked-files=normal", run.call_args_list[2].args[0])
        with patch.object(build.subprocess, "run", side_effect=OSError("fixture missing git")):
            self.assertEqual(build.source_state(), {"commit": None, "tree": None, "dirty": None})

    def test_existing_bundle_is_never_overwritten(self):
        self.diag.mkdir()
        self.metadata.write_text("existing fixture")
        self.assertFalse(self.generate([]))
        self.assertEqual(self.metadata.read_text(), "existing fixture")
        self.assertFalse(self.archive.exists())

    def test_existing_fragment_is_preserved(self):
        self.diag.mkdir()
        fragment = self.diag / "build-12345678-part001.logd"
        fragment.write_bytes(b"historical fixture")
        self.assertFalse(self.generate([]))
        self.assertEqual(fragment.read_bytes(), b"historical fixture")
        self.assertFalse(self.metadata.exists())

    def test_diagnostic_symlink_refused_for_generate_and_clean(self):
        outside = self.tmp / "outside"
        outside.mkdir()
        victim = outside / "build-cafebabe.json"
        victim.write_text("preserve fixture")
        self.diag.symlink_to(outside, target_is_directory=True)
        self.assertFalse(self.generate([]))
        with patch.object(sys, "argv", ["build.py", "--clean", "--module", "backend"]), \
                patch.object(build, "clean_module") as clean:
            self.assertEqual(build.main(), 1)
        clean.assert_not_called()
        self.assertEqual(victim.read_text(), "preserve fixture")

    def test_module_artifact_symlink_cannot_escape_repository(self):
        outside = self.tmp / "outside"
        outside.mkdir()
        (self.root / "link").symlink_to(outside, target_is_directory=True)
        self.assertIsNone(build._metadata_artifact("link/app"))
        self.assertIsNone(build._metadata_artifact("../outside/app"))
        self.assertEqual(build._metadata_artifact("backend/target/server"), "backend/target/server")

    def test_clean_rejects_symlink_artifact_before_any_module_cleanup(self):
        self.diag.mkdir()
        target = self.tmp / "outside-fixture.json"
        target.write_text("preserve")
        self.metadata.symlink_to(target)
        with patch.object(sys, "argv", ["build.py", "--clean", "--module", "backend"]), \
                patch.object(build, "clean_module") as clean:
            self.assertEqual(build.main(), 1)
        clean.assert_not_called()
        self.assertEqual(target.read_text(), "preserve")

    def test_windows_identity_and_path_case_variants_are_redacted(self):
        with patch.object(build, "ROOT", PureWindowsPath(r"C:\Users\Alice\repo")), \
                patch.object(build.Path, "home", return_value=PureWindowsPath(r"C:\Users\Alice")), \
                patch.object(build.getpass, "getuser", return_value="Alice"), \
                patch.object(build.platform, "node", return_value="WorkStation"):
            self.assertEqual(build._metadata_redact(r"user=ALICE host=WORKSTATION file=c:\users\ALICE\repo\src"),
                             r"user=<USER> host=<HOST> file=<REPO>\src")

    def test_resolved_home_and_temp_aliases_and_labelled_secrets_are_redacted(self):
        actual_home = self.tmp / "real-home"
        actual_temp = self.tmp / "real-temp"
        actual_home.mkdir()
        actual_temp.mkdir()
        alias_home = self.tmp / "home-alias"
        alias_temp = self.tmp / "temp-alias"
        alias_home.symlink_to(actual_home, target_is_directory=True)
        alias_temp.symlink_to(actual_temp, target_is_directory=True)
        with patch.object(build.Path, "home", return_value=alias_home), \
                patch.object(build.tempfile, "gettempdir", return_value=str(alias_temp)):
            output = build._metadata_redact(f"{actual_home.resolve()}/file {actual_temp.resolve()}/compiler.obj "
                                           "API_KEY=fixturekey access-token: fixturetoken "
                                           "Authorization: Bearer fixturebearer")
        self.assertEqual(output, "<HOME>/file <TEMP>/compiler.obj API_KEY=<REDACTED> "
                                 "access-token: <REDACTED> Authorization: Bearer <REDACTED>")

    def test_system_information_uses_only_minimal_runtime_fields(self):
        with patch.object(build.subprocess, "run", side_effect=AssertionError("no shell collection")), \
                patch.object(build.Path, "home", side_effect=AssertionError("no HOME scan")), \
                patch.object(build.platform, "node", side_effect=AssertionError("no hostname")):
            fields = build.collect_system_info().splitlines()
        self.assertEqual([line.split(":")[0] for line in fields],
                         ["Zeroeye build runtime", "python", "system", "release", "machine", "cpu_count"])

    def test_main_fails_when_packaging_fails_even_if_selected_build_passes(self):
        with patch.object(sys, "argv", ["build.py", "--module", "backend"]), \
                patch.object(build, "build_module", return_value=(True, 0.1, "synthetic fixture")), \
                patch.object(build, "verify_binary", return_value=None), \
                patch.object(build, "source_state", return_value=self.state.copy()), \
                patch.object(build, "generate_logd", return_value=False):
            self.assertEqual(build.main(), 1)


if __name__ == "__main__":
    unittest.main()
