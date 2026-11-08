import json
import tempfile
import unittest
from pathlib import Path
from unittest import mock

import build
from tools.verify_diagnostic_bundle import validate_diagnostic_bundle


class DiagnosticBundleValidationTests(unittest.TestCase):
    def _write_metadata(self, root: Path, name: str, diagnostic_logd):
        metadata = root / "diagnostic" / name
        metadata.parent.mkdir(parents=True, exist_ok=True)
        metadata.write_text(
            json.dumps(
                {
                    "commit": "12345678",
                    "diagnostic_logd": diagnostic_logd,
                    "modules": [],
                }
            ),
            encoding="utf-8",
        )
        return metadata

    def test_valid_metadata_matches_existing_repo_relative_logd(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            root = Path(tmpdir)
            (root / "diagnostic").mkdir()
            (root / "diagnostic" / "build-12345678.logd").write_bytes(b"logd")
            metadata = self._write_metadata(
                root,
                "build-12345678.json",
                "diagnostic/build-12345678.logd",
            )

            self.assertEqual(validate_diagnostic_bundle(metadata, root), [])

    def test_chunked_logd_references_are_allowed(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            root = Path(tmpdir)
            diagnostic = root / "diagnostic"
            diagnostic.mkdir()
            (diagnostic / "build-12345678-part001.logd").write_bytes(b"one")
            (diagnostic / "build-12345678-part002.logd").write_bytes(b"two")
            metadata = self._write_metadata(
                root,
                "build-12345678.json",
                [
                    "diagnostic/build-12345678-part001.logd",
                    "diagnostic/build-12345678-part002.logd",
                ],
            )

            self.assertEqual(validate_diagnostic_bundle(metadata, root), [])

    def test_missing_json_fails_clearly(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            errors = validate_diagnostic_bundle(Path(tmpdir) / "diagnostic" / "missing.json", Path(tmpdir))

        self.assertTrue(any("missing diagnostic JSON" in error for error in errors))

    def test_missing_logd_reference_fails_clearly(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            root = Path(tmpdir)
            metadata = self._write_metadata(
                root,
                "build-12345678.json",
                "diagnostic/build-12345678.logd",
            )

            errors = validate_diagnostic_bundle(metadata, root)

        self.assertTrue(any("referenced .logd artifact is missing" in error for error in errors))

    def test_mismatched_logd_pair_fails_clearly(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            root = Path(tmpdir)
            diagnostic = root / "diagnostic"
            diagnostic.mkdir()
            (diagnostic / "build-deadbeef.logd").write_bytes(b"logd")
            metadata = self._write_metadata(
                root,
                "build-12345678.json",
                "diagnostic/build-deadbeef.logd",
            )

            errors = validate_diagnostic_bundle(metadata, root)

        self.assertTrue(any("JSON/logd pair mismatch" in error for error in errors))

    def test_absolute_or_windows_paths_are_rejected(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            root = Path(tmpdir)
            metadata = self._write_metadata(
                root,
                "build-12345678.json",
                r"C:\Users\someone\diagnostic\build-12345678.logd",
            )

            errors = validate_diagnostic_bundle(metadata, root)

        self.assertTrue(any("repository-relative / path" in error for error in errors))

    def test_local_path_and_user_values_are_redacted(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            root = Path(tmpdir)
            diagnostic = root / "diagnostic"
            diagnostic.mkdir()
            (diagnostic / "build-12345678.logd").write_bytes(b"logd")
            metadata = self._write_metadata(
                root,
                "build-12345678.json",
                "diagnostic/build-12345678.logd",
            )
            payload = json.loads(metadata.read_text(encoding="utf-8"))
            payload["output"] = f"leaked path {root}"
            metadata.write_text(json.dumps(payload), encoding="utf-8")

            with mock.patch("tools.verify_diagnostic_bundle.getpass.getuser", return_value="example-user"):
                with mock.patch("tools.verify_diagnostic_bundle.platform.node", return_value="example-host"):
                    errors = validate_diagnostic_bundle(metadata, root)

        self.assertTrue(any("leaks local value" in error for error in errors))

    def test_build_report_uses_posix_reassemble_target_for_chunked_logd(self):
        report = build.build_diagnostic_report(
            results=[],
            commit_id="12345678",
            logd_relpaths=[
                "diagnostic/build-12345678-part001.logd",
                "diagnostic/build-12345678-part002.logd",
            ],
            password="pw",
            chunked=True,
        )

        self.assertIn("diagnostic/build-12345678.logd", report["decrypt_command"])
        self.assertNotIn("\\", report["decrypt_command"])


if __name__ == "__main__":
    unittest.main()
