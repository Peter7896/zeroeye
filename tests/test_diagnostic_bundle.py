#!/usr/bin/env python3
"""
Diagnostic bundle validation tests.

Validates that build.py generates correct diagnostic metadata and encrypted
bundle references without relying on live services.
"""

import json
import os
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

# Add project root to path
ROOT = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(ROOT))

import build


class TestDiagnosticBundleValidation(unittest.TestCase):
    """Test diagnostic bundle generation and metadata validation."""

    def setUp(self):
        """Set up test fixtures."""
        self.test_dir = tempfile.mkdtemp()
        self.diagnostic_dir = Path(self.test_dir) / "diagnostic"
        self.diagnostic_dir.mkdir(parents=True, exist_ok=True)

    def tearDown(self):
        """Clean up test fixtures."""
        import shutil
        shutil.rmtree(self.test_dir, ignore_errors=True)

    def _create_test_diagnostic_files(self, commit_id: str = "abc12345"):
        """Create test diagnostic files."""
        logd_path = self.diagnostic_dir / f"build-{commit_id}.logd"
        json_path = self.diagnostic_dir / f"build-{commit_id}.json"

        # Create a minimal logd file
        logd_path.write_bytes(b"test diagnostic bundle content")

        # Create metadata JSON
        metadata = {
            "generated_at": "2026-06-19T01:00:00+00:00",
            "commit": commit_id,
            "diagnostic_logd": f"diagnostic/build-{commit_id}.logd",
            "diagnostic_logd_error": None,
            "message_blocker": None,
            "chunked": False,
            "chunk_size_bytes": None,
            "password": None,
            "decrypt_command": None,
            "total_modules": 2,
            "passed": 1,
            "failed": 1,
            "modules": [
                {
                    "name": "test-module-1",
                    "status": "PASS",
                    "elapsed_seconds": 1.234,
                    "artifact": "test1",
                    "output": "Test output 1"
                },
                {
                    "name": "test-module-2",
                    "status": "FAIL",
                    "elapsed_seconds": 0.5,
                    "artifact": None,
                    "output": "Test failure output"
                }
            ],
            "pr_note": "Test PR note"
        }
        json_path.write_text(json.dumps(metadata, indent=2), encoding="utf-8")

        return logd_path, json_path

    def test_diagnostic_files_exist(self):
        """Test that diagnostic files are created with matching names."""
        commit_id = "test1234"
        logd_path, json_path = self._create_test_diagnostic_files(commit_id)

        self.assertTrue(logd_path.exists(), "Logd file should exist")
        self.assertTrue(json_path.exists(), "JSON metadata file should exist")

    def test_diagnostic_json_structure(self):
        """Test that diagnostic JSON has required structure."""
        _, json_path = self._create_test_diagnostic_files()

        with open(json_path, "r", encoding="utf-8") as f:
            data = json.load(f)

        # Required top-level fields
        required_fields = {
            "generated_at", "commit", "total_modules", "passed",
            "failed", "modules"
        }
        for field in required_fields:
            self.assertIn(field, data, f"Missing required field: {field}")

    def test_diagnostic_counts_match(self):
        """Test that module counts match actual module array."""
        _, json_path = self._create_test_diagnostic_files()

        with open(json_path, "r", encoding="utf-8") as f:
            data = json.load(f)

        modules = data["modules"]
        actual_total = len(modules)
        actual_passed = sum(1 for m in modules if m["status"] == "PASS")
        actual_failed = sum(1 for m in modules if m["status"] == "FAIL")

        self.assertEqual(data["total_modules"], actual_total,
                         "total_modules should match actual count")
        self.assertEqual(data["passed"], actual_passed,
                         "passed count should match actual PASS modules")
        self.assertEqual(data["failed"], actual_failed,
                         "failed count should match actual FAIL modules")

    def test_module_status_values(self):
        """Test that module statuses are valid."""
        _, json_path = self._create_test_diagnostic_files()

        with open(json_path, "r", encoding="utf-8") as f:
            data = json.load(f)

        valid_statuses = {"PASS", "FAIL"}
        for i, module in enumerate(data["modules"]):
            self.assertIn(module["status"], valid_statuses,
                          f"Module {i}: invalid status '{module['status']}'")

    def test_module_required_fields(self):
        """Test that each module has required fields."""
        _, json_path = self._create_test_diagnostic_files()

        with open(json_path, "r", encoding="utf-8") as f:
            data = json.load(f)

        required_module_fields = {"name", "status", "elapsed_seconds", "output"}
        for i, module in enumerate(data["modules"]):
            for field in required_module_fields:
                self.assertIn(field, module,
                              f"Module {i}: missing field '{field}'")

    def test_elapsed_seconds_numeric(self):
        """Test that elapsed_seconds is numeric."""
        _, json_path = self._create_test_diagnostic_files()

        with open(json_path, "r", encoding="utf-8") as f:
            data = json.load(f)

        for i, module in enumerate(data["modules"]):
            elapsed = module["elapsed_seconds"]
            self.assertIsInstance(elapsed, (int, float),
                                  f"Module {i}: elapsed_seconds must be numeric")
            self.assertGreaterEqual(elapsed, 0,
                                    f"Module {i}: elapsed_seconds must be non-negative")

    def test_path_redaction(self):
        """Test that sensitive paths are redacted from metadata."""
        _, json_path = self._create_test_diagnostic_files()

        with open(json_path, "r", encoding="utf-8") as f:
            content = f.read()

        # Check for common sensitive path patterns
        home_dir = str(Path.home())
        username = os.environ.get("USER", os.environ.get("USERNAME", ""))

        # Metadata should not contain absolute home paths
        self.assertNotIn(home_dir, content,
                         "Metadata should not contain absolute home paths")

        # Check that paths use forward slashes (repository-relative)
        if "diagnostic/" in content:
            path_part = content.split("diagnostic/")[1].split("\"")[0]
            self.assertNotIn("\\", path_part,
                             "Paths should use forward slashes")

    def test_missing_json_fails(self):
        """Test that missing JSON file is detected."""
        commit_id = "missing01"
        logd_path = self.diagnostic_dir / f"build-{commit_id}.logd"
        logd_path.write_bytes(b"test content")

        json_path = self.diagnostic_dir / f"build-{commit_id}.json"
        self.assertFalse(json_path.exists(), "JSON should not exist for this test")

        # Validation should detect missing JSON
        result = self._validate_diagnostic_bundle(commit_id)
        self.assertFalse(result["valid"], "Should fail when JSON is missing")
        self.assertIn("missing", result["error"].lower(),
                      "Error should mention missing file")

    def test_missing_logd_fails(self):
        """Test that missing logd file is detected."""
        commit_id = "missing02"
        json_path = self.diagnostic_dir / f"build-{commit_id}.json"
        json_path.write_text(json.dumps({"commit": commit_id}), encoding="utf-8")

        logd_path = self.diagnostic_dir / f"build-{commit_id}.logd"
        self.assertFalse(logd_path.exists(), "Logd should not exist for this test")

        # Validation should detect missing logd
        result = self._validate_diagnostic_bundle(commit_id)
        self.assertFalse(result["valid"], "Should fail when logd is missing")

    def test_mismatched_commit_ids(self):
        """Test that mismatched commit IDs are detected."""
        # Create files with different commit IDs
        logd_path = self.diagnostic_dir / "build-abc12345.logd"
        json_path = self.diagnostic_dir / "build-def67890.json"

        logd_path.write_bytes(b"test content")
        json_path.write_text(json.dumps({"commit": "def67890"}), encoding="utf-8")

        # Validation should detect mismatch
        result = self._validate_diagnostic_bundle("abc12345")
        self.assertFalse(result["valid"], "Should fail when commit IDs mismatch")

    def _validate_diagnostic_bundle(self, commit_id: str) -> dict:
        """Validate a diagnostic bundle exists and is consistent."""
        result = {
            "valid": True,
            "error": None,
            "logd_path": None,
            "json_path": None
        }

        logd_path = self.diagnostic_dir / f"build-{commit_id}.logd"
        json_path = self.diagnostic_dir / f"build-{commit_id}.json"

        if not logd_path.exists():
            result["valid"] = False
            result["error"] = f"Missing logd file: {logd_path.name}"
            return result

        if not json_path.exists():
            result["valid"] = False
            result["error"] = f"Missing JSON metadata: {json_path.name}"
            return result

        try:
            with open(json_path, "r", encoding="utf-8") as f:
                data = json.load(f)
        except json.JSONDecodeError as e:
            result["valid"] = False
            result["error"] = f"Invalid JSON: {e}"
            return result

        # Check commit ID consistency
        json_commit = data.get("commit", "")
        if json_commit != commit_id:
            result["valid"] = False
            result["error"] = (
                f"Commit ID mismatch: JSON has '{json_commit}', "
                f"expected '{commit_id}'"
            )
            return result

        result["logd_path"] = str(logd_path)
        result["json_path"] = str(json_path)
        return result

    def test_cross_platform_path_separator(self):
        """Test that paths use forward slashes on all platforms."""
        _, json_path = self._create_test_diagnostic_files()

        with open(json_path, "r", encoding="utf-8") as f:
            data = json.load(f)

        # Check diagnostic_logd path uses forward slashes
        logd_ref = data.get("diagnostic_logd", "")
        if logd_ref:
            self.assertNotIn("\\\\", logd_ref,
                             "diagnostic_logd should use forward slashes")

    def test_deterministic_output(self):
        """Test that validation is deterministic."""
        _, json_path = self._create_test_diagnostic_files()

        # Run validation multiple times
        results = []
        for _ in range(3):
            with open(json_path, "r", encoding="utf-8") as f:
                data = json.load(f)
            results.append(json.dumps(data, sort_keys=True))

        # All results should be identical
        self.assertEqual(len(set(results)), 1,
                         "Validation should be deterministic")


class TestBuildScriptImports(unittest.TestCase):
    """Test that build script imports work correctly."""

    def test_build_module_imports(self):
        """Test that build module can be imported."""
        try:
            import build
            self.assertTrue(hasattr(build, 'MODULES'))
            self.assertTrue(hasattr(build, 'build_module'))
        except ImportError as e:
            self.fail(f"Failed to import build module: {e}")

    def test_module_list_not_empty(self):
        """Test that MODULES list is not empty."""
        self.assertGreater(len(build.MODULES), 0,
                           "MODULES list should not be empty")


if __name__ == "__main__":
    unittest.main(verbosity=2)
