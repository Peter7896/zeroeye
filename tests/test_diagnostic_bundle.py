"""Tests for diagnostic bundle JSON/logd contract."""
import json
import re
from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parents[1]
DIAG = ROOT / "diagnostic"


def _latest_pair():
    json_files = sorted(DIAG.glob("build-*.json"))
    if not json_files:
        pytest.skip("no diagnostic json")
    meta = json_files[-1]
    commit = meta.stem.replace("build-", "")
    logd = DIAG / f"build-{commit}.logd"
    return meta, logd, commit


def test_diagnostic_json_and_logd_match():
    meta_path, logd_path, commit = _latest_pair()
    assert meta_path.exists(), "missing diagnostic json"
    data = json.loads(meta_path.read_text(encoding="utf-8"))
    assert data.get("commit") == commit
    logd_ref = data.get("diagnostic_logd")
    if logd_ref is None:
        assert data.get("diagnostic_logd_error"), "missing logd must explain error"
        return
    refs = [logd_ref] if isinstance(logd_ref, str) else logd_ref
    assert refs, "empty logd reference list"
    for ref in refs:
        assert ref.replace("\\", "/").startswith("diagnostic/")
        assert (ROOT / ref).exists(), f"missing artifact {ref}"


def test_paths_are_repo_relative_and_redacted():
    meta_path, _, _ = _latest_pair()
    raw = meta_path.read_text(encoding="utf-8")
    data = json.loads(raw)
    blob = json.dumps(data)
    assert "\\Users\\" not in blob and "/home/" not in blob.lower()
    for mod in data.get("modules", []):
        art = mod.get("artifact")
        if art:
            assert "\\" not in art or art.startswith("diagnostic")
            assert art.replace("\\", "/").count("..") == 0


def test_validate_helper_fails_on_mismatch(tmp_path):
    from tools.diagnostic_validate import validate_pair

    bad_json = tmp_path / "build-deadbeef.json"
    bad_json.write_text(json.dumps({"commit": "deadbeef", "diagnostic_logd": "diagnostic/build-cafebabe.logd"}))
    with pytest.raises(SystemExit):
        validate_pair(bad_json, tmp_path)
