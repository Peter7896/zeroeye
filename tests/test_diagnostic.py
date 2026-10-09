"""Deterministic metadata checks; temporary bytes are not encrypted evidence.

Retains PR #31's matching-pair/failure cases and uses the shared validator
adapted from PR #28/#22 rather than an approximate second implementation.
"""
from __future__ import annotations

import json
import tempfile
import unittest
from pathlib import Path
from unittest import mock

from tools.verify_diagnostic_bundle import validate_diagnostic_bundle

REPO_ROOT = Path(__file__).resolve().parents[1]


def validate_diagnostic_directory(diagnostic_dir: Path) -> tuple[list[Path], list[Path]]:
    """Assert that every fixture's exact reference and metadata contract is valid."""
    json_files = sorted(diagnostic_dir.glob("build-*.json"))
    logd_files = sorted(diagnostic_dir.glob("build-*.logd"))
    if not json_files:
        raise AssertionError("Missing diagnostic JSON")
    if not logd_files:
        raise AssertionError("Missing diagnostic .logd")
    referenced = set()
    for metadata in json_files:
        errors = validate_diagnostic_bundle(metadata, diagnostic_dir.parent)
        if errors:
            raise AssertionError("; ".join(errors))
        reference = json.loads(metadata.read_text(encoding="utf-8"))["diagnostic_logd"]
        referenced.update([reference] if isinstance(reference, str) else reference)
    if referenced != {"diagnostic/" + path.name for path in logd_files}:
        raise AssertionError("Mismatched diagnostic bundle pair: unreferenced .logd")
    return json_files, logd_files


class TestDiagnosticBundleValidation(unittest.TestCase):
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
            mock.patch("tools.verify_diagnostic_bundle.Path.home", return_value=self.root / "fixture-home"),
            mock.patch("tools.verify_diagnostic_bundle.getpass.getuser", return_value="fixture-user"),
            mock.patch("tools.verify_diagnostic_bundle.platform.node", return_value="fixture-host"),
        ):
            patcher.start()
            self.addCleanup(patcher.stop)

    def validate(self):
        self.metadata.write_text(json.dumps(self.report), encoding="utf-8")
        return validate_diagnostic_directory(self.directory)

    def test_generated_bundle_is_a_matching_pair(self):
        self.assertEqual(self.validate(), ([self.metadata], [self.logd]))

    def test_missing_json_fails_clearly(self):
        with self.assertRaisesRegex(AssertionError, "Missing diagnostic JSON"):
            validate_diagnostic_directory(self.directory)

    def test_missing_logd_fails_clearly(self):
        self.logd.unlink()
        with self.assertRaisesRegex(AssertionError, r"Missing diagnostic \.logd"):
            self.validate()

    def test_mismatched_pair_fails_clearly(self):
        self.logd.rename(self.directory / "build-XYZ.logd")
        with self.assertRaisesRegex(AssertionError, "artifact is missing"):
            self.validate()

    def test_wrong_json_log_reference_fails_clearly(self):
        self.report["diagnostic_logd"] = "diagnostic/build-XYZ.logd"
        with self.assertRaisesRegex(AssertionError, "pair mismatch"):
            self.validate()

    def test_windows_absolute_artifact_path_fails(self):
        self.report["modules"][0]["artifact"] = r"C:\Users\fictional\repo\app.exe"
        with self.assertRaisesRegex(AssertionError, "repository-relative / path"):
            self.validate()

    def test_unix_absolute_artifact_path_fails(self):
        self.report["modules"][0]["artifact"] = "/home/fictional/repo/app"
        with self.assertRaisesRegex(AssertionError, "repository-relative / path"):
            self.validate()

    def test_relative_forward_slash_artifact_path_is_accepted(self):
        self.validate()

    def test_invalid_json_or_non_object_fails_clearly(self):
        for contents in ("[]", "null", "42", "{", "\ud800"):
            with self.subTest(contents=repr(contents)):
                self.metadata.write_bytes(contents.encode("utf-8", errors="surrogatepass"))
                self.assertTrue(validate_diagnostic_bundle(self.metadata, self.root))

    def test_missing_or_invalid_reference_types(self):
        for reference in (None, "", [], {}, [None], [1]):
            with self.subTest(reference=reference):
                self.report["diagnostic_logd"] = reference
                with self.assertRaisesRegex(AssertionError, "diagnostic_logd is missing"):
                    self.validate()

    def test_duplicate_references_are_rejected(self):
        self.report["diagnostic_logd"] = ["diagnostic/build-ABC.logd"] * 2
        with self.assertRaisesRegex(AssertionError, "duplicate"):
            self.validate()

    def test_gap_or_reversed_chunks_are_rejected(self):
        self.logd.unlink()
        for number in (1, 2, 3):
            (self.directory / f"build-ABC-part{number:03d}.logd").write_bytes(b"unit-test fixture")
        for numbers in ((1, 3), (2, 1), (0,), (2,)):
            with self.subTest(numbers=numbers):
                self.report["diagnostic_logd"] = [f"diagnostic/build-ABC-part{n:03d}.logd" for n in numbers]
                with self.assertRaisesRegex(AssertionError, "ordered and consecutive"):
                    self.validate()

    def test_unreferenced_logd_is_rejected(self):
        (self.directory / "build-XYZ.logd").write_bytes(b"unit-test fixture")
        with self.assertRaisesRegex(AssertionError, "unreferenced"):
            self.validate()

    def test_sensitive_values_rejected_without_echoing_them(self):
        for value in (str(self.root), str(self.root / "fixture-home"), "fixture-user", "fixture-host"):
            with self.subTest(value=value):
                self.report["modules"][0]["output"] = "context=" + value
                self.metadata.write_text(json.dumps(self.report), encoding="utf-8")
                errors = validate_diagnostic_bundle(self.metadata, self.root)
                self.assertIn("diagnostic metadata leaks local value", errors)
                self.assertFalse(any(value in error for error in errors))


if __name__ == "__main__":
    unittest.main()
