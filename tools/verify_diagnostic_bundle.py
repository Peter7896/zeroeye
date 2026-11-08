#!/usr/bin/env python3
"""Validate public diagnostic metadata and file references, not encryption contents."""
from __future__ import annotations

import argparse
import getpass
import json
import platform
import re
import tempfile
from pathlib import Path, PurePosixPath, PureWindowsPath
from typing import Any

ROOT = Path(__file__).resolve().parents[1]


def _is_repo_relative_posix_path(value: str) -> bool:
    path = PurePosixPath(value)
    return (bool(value) and value != "." and "\\" not in value
            and not path.is_absolute() and not PureWindowsPath(value).drive
            and ".." not in path.parts and path.as_posix() == value)


def _sensitive_tokens(root: Path) -> list[str]:
    values = {str(root), str(root.resolve()), str(Path.home()), tempfile.gettempdir(),
              getpass.getuser(), platform.node()}
    tokens = set()
    for value in values:
        if value and value not in ("/", "\\"):
            tokens.update((value, value.replace("\\", "/"), value.replace("/", "\\")))
    return sorted(tokens, key=len, reverse=True)


def _strings(value: Any):
    if isinstance(value, str):
        yield value
    elif isinstance(value, list):
        for item in value:
            yield from _strings(item)
    elif isinstance(value, dict):
        for key, item in value.items():
            yield key
            yield from _strings(item)


def validate_diagnostic_bundle(metadata_path: Path, root: Path = ROOT) -> list[str]:
    errors: list[str] = []
    if not metadata_path.is_file():
        return [f"missing diagnostic JSON: {metadata_path}"]
    try:
        report = json.loads(metadata_path.read_text(encoding="utf-8"))
    except (OSError, UnicodeError, json.JSONDecodeError) as exc:
        return [f"invalid diagnostic JSON: {exc}"]
    if not isinstance(report, dict):
        return ["diagnostic JSON must be an object"]

    reference = report.get("diagnostic_logd")
    paths = [reference] if isinstance(reference, str) else reference
    if not isinstance(paths, list) or not paths or not all(isinstance(p, str) and p for p in paths):
        return ["diagnostic_logd is missing or not a nonempty string/list of strings"]
    if len(set(paths)) != len(paths):
        errors.append("diagnostic_logd contains duplicate references")
    prefix = metadata_path.stem
    commit = report.get("commit")
    if not isinstance(commit, str) or not commit or prefix != f"build-{commit}":
        errors.append("diagnostic JSON filename does not match its commit")
    chunk_numbers = []
    single = False
    for value in paths:
        if not _is_repo_relative_posix_path(value) or PurePosixPath(value).parent != PurePosixPath("diagnostic"):
            errors.append(f"diagnostic_logd must be a repository-relative / path under diagnostic/: {value}")
            continue
        artifact = root / value
        if not artifact.is_file():
            errors.append(f"referenced .logd artifact is missing: {value}")
        elif not artifact.resolve().is_relative_to(root.resolve()):
            errors.append(f"referenced .logd artifact resolves outside repository: {value}")
        elif artifact.stat().st_size == 0:
            errors.append(f"referenced .logd artifact is empty: {value}")
        filename = PurePosixPath(value).name
        match = re.fullmatch(re.escape(prefix) + r"-part([0-9]{3,})\.logd", filename)
        if filename == prefix + ".logd":
            single = True
        elif match:
            index = int(match.group(1))
            if index < 1 or match.group(1) != f"{index:03d}":
                errors.append(f"invalid diagnostic chunk number: {value}")
            chunk_numbers.append(index)
        else:
            errors.append(f"diagnostic JSON/logd pair mismatch: {metadata_path.name} -> {value}")
    if single and len(paths) != 1:
        errors.append("diagnostic bundle cannot mix a single logd with chunks")
    if chunk_numbers and chunk_numbers != list(range(1, len(paths) + 1)):
        errors.append("diagnostic chunks must be ordered and consecutive from part001")

    modules = report.get("modules", [])
    if not isinstance(modules, list) or not all(isinstance(m, dict) for m in modules):
        errors.append("modules must be a list of objects")
    else:
        for module in modules:
            artifact = module.get("artifact")
            if artifact is not None and (not isinstance(artifact, str) or not _is_repo_relative_posix_path(artifact)):
                errors.append("module artifact must be a repository-relative / path")

    for token in _sensitive_tokens(root):
        pattern = r"(?<![\w.-])" + re.escape(token) + r"(?![\w.-])"
        flags = re.IGNORECASE if PureWindowsPath(token).drive else 0
        if any(re.search(pattern, value, flags=flags) for value in _strings(report)):
            # Do not reproduce the leaked identifier in the error output.
            errors.append("diagnostic metadata leaks local value")
            break
    return errors


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("metadata", type=Path, help="Path to diagnostic/build-*.json")
    args = parser.parse_args()
    errors = validate_diagnostic_bundle(args.metadata)
    for error in errors:
        print(f"ERROR: {error}")
    if errors:
        return 1
    print(f"OK: {args.metadata}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
