"""
Diagnostic bundle validation tests for ZeroEye.

These tests validate that a build.py run produces a matching pair of
  - diagnostic/build-<commit>.json   (metadata)
  - diagnostic/build-<commit>.logd   (encrypted diagnostic bundle)

and that the metadata is well-formed, redacts sensitive paths, and uses
repository-relative '/' paths for artifact reporting.

Run with:
    python3 -m pytest tests/test_diagnostic_bundle_validation.py -v
"""

import json
import os
import re
import subprocess
import sys
import unittest
from pathlib import Path

# ── repo layout ──────────────────────────────────────────────────────────────
REPO_ROOT = Path(__file__).resolve().parent.parent
DIAGNOSTIC_DIR = REPO_ROOT / "diagnostic"
BUILD_PY = REPO_ROOT / "build.py"

_COMMIT_RE = re.compile(r"^build-([0-9a-f]{8})\.(json|logd)$")


def _latest_diagnostic_pair() -> tuple[Path, Path, str]:
    """Find the most recent (highest commit hash) diagnostic pair in diagnostic/.

    Returns (json_path, logd_path, commit_id).

    build.py names artifacts with the first 8 hex chars of HEAD at build time.
    We pick the lexicographically largest commit id so we always test the most
    recently produced pair, even if build.py itself committed the artifacts
    (which advances HEAD).
    """
    if not DIAGNOSTIC_DIR.is_dir():
        raise FileNotFoundError(f"Diagnostic directory not found: {DIAGNOSTIC_DIR}")

    commits: dict[str, dict[str, Path]] = {}
    for entry in DIAGNOSTIC_DIR.iterdir():
        m = _COMMIT_RE.match(entry.name)
        if not m:
            continue
        cid = m.group(1)
        ext = m.group(2)
        commits.setdefault(cid, {})[ext] = entry

    # Filter to pairs that have both json and logd
    pairs = {
        cid: paths for cid, paths in commits.items()
        if "json" in paths and "logd" in paths
    }
    if not pairs:
        raise FileNotFoundError(
            "No complete diagnostic JSON+.logd pair found in "
            f"{DIAGNOSTIC_DIR}. Run 'python3 build.py' first."
        )

    # Pick the latest commit id (lexicographic works for hex prefixes)
    latest = max(pairs)
    return pairs[latest]["json"], pairs[latest]["logd"], latest


def _load_json(path: Path) -> dict:
    """Load and parse a JSON file, raising AssertionError on failure."""
    assert path.is_file(), f"JSON file not found: {path}"
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except json.JSONDecodeError as exc:
        raise AssertionError(f"Invalid JSON in {path}: {exc}") from exc


# ── helpers for redaction checks ─────────────────────────────────────────────
_HOME = Path.home()
_USER = os.environ.get("USER", os.environ.get("USERNAME", ""))
_TMP_DIRS = ["/tmp/", "/var/tmp/"]


def _collect_sensitive_leaks(text: str, context: str) -> None:
    """Assert that *text* does not contain home/temp/user tokens."""
    home_str = str(_HOME)
    if home_str in text:
        raise AssertionError(
            f"Home directory path leaked in {context}: found {home_str!r}"
        )
    if os.name != "nt":
        for tmp in _TMP_DIRS:
            if tmp in text:
                raise AssertionError(
                    f"Temp directory path leaked in {context}: found {tmp!r}"
                )
    if _USER and len(_USER) > 2 and _USER not in ("user",):
        user_pattern = re.compile(r"(^|/)" + re.escape(_USER) + r"(/|$)")
        match = user_pattern.search(text)
        if match:
            raise AssertionError(
                f"Username {_USER!r} leaked in {context} at position {match.start()}"
            )


