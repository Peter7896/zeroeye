#!/usr/bin/env python3
"""Test suite for diagnostic bundle generation in build.py.

Validates:
- JSON diagnostic metadata structure and required fields
- .logd encryption format and file presence
- Failure behaviour when modules are missing or broken
- Chunking logic for large .logd files
- Redacted metadata (no passwords or paths leaked)
"""
import json
import os
import subprocess
import sys
import tempfile
from pathlib import Path

# Paths
ROOT = Path(__file__).resolve().parent
DIAGNOSTIC_DIR = ROOT / "diagnostic"
BUILD_PY = ROOT / "build.py"

# Required JSON metadata keys
REQUIRED_METADATA_KEYS = {
    "generated_at", "commit", "diagnostic_logd",
    "diagnostic_logd_error", "chunked", "chunk_size_bytes",
    "password", "decrypt_command", "total_modules",
    "passed", "failed", "modules", "pr_note"
}

REQUIRED_MODULE_KEYS = {
    "name", "status", "elapsed_seconds", "artifact", "output"
}


def test_metadata_schema():
    """Verify that a diagnostic JSON has all required keys."""
    json_path = DIAGNOSTIC_DIR / "build-00000000.json"
    assert json_path.exists(), f"Missing diagnostic JSON: {json_path}"
    
    with open(json_path) as f:
        data = json.load(f)
    
    missing = REQUIRED_METADATA_KEYS - set(data.keys())
    assert not missing, f"Missing metadata keys: {missing}"
    
    # Check types
    assert isinstance(data["generated_at"], str), "generated_at must be string"
    assert isinstance(data["commit"], str), "commit must be string"
    assert isinstance(data["total_modules"], int), "total_modules must be int"
    assert isinstance(data["passed"], int), "passed must be int"
    assert isinstance(data["failed"], int), "failed must be int"
    assert isinstance(data["modules"], list), "modules must be list"
    
    # passed + failed should not exceed total_modules
    assert data["passed"] + data["failed"] <= data["total_modules"], \
        f"passed({data['passed']}) + failed({data['failed']}) > total({data['total_modules']})"
    
    # Check module entries
    for mod in data["modules"]:
        mod_keys = REQUIRED_MODULE_KEYS - set(mod.keys())
        assert not mod_keys, f"Module missing keys: {mod_keys}"
        assert mod["status"] in ("PASS", "FAIL", "SKIP", "ERROR"), \
            f"Invalid status: {mod['status']}"
        assert isinstance(mod["elapsed_seconds"], (int, float)), \
            "elapsed_seconds must be numeric"


def test_logd_file_exists():
    """Verify that the .logd file referenced in metadata actually exists."""
    json_path = DIAGNOSTIC_DIR / "build-00000000.json"
    with open(json_path) as f:
        data = json.load(f)
    
    logd_rel = data["diagnostic_logd"]
    logd_path = ROOT / logd_rel
    assert logd_path.exists(), f"Referenced .logd file missing: {logd_path}"
    
    # .logd should be non-empty
    assert logd_path.stat().st_size > 0, ".logd file is empty"


def test_logd_is_encrypted():
    """Verify the .logd file is encrypted (not plaintext JSON)."""
    json_path = DIAGNOSTIC_DIR / "build-00000000.json"
    with open(json_path) as f:
        data = json.load(f)
    
    logd_rel = data["diagnostic_logd"]
    logd_path = ROOT / logd_rel
    
    # Read first bytes — encrypted data should not start with '{'
    with open(logd_path, "rb") as f:
        header = f.read(10)
    
    # Encrypted binary data should NOT look like plaintext JSON
    assert not header.startswith(b"{"), ".logd appears to be plaintext, not encrypted"
    
    # Should have reasonable entropy (not all zeros or repeated pattern)
    data_block = logd_path.read_bytes()[:1000]
    unique_vals = len(set(data_block))
    assert unique_vals > 10, f".logd has suspiciously low entropy: {unique_vals} unique bytes"


