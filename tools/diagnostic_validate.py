#!/usr/bin/env python3
"""Validate diagnostic JSON / logd pairing."""
from __future__ import annotations

import json
import sys
from pathlib import Path


def validate_pair(meta_path: Path, root: Path) -> None:
    if not meta_path.exists():
        print(f"missing json: {meta_path}", file=sys.stderr)
        sys.exit(1)
    data = json.loads(meta_path.read_text(encoding="utf-8"))
    commit = data.get("commit")
    if not commit:
        print("json missing commit", file=sys.stderr)
        sys.exit(1)
    logd_ref = data.get("diagnostic_logd")
    if logd_ref is None:
        if not data.get("diagnostic_logd_error"):
            print("missing logd and no diagnostic_logd_error", file=sys.stderr)
            sys.exit(1)
        return
    refs = [logd_ref] if isinstance(logd_ref, str) else list(logd_ref)
    for ref in refs:
        p = root / ref.replace("\\", "/")
        if not p.exists():
            print(f"missing logd artifact: {ref}", file=sys.stderr)
            sys.exit(1)
        if f"build-{commit}" not in ref.replace("\\", "/"):
            print(f"logd ref {ref} does not match commit {commit}", file=sys.stderr)
            sys.exit(1)


def main() -> None:
    root = Path(__file__).resolve().parents[1]
    diag = root / "diagnostic"
    jsons = sorted(diag.glob("build-*.json"))
    if not jsons:
        print("no diagnostic json files", file=sys.stderr)
        sys.exit(1)
    validate_pair(jsons[-1], root)
    print("diagnostic pair ok")


if __name__ == "__main__":
    main()
