#!/usr/bin/env python3
"""Diagnostic bundle validation tests ($25 bounty, issue #1)."""
from __future__ import annotations

import json
import os
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

import build  # noqa: E402


class TestDiagnosticBundle(unittest.TestCase):

    def setUp(self):
        self.home = str(Path.home())
        self.username = os.environ.get("USER", os.environ.get("USERNAME", ""))

    # ── artifact existence ────────────────────────────────────────

    def test_diagnostic_paths_for_commit_returns_paths(self):
        logd, meta, commit_id = build.diagnostic_paths_for_commit()
        self.assertIsInstance(commit_id, str)
        self.assertTrue(len(commit_id) > 0)
        self.assertIn("diagnostic", str(logd).replace("\\", "/"))
        self.assertIn("diagnostic", str(meta).replace("\\", "/"))

    def test_logd_and_json_use_same_commit_id(self):
        logd, meta, cid1 = build.diagnostic_paths_for_commit()
        logd2, meta2, cid2 = build.diagnostic_paths_for_commit()
        self.assertEqual(cid1, cid2,
                         "same commit should produce same commit_id")

    # ── no path leakage ───────────────────────────────────────────

    def test_report_does_not_leak_home(self):
        report = build.build_diagnostic_report([], "test-c")
        json_str = json.dumps(report)
        home_norm = self.home.replace("\\", "/")
        json_norm = json_str.replace("\\", "/")
        self.assertNotIn(home_norm, json_norm)

    def test_report_does_not_leak_temp(self):
        report = build.build_diagnostic_report([], "test-c")
        tmp = tempfile.gettempdir().replace("\\", "/").split(":")[-1]
        if len(tmp) > 4:
            self.assertNotIn(tmp, json.dumps(report).replace("\\", "/"))

    def test_report_does_not_leak_username(self):
        if not self.username or len(self.username) < 2:
            self.skipTest("no username")
        report = build.build_diagnostic_report([], "test-c")
        self.assertNotIn(self.username.lower(),
                         json.dumps(report).lower())

    # ── report structure ──────────────────────────────────────────

    def test_report_has_required_keys(self):
        report = build.build_diagnostic_report([], "abc123")
        for key in ("commit", "total_modules", "passed", "failed", "modules"):
            self.assertIn(key, report)

    def test_report_includes_diagnostic_logd(self):
        report = build.build_diagnostic_report([], "test")
        self.assertIn("diagnostic_logd", report)

    def test_commit_id_present(self):
        report = build.build_diagnostic_report([], "commit-hash-123")
        self.assertEqual(report["commit"], "commit-hash-123")

    # ── cross-platform ────────────────────────────────────────────

    def test_report_is_json_serializable(self):
        report = build.build_diagnostic_report(
            [("mod", True, 0.1, "ok", None)], "c1"
        )
        json_str = json.dumps(report)
        self.assertIsInstance(json_str, str)
        roundtrip = json.loads(json_str)
        self.assertEqual(roundtrip["commit"], "c1")


if __name__ == "__main__":
    unittest.main()