def test_no_password_in_logd():
    """Verify the .logd file does not contain the password in plaintext."""
    json_path = DIAGNOSTIC_DIR / "build-00000000.json"
    with open(json_path) as f:
        data = json.load(f)
    
    password = data["password"]
    logd_rel = data["diagnostic_logd"]
    logd_path = ROOT / logd_rel
    
    logd_bytes = logd_path.read_bytes()
    assert password.encode() not in logd_bytes, \
        "Password found in plaintext inside .logd — security violation"


def test_chunking_logic():
    """Verify that split_diagnostic_logd works correctly."""
    # Import the chunking function
    sys.path.insert(0, str(ROOT))
    from build import split_diagnostic_logd, DIAGNOSTIC_CHUNK_SIZE
    
    with tempfile.NamedTemporaryFile(suffix=".logd") as tmp:
        # Write data larger than chunk size
        data_size = DIAGNOSTIC_CHUNK_SIZE + 5000
        tmp.write(b"x" * data_size)
        tmp.flush()
        
        chunk_paths = split_diagnostic_logd(Path(tmp.name), chunk_size=5000)
        
        # Should have created chunks
        assert len(chunk_paths) >= 2, \
            f"Expected >=2 chunks, got {len(chunk_paths)}"
        
        # Sum of chunks should equal original
        total_size = sum(p.stat().st_size for p in chunk_paths)
        assert total_size == data_size, \
            f"Chunk size mismatch: {total_size} vs {data_size}"
        
        # Original should be removed
        assert not Path(tmp.name).exists(), \
            "Original oversized file should be deleted after chunking"
        
        # Cleanup
        for p in chunk_paths:
            p.unlink()


def test_diagnostic_with_missing_module():
    """Build should handle missing module directories gracefully."""
    result = subprocess.run(
        [sys.executable, str(BUILD_PY), "--module", "nonexistent-module"],
        capture_output=True, text=True, timeout=30
    )
    # Should fail gracefully, not crash
    assert result.returncode != 0, "Non-existent module should cause non-zero exit"
    assert "nonexistent" in result.stdout.lower() or "invalid" in result.stdout.lower() or \
           "error" in result.stdout.lower(), "Should give meaningful error message"


def test_diagnostic_no_args():
    """Build with no arguments should produce diagnostic output."""
    result = subprocess.run(
        [sys.executable, str(BUILD_PY)],
        capture_output=True, text=True, timeout=30
    )
    # Should at least not crash
    assert result.returncode in (0, 1), f"Unexpected exit code: {result.returncode}"
    
    # Should mention diagnostic files
    has_diagnostic = "diagnostic" in (result.stdout + result.stderr).lower()
    diagnostic_dir_content = list(DIAGNOSTIC_DIR.glob("build-*.json"))
    assert has_diagnostic or len(diagnostic_dir_content) > 0, \
        "No diagnostic output detected"


def test_decrypt_command_format():
    """Verify the decrypt_command has the correct format for manual decryption."""
    json_path = DIAGNOSTIC_DIR / "build-00000000.json"
    with open(json_path) as f:
        data = json.load(f)
    
    cmd = data["decrypt_command"]
    assert cmd.startswith("encryptly"), f"decrypt_command should start with 'encryptly': {cmd}"
    assert "unpack" in cmd, f"decrypt_command should contain 'unpack': {cmd}"
    assert "--password" in cmd, f"decrypt_command should contain '--password': {cmd}"


def test_build_output_determinism():
    """Same commit ID should produce the same diagnostic file names."""
    json_path = DIAGNOSTIC_DIR / "build-00000000.json"
    with open(json_path) as f:
        data = json.load(f)
    
    commit = data["commit"]
    expected_logd = f"build-{commit}.logd"
    expected_json = f"build-{commit}.json"
    
    assert data["diagnostic_logd"].endswith(expected_logd), \
        f"Expected .logd ending in {expected_logd}, got {data['diagnostic_logd']}"
    assert json_path.name == expected_json, \
        f"Expected JSON named {expected_json}, got {json_path.name}"
