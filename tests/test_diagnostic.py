"""
Regression tests for ZeroEye diagnostic bundle metadata.

Issue #1 coverage:
1. Matching diagnostic/build-*.json and diagnostic/build-*.logd.
2. JSON references the matching .logd file.
3. Artifact paths are repository-relative and use '/'.
4. Local home/repository/temp paths and host usernames are not leaked.
5. Missing JSON, missing .logd, mismatched pairs, and wrong references fail clearly.
6. Checks are deterministic on Windows and Unix-like hosts.

Only the Python standard library is required.
"""
from __future__ import annotations

import json
import os
import re
import tempfile
import unittest
from pathlib import Path
from typing import Any, Iterable

#Using a fixed path compatible with the Colab environment to avoid the `__file__` error.
if '__file__' in globals():
    REPO_ROOT = Path(__file__).resolve().parents[1]
else:
    REPO_ROOT = Path.cwd()
DIAGNOSTIC_DIR = REPO_ROOT / "diagnostic"
JSON_PATTERN = "build-*.json"
LOGD_PATTERN = "build-*.logd"

def _all_strings(value: Any) -> Iterable[str]:
    """Yield every string contained in a JSON-compatible object."""
    if isinstance(value, str):
        yield value
    elif isinstance(value, dict):
        for key, item in value.items():
            yield from _all_strings(key)
            yield from _all_strings(item)
    elif isinstance(value, (list, tuple)):
        for item in value:
            yield from _all_strings(item)

def _artifact_path_strings(value: Any) -> Iterable[str]:
    """
    Yield strings that represent artifact/path values.
    """
    path_key_re = re.compile(
        r"(?:^|_)(?:path|paths|artifact|artifacts|file|files|output|outputs|location)(?:$|_)",
        re.IGNORECASE,
    )

    def walk(obj: Any) -> Iterable[str]:
        if isinstance(obj, dict):
            for key, item in obj.items():
                key_text = str(key)
                if isinstance(item, str) and (
                    path_key_re.search(key_text)
                    or "/" in item
                    or "\\" in item
                ):
                    yield item
                yield from walk(item)
        elif isinstance(obj, (list, tuple)):
            for item in obj:
                yield from walk(item)

    yield from walk(value)

def _normalize_path_text(value: str) -> str:
    """Normalize path separators for cross-platform comparisons."""
    return value.replace("\\", "/")

def _is_absolute_or_drive_path(value: str) -> bool:
    """Detect POSIX, UNC, and Windows drive absolute paths."""
    normalized = _normalize_path_text(value)

    if normalized.startswith("/"):
        return True
    if normalized.startswith("//"):
        return True
    if re.match(r"^[A-Za-z]:/", normalized):
        return True
    return False

def _sensitive_path_tokens() -> list[str]:
    """
    Return host-specific values that must not occur in diagnostic metadata.
    """
    tokens: list[str] = []

    username = os.environ.get("USERNAME") or os.environ.get("USER")
    if username:
        tokens.append(username)

    paths = (
        Path.home(),
        Path(tempfile.gettempdir()),
        REPO_ROOT,
    )

    for path in paths:
        try:
            tokens.extend([str(path), str(path.resolve())])
        except OSError:
            tokens.append(str(path))

    unique: list[str] = []
    seen: set[str] = set()

    for token in tokens:
        if not token:
            continue

        normalized = _normalize_path_text(token).rstrip("/")
        key = normalized.casefold()

        if key not in seen:
            seen.add(key)
            unique.append(normalized)

    return unique

def _load_json(path: Path) -> Any:
    with path.open("r", encoding="utf-8") as handle:
        return json.load(handle)

def _json_references_logd(metadata: Any, logd_name: str) -> bool:
    """Return True when metadata contains the matching .logd filename."""
    expected = logd_name.casefold()

    for text in _all_strings(metadata):
        normalized = _normalize_path_text(text).casefold()
        if expected in normalized:
            return True

    return False

