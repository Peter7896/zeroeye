#!/usr/bin/env python3

from __future__ import annotations

import argparse
import getpass
import json
import os
import platform
import tempfile
from pathlib import Path, PurePosixPath
from typing import Any


ROOT = Path(__file__).resolve().parents[1]


def _as_list(value: Any) -> list[str]:
    if value is None:
        return []
    if isinstance(value, str):
        return [value]
    if isinstance(value, list) and all(isinstance(item, str) for item in value):
        return value
    return []


def _is_repo_relative_posix_path(value: str) -> bool:
    if "\\" in value:
        return False
    path = PurePosixPath(value)
    return not path.is_absolute() and ".." not in path.parts and value.startswith("diagnostic/")


def _sensitive_tokens(root: Path) -> list[str]:
    candidates = {
        str(root),
        str(root.resolve()),
        str(Path.home()),
        tempfile.gettempdir(),
        getpass.getuser(),
        platform.node(),
    }
    return sorted({token for token in candidates if token and len(token) >= 3}, key=len, reverse=True)


def validate_diagnostic_bundle(metadata_path: Path, root: Path = ROOT) -> list[str]:
    errors: list[str] = []
    if not metadata_path.exists():
        return [f"missing diagnostic JSON: {metadata_path}"]

    try:
        report = json.loads(metadata_path.read_text(encoding="utf-8"))
    except json.JSONDecodeError as exc:
        return [f"invalid diagnostic JSON: {exc}"]

    logd_paths = _as_list(report.get("diagnostic_logd"))
    if not logd_paths:
        errors.append("diagnostic_logd is missing or not a string/list")

    for logd_path in logd_paths:
        if not _is_repo_relative_posix_path(logd_path):
            errors.append(f"diagnostic_logd must be a repository-relative / path: {logd_path}")
            continue
        absolute_logd = root / Path(logd_path)
        if not absolute_logd.exists():
            errors.append(f"referenced .logd artifact is missing: {logd_path}")
        if absolute_logd.suffix != ".logd":
            errors.append(f"referenced diagnostic artifact must end with .logd: {logd_path}")

    expected_prefix = metadata_path.stem
    for logd_path in logd_paths:
        if Path(logd_path).stem != expected_prefix and not Path(logd_path).stem.startswith(f"{expected_prefix}-part"):
            errors.append(f"diagnostic JSON/logd pair mismatch: {metadata_path.name} -> {logd_path}")

    serialized = json.dumps(report, sort_keys=True)
    for token in _sensitive_tokens(root):
        escaped_token = json.dumps(token)[1:-1]
        if token in serialized or escaped_token in serialized:
            errors.append(f"diagnostic metadata leaks local value: {token}")

    return errors


def main() -> int:
    parser = argparse.ArgumentParser(description="Validate diagnostic JSON/logd bundle pairing")
    parser.add_argument("metadata", type=Path, help="Path to diagnostic/build-*.json")
    args = parser.parse_args()

    errors = validate_diagnostic_bundle(args.metadata)
    if errors:
        for error in errors:
            print(f"ERROR: {error}")
        return 1
    print(f"OK: {args.metadata}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
