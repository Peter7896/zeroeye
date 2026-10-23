"""Deterministic tests for Zeroeye diagnostic JSON and encrypted log references."""
import hashlib
import json
import tempfile
import unittest
from pathlib import Path
from diagnostic_validator import validate_bundle


class DiagnosticBundleTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.diagnostic = self.root / "diagnostic"
        self.diagnostic.mkdir()
        self.log = self.diagnostic / "build-abcdef12.logd"
        self.log.write_bytes(b"encrypted")
        self.meta = self.diagnostic / "build-abcdef12.json"
        self.report = {
            "commit": "abcdef12",
            "diagnostic_logd": "diagnostic/build-abcdef12.logd",
            "artifact_sha256": {
                "diagnostic/build-abcdef12.logd": hashlib.sha256(b"encrypted").hexdigest()
            },
            "modules": [{"artifact": "backend/target/debug/backend"}],
        }
        self.save()

    def save(self):
        self.meta.write_text(json.dumps(self.report), encoding="utf-8")

    def rejects(self, message):
        self.save()
        with self.assertRaisesRegex(ValueError, message):
            validate_bundle(self.meta, self.root)

    def test_valid_pair(self):
        self.assertEqual(validate_bundle(self.meta, self.root), [self.log])

    def test_missing_json(self):
        self.meta.unlink()
        with self.assertRaisesRegex(ValueError, "JSON is missing"):
            validate_bundle(self.meta, self.root)

    def test_missing_reference(self):
        self.report["diagnostic_logd"] = None
        self.rejects("reference is missing")

    def test_mismatched_commit(self):
        self.report["diagnostic_logd"] = "diagnostic/build-ffffffff.logd"
        self.rejects("mismatches")

    def test_missing_log(self):
        self.log.unlink()
        with self.assertRaisesRegex(ValueError, "file is missing"):
            validate_bundle(self.meta, self.root)

    def test_corrupt_log(self):
        self.log.write_bytes(b"tampered")
        with self.assertRaisesRegex(ValueError, "checksum mismatch"):
            validate_bundle(self.meta, self.root)

    def test_missing_checksum(self):
        self.report.pop("artifact_sha256")
        self.rejects("checksum is missing")

    def test_invalid_checksum(self):
        self.report["artifact_sha256"]["diagnostic/build-abcdef12.logd"] = "bad"
        self.rejects("checksum is missing or invalid")

    def test_absolute_artifact(self):
        self.report["modules"][0]["artifact"] = "/private/user"
        self.rejects("repository-relative")

    def test_windows_artifact(self):
        self.report["modules"][0]["artifact"] = r"C:\Users\alice"
        self.rejects("repository-relative")

    def test_parent_traversal(self):
        self.report["modules"][0]["artifact"] = "../secret"
        self.rejects("repository-relative")

    def test_windows_log_reference(self):
        self.report["diagnostic_logd"] = r"diagnostic\build-abcdef12.logd"
        self.rejects("POSIX")

    def test_redaction_home_path(self):
        self.report["modules"][0]["output"] = "failure at /home/alice/private/token"
        self.rejects("sensitive")

    def test_redaction_windows_profile(self):
        self.report["modules"][0]["output"] = r"C:\Users\alice\secret"
        self.rejects("sensitive")

    def test_redaction_credentials(self):
        self.report["modules"][0]["output"] = "api_key=abc123"
        self.rejects("sensitive")

    def test_json_array(self):
        self.meta.write_text("[]")
        with self.assertRaisesRegex(ValueError, "must be an object"):
            validate_bundle(self.meta, self.root)

    def test_modules_string(self):
        self.report["modules"] = "unexpected"
        self.rejects("list of objects")

    def test_modules_non_object(self):
        self.report["modules"] = [None]
        self.rejects("list of objects")

    def test_duplicate_log(self):
        self.report["diagnostic_logd"] = ["diagnostic/build-abcdef12.logd"] * 2
        self.rejects("duplicate")

    def test_chunk_order(self):
        names = ["diagnostic/build-abcdef12-part002.logd", "diagnostic/build-abcdef12-part001.logd"]
        for name in names:
            (self.root / name).write_bytes(b"part")
        self.report["diagnostic_logd"] = names
        self.report["artifact_sha256"] = {name: hashlib.sha256(b"part").hexdigest() for name in names}
        self.rejects("ordered and contiguous")

    def test_valid_chunks(self):
        names = ["diagnostic/build-abcdef12-part001.logd", "diagnostic/build-abcdef12-part002.logd"]
        for name in names:
            (self.root / name).write_bytes(b"part")
        self.report["diagnostic_logd"] = names
        self.report["artifact_sha256"] = {name: hashlib.sha256(b"part").hexdigest() for name in names}
        self.report["chunked"] = True
        self.save()
        self.assertEqual(len(validate_bundle(self.meta, self.root)), 2)


if __name__ == "__main__":
    unittest.main()