def validate_diagnostic_directory(
    diagnostic_dir: Path,
) -> tuple[list[Path], list[Path]]:
    """
    Validate the generated diagnostic directory.
    """
    json_files = sorted(diagnostic_dir.glob(JSON_PATTERN))
    logd_files = sorted(diagnostic_dir.glob(LOGD_PATTERN))

    if not json_files:
        raise AssertionError(f"Missing diagnostic JSON: expected {diagnostic_dir / JSON_PATTERN}")

    if not logd_files:
        raise AssertionError(f"Missing diagnostic .logd: expected {diagnostic_dir / LOGD_PATTERN}")

    json_by_stem = {path.stem: path for path in json_files}
    logd_by_stem = {path.name.removesuffix(".logd"): path for path in logd_files}

    missing_logd = sorted(set(json_by_stem) - set(logd_by_stem))
    missing_json = sorted(set(logd_by_stem) - set(json_by_stem))

    if missing_logd or missing_json:
        details = []
        if missing_logd:
            details.append("JSON without matching .logd: " + ", ".join(missing_logd))
        if missing_json:
            details.append(".logd without matching JSON: " + ", ".join(missing_json))
        raise AssertionError("Mismatched diagnostic bundle pair: " + "; ".join(details))

    for stem, json_path in sorted(json_by_stem.items()):
        logd_path = logd_by_stem[stem]
        metadata = _load_json(json_path)

        if not _json_references_logd(metadata, logd_path.name):
            raise AssertionError(f"{json_path} does not reference its matching diagnostic log {logd_path.name!r}")

        strings = list(_all_strings(metadata))

        for sensitive in _sensitive_path_tokens():
            sensitive_normalized = _normalize_path_text(sensitive).casefold()
            for text in strings:
                text_normalized = _normalize_path_text(text).casefold()
                if sensitive_normalized and sensitive_normalized in text_normalized:
                    raise AssertionError(f"Sensitive local path/username leaked in {json_path}: {sensitive!r}")

        for artifact_path in _artifact_path_strings(metadata):
            normalized = _normalize_path_text(artifact_path)
            if "\\" in artifact_path:
                raise AssertionError(f"Artifact path is not slash-normalized in {json_path}: {artifact_path!r}")
            if _is_absolute_or_drive_path(normalized):
                raise AssertionError(f"Artifact path is absolute instead of repository-relative in {json_path}: {artifact_path!r}")

    return json_files, logd_files

class TestDiagnosticBundleValidation(unittest.TestCase):
    def test_generated_bundle_is_a_matching_pair(self) -> None:
        json_files, logd_files = validate_diagnostic_directory(DIAGNOSTIC_DIR)
        self.assertGreaterEqual(len(json_files), 1)
        self.assertEqual(
            {p.stem for p in json_files},
            {p.name.removesuffix(".logd") for p in logd_files},
        )

    def test_metadata_contract_is_valid(self) -> None:
        validate_diagnostic_directory(DIAGNOSTIC_DIR)

class TestDiagnosticFailureModes(unittest.TestCase):
    def _write_json(self, directory: Path, name: str, logd_name: str, *, artifact_path: str | None = None) -> None:
        payload = {
            "artifact_path": artifact_path or f"diagnostic/{logd_name}",
            "diagnostic_log": logd_name,
        }
        (directory / name).write_text(json.dumps(payload), encoding="utf-8")

    def test_missing_json_fails_clearly(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            directory = Path(tmp)
            (directory / "build-ABC.logd").write_bytes(b"encrypted")
            with self.assertRaisesRegex(AssertionError, "Missing diagnostic JSON"):
                validate_diagnostic_directory(directory)

    def test_missing_logd_fails_clearly(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            directory = Path(tmp)
            self._write_json(directory, "build-ABC.json", "build-ABC.logd")
            with self.assertRaisesRegex(AssertionError, r"Missing diagnostic \.logd"):
                validate_diagnostic_directory(directory)

    def test_mismatched_pair_fails_clearly(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            directory = Path(tmp)
            self._write_json(directory, "build-ABC.json", "build-ABC.logd")
            (directory / "build-XYZ.logd").write_bytes(b"encrypted")
            with self.assertRaisesRegex(AssertionError, "Mismatched diagnostic bundle pair"):
                validate_diagnostic_directory(directory)

    def test_wrong_json_log_reference_fails_clearly(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            directory = Path(tmp)
            self._write_json(directory, "build-ABC.json", "build-XYZ.logd")
            (directory / "build-ABC.logd").write_bytes(b"encrypted")
            with self.assertRaisesRegex(AssertionError, "does not reference its matching"):
                validate_diagnostic_directory(directory)

    def test_windows_absolute_artifact_path_fails(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            directory = Path(tmp)
            self._write_json(directory, "build-ABC.json", "build-ABC.logd", artifact_path=r"C:\Users\alice\zeroeye\diagnostic\build-ABC.logd")
            (directory / "build-ABC.logd").write_bytes(b"encrypted")
            with self.assertRaisesRegex(AssertionError, "Artifact path is not slash-normalized"):
                validate_diagnostic_directory(directory)

    def test_unix_absolute_artifact_path_fails(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            directory = Path(tmp)
            self._write_json(directory, "build-ABC.json", "build-ABC.logd", artifact_path="/home/alice/zeroeye/diagnostic/build-ABC.logd")
            (directory / "build-ABC.logd").write_bytes(b"encrypted")
            with self.assertRaisesRegex(AssertionError, "Artifact path is absolute"):
                validate_diagnostic_directory(directory)

    def test_relative_forward_slash_artifact_path_is_accepted(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            directory = Path(tmp)
            self._write_json(directory, "build-ABC.json", "build-ABC.logd", artifact_path="diagnostic/build-ABC.logd")
            (directory / "build-ABC.logd").write_bytes(b"encrypted")
            validate_diagnostic_directory(directory)

if __name__ == '__main__':
    unittest.main(argv=['first-arg-is-ignored'], verbosity=2, exit=False)
