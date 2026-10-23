"""Validate Zeroeye public diagnostic metadata and encrypted log references."""
import hashlib
import json
import re
from pathlib import Path, PurePosixPath


def validate_bundle(metadata_path: Path, root: Path) -> list[Path]:
    root = root.resolve()
    metadata_path = metadata_path.resolve()
    if not metadata_path.is_relative_to(root / "diagnostic"):
        raise ValueError("metadata must be inside diagnostic/")
    if not metadata_path.is_file():
        raise ValueError("diagnostic JSON is missing")
    report = json.loads(metadata_path.read_text(encoding="utf-8"))
    match = re.fullmatch(r"build-([0-9a-f]{8})\.json", metadata_path.name)
    if not isinstance(report, dict):
        raise ValueError("diagnostic JSON must be an object")
    if not match or report.get("commit") != match.group(1):
        raise ValueError("diagnostic JSON commit does not match filename")
    refs = report.get("diagnostic_logd")
    if isinstance(refs, str):
        refs = [refs]
    if not isinstance(refs, list) or not refs or not all(isinstance(x, str) for x in refs):
        raise ValueError("diagnostic .logd reference is missing")
    paths = []
    for ref in refs:
        p = PurePosixPath(ref)
        if "\\" in ref or p.is_absolute() or ".." in p.parts or not ref.startswith("diagnostic/"):
            raise ValueError("diagnostic paths must be repository-relative POSIX paths")
        if not re.fullmatch(rf"diagnostic/build-{match.group(1)}(?:-part\d{{3}})?\.logd", ref):
            raise ValueError("diagnostic .logd reference mismatches JSON commit")
        resolved = (root / ref).resolve()
        if not resolved.is_relative_to(root / "diagnostic") or not resolved.is_file():
            raise ValueError(f"diagnostic .logd file is missing: {ref}")
        hashes = report.get("artifact_sha256")
        if not isinstance(hashes, dict) or not isinstance(hashes.get(ref), str) or not re.fullmatch(r"[0-9a-f]{64}", hashes[ref]):
            raise ValueError(f"diagnostic checksum is missing or invalid: {ref}")
        if hashlib.sha256(resolved.read_bytes()).hexdigest() != hashes[ref]:
            raise ValueError(f"diagnostic checksum mismatch: {ref}")
        paths.append(resolved)
    if len(paths) != len(set(paths)):
        raise ValueError("duplicate diagnostic .logd reference")
    if len(refs) > 1:
        expected_refs = [f"diagnostic/build-{match.group(1)}-part{i:03d}.logd" for i in range(1, len(refs) + 1)]
        if refs != expected_refs:
            raise ValueError("diagnostic chunks must be ordered and contiguous")
    if report.get("chunked") is True and len(refs) < 2:
        raise ValueError("chunked metadata requires multiple diagnostic chunks")
    modules = report.get("modules", [])
    if not isinstance(modules, list) or any(not isinstance(module, dict) for module in modules):
        raise ValueError("modules must be a list of objects")
    for module in modules:
        artifact = module.get("artifact")
        if artifact is not None and (not isinstance(artifact, str) or "\\" in artifact or PurePosixPath(artifact).is_absolute() or ".." in PurePosixPath(artifact).parts):
            raise ValueError("module artifact must be a repository-relative POSIX path")
    sensitive = [
        r"(?i)(?:/home/|/Users/|/root/)[^\s\"/]+/",
        r"(?i)[a-z]:\\Users\\[^\\\" ]+",
        r"(?i)\\\\[^\\\" ]+\\Users\\",
        r"(?i)\b(?:api[_-]?key|access[_-]?token|secret|password)\s*[:=]\s*(?!<REDACTED>)[^\s,;\"}]+",
    ]
    def strings(value):
        if isinstance(value, str):
            yield value
        elif isinstance(value, dict):
            for v in value.values():
                yield from strings(v)
        elif isinstance(value, list):
            for v in value:
                yield from strings(v)
    for value in strings(report):
        if any(re.search(pattern, value) for pattern in sensitive):
            raise ValueError("sensitive local path or credential in diagnostic metadata")
    return paths


def main(argv=None) -> int:
    import argparse
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("metadata", type=Path, help="diagnostic/build-<commit>.json")
    parser.add_argument("--root", type=Path, default=Path.cwd(), help="repository root")
    args = parser.parse_args(argv)
    try:
        paths = validate_bundle(args.metadata, args.root)
    except (ValueError, OSError, json.JSONDecodeError) as exc:
        parser.exit(1, f"Invalid diagnostic bundle: {exc}\n")
    print(f"Valid diagnostic bundle: {len(paths)} encrypted artifact(s)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