# ── test class ───────────────────────────────────────────────────────────────
class TestDiagnosticBundleValidation(unittest.TestCase):
    """Validate the diagnostic bundle produced by build.py."""

    @classmethod
    def setUpClass(cls) -> None:
        """Locate the latest diagnostic pair and load the JSON."""
        cls.json_path, cls.logd_path, cls.commit_id = _latest_diagnostic_pair()
        cls.data: dict = {}

    # ── existence checks ───────────────────────────────────────────────────

    def test_01_json_exists(self) -> None:
        """The diagnostic JSON metadata file must exist."""
        assert self.json_path.is_file(), (
            f"Diagnostic JSON not found: {self.json_path}\n"
            f"Run 'python3 build.py' first to generate diagnostic artifacts."
        )

    def test_02_logd_exists(self) -> None:
        """The diagnostic .logd bundle must exist."""
        assert self.logd_path.is_file(), (
            f"Diagnostic .logd not found: {self.logd_path}\n"
            f"Run 'python3 build.py' first to generate diagnostic artifacts."
        )

    # ── JSON structure checks ──────────────────────────────────────────────

    def test_03_json_is_valid(self) -> None:
        """The diagnostic JSON must be parseable and well-formed."""
        self.__class__.data = _load_json(self.json_path)

    def test_04_json_has_required_keys(self) -> None:
        """The JSON must contain all required top-level keys."""
        data = self.data
        required_keys = [
            "generated_at",
            "commit",
            "diagnostic_logd",
            "total_modules",
            "passed",
            "failed",
            "modules",
        ]
        for key in required_keys:
            assert key in data, f"Missing required key {key!r} in diagnostic JSON"

    def test_05_json_commit_matches_filename(self) -> None:
        """The commit field in JSON must match the filename commit id."""
        data = self.data
        assert data["commit"] == self.commit_id, (
            f"Commit mismatch: JSON says {data['commit']!r}, "
            f"filename expects {self.commit_id!r}"
        )

    def test_06_json_logd_reference_matches_filename(self) -> None:
        """The diagnostic_logd field must reference the .logd file on disk."""
        data = self.data
        logd_ref = data.get("diagnostic_logd")
        assert logd_ref is not None, "diagnostic_logd is null; expected a path string"
        assert isinstance(logd_ref, str), (
            f"diagnostic_logd should be a string, got {type(logd_ref).__name__}"
        )
        expected_ref = f"diagnostic/build-{self.commit_id}.logd"
        assert logd_ref == expected_ref, (
            f"diagnostic_logd reference mismatch: got {logd_ref!r}, "
            f"expected {expected_ref!r}"
        )

    def test_07_json_module_counts_consistent(self) -> None:
        """passed + failed must equal total_modules and match the modules list."""
        data = self.data
        modules = data["modules"]
        assert isinstance(modules, list), "modules must be a list"
        assert data["total_modules"] == len(modules), (
            f"total_modules={data['total_modules']} but modules list has "
            f"{len(modules)} entries"
        )
        assert data["passed"] + data["failed"] == data["total_modules"], (
            f"passed({data['passed']}) + failed({data['failed']}) != "
            f"total_modules({data['total_modules']})"
        )

    def test_08_json_each_module_has_required_fields(self) -> None:
        """Each module entry must have name, status, elapsed_seconds, artifact, output."""
        data = self.data
        required_module_keys = {"name", "status", "elapsed_seconds", "artifact", "output"}
        for mod in data["modules"]:
            missing = required_module_keys - set(mod.keys())
            assert not missing, (
                f"Module {mod.get('name', '?')!r} missing keys: {missing}"
            )

    def test_09_json_module_status_values(self) -> None:
        """Module status must be 'PASS' or 'FAIL'."""
        for mod in self.data["modules"]:
            assert mod["status"] in ("PASS", "FAIL"), (
                f"Module {mod['name']!r}: status must be PASS or FAIL, "
                f"got {mod['status']!r}"
            )

    # ── redaction checks ───────────────────────────────────────────────────

    def test_10_no_home_paths_in_json(self) -> None:
        """Local home directory paths must be redacted from diagnostic JSON."""
        raw = self.json_path.read_text(encoding="utf-8")
        _collect_sensitive_leaks(raw, "diagnostic JSON")

    def test_11_no_temp_paths_in_json(self) -> None:
        """Temporary directory paths must be redacted from diagnostic JSON."""
        if os.name == "nt":
            self.skipTest("Temp path check is Unix-only")
        raw = self.json_path.read_text(encoding="utf-8")
        _collect_sensitive_leaks(raw, "diagnostic JSON (temp paths)")

    def test_12_no_username_in_json(self) -> None:
        """The local username must be redacted from diagnostic JSON."""
        raw = self.json_path.read_text(encoding="utf-8")
        _collect_sensitive_leaks(raw, "diagnostic JSON (username)")

    # ── artifact path format checks ────────────────────────────────────────

    def test_13_artifact_paths_use_forward_slashes(self) -> None:
        """Artifact paths must use '/' separators on all platforms."""
        for mod in self.data["modules"]:
            artifact = mod.get("artifact")
            if artifact is None:
                continue
            assert "\\" not in artifact, (
                f"Module {mod['name']!r}: artifact path must use '/' separators, "
                f"got {artifact!r}"
            )

    def test_14_diagnostic_logd_uses_forward_slashes(self) -> None:
        """The diagnostic_logd reference must use '/' path separators."""
        logd_ref = self.data.get("diagnostic_logd")
        if logd_ref is None:
            self.skipTest("diagnostic_logd is null (no bundle created)")
        assert isinstance(logd_ref, str)
        assert "\\" not in logd_ref, (
            f"diagnostic_logd must use '/' separators, got {logd_ref!r}"
        )

    def test_15_diagnostic_logd_is_repo_relative(self) -> None:
        """The diagnostic_logd reference must be relative to the repo root."""
        logd_ref = self.data.get("diagnostic_logd")
        if logd_ref is None:
            self.skipTest("diagnostic_logd is null (no bundle created)")
        assert not os.path.isabs(logd_ref), (
            f"diagnostic_logd must be repo-relative, got absolute path {logd_ref!r}"
        )

    # ── pair matching ──────────────────────────────────────────────────────

    def test_16_json_logd_pair_matches(self) -> None:
        """The JSON and .logd files must reference the same commit id."""
        data = self.data
        logd_ref = data.get("diagnostic_logd")
        if logd_ref is None:
            self.skipTest("No diagnostic_logd reference (encryptly unavailable)")

        basename = os.path.basename(logd_ref)
        match = re.match(r"build-([0-9a-f]{8})\.logd$", basename)
        assert match is not None, (
            f"Could not parse commit id from diagnostic_logd: {logd_ref!r}"
        )
        logd_commit = match.group(1)
        assert logd_commit == self.commit_id, (
            f"Commit id mismatch: JSON commit={self.commit_id!r}, "
            f"logd reference commit={logd_commit!r}"
        )

    def test_17_logd_file_is_not_empty(self) -> None:
        """The .logd file must not be empty (it should contain encrypted data)."""
        assert self.logd_path.is_file(), f".logd file not found: {self.logd_path}"
        size = self.logd_path.stat().st_size
        assert size > 0, f".logd file is empty (0 bytes): {self.logd_path}"

    # ── build.py source-level checks ───────────────────────────────────────

    def test_18_build_py_has_diagnostic_functions(self) -> None:
        """build.py must contain the expected diagnostic functions."""
        source = BUILD_PY.read_text(encoding="utf-8")
        expected_functions = [
            "diagnostic_paths_for_commit",
            "build_diagnostic_report",
            "write_diagnostic_report",
            "generate_logd",
        ]
        for func in expected_functions:
            assert f"def {func}" in source, (
                f"build.py missing expected function: {func}()"
            )

    def test_19_build_py_uses_relative_to_for_display(self) -> None:
        """build.py must use .relative_to(ROOT) for diagnostic path display."""
        source = BUILD_PY.read_text(encoding="utf-8")
        assert ".relative_to(ROOT)" in source, (
            "build.py should use .relative_to(ROOT) for repo-relative paths"
        )

    def test_20_build_py_commits_diagnostic_artifacts(self) -> None:
        """build.py must commit diagnostic artifacts after generation."""
        source = BUILD_PY.read_text(encoding="utf-8")
        assert "commit_diagnostic_artifacts" in source, (
            "build.py should call commit_diagnostic_artifacts to persist diagnostics"
        )


if __name__ == "__main__":
    unittest.main()
