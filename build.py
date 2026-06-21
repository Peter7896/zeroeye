import json
import os
from pathlib import Path
from typing import Optional

# ... (truncated) ...

def validate_diagnostic_bundles():
    logd_path, metadata_path, _ = diagnostic_paths_for_commit()
    json_path = metadata_path

    # Check if the JSON file exists
    if not json_path.exists():
        raise FileNotFoundError(f"Expected JSON report not found: {json_path}")

    # Check if the .logd file exists
    if not logd_path.exists():
        raise FileNotFoundError(f"Expected .logd artifact not found: {logd_path}")

    # Validate repository-relative paths
    with open(json_path) as json_file:
        report = json.load(json_file)
        for entry in report['results']:
            if not entry['artifact'].startswith('/'):
                raise ValueError(f"Artifact path is not repository-relative: {entry['artifact']}")

    # Check for redacted paths and usernames
    for entry in report['results']:
        if 'home' in entry['output'] or 'repo' in entry['output']:
            raise ValueError(f"Output contains unredacted paths: {entry['output']}")

    print("Diagnostic bundles validated successfully.")

# ... (truncated) ...

def generate_logd(
    results: list[tuple[str, bool, float, str, Optional[str]]],
    verbose: bool = False,
) -> bool:
    logd_path, metadata_path, commit_id = diagnostic_paths_for_commit()
    display_logd = logd_path.relative_to(ROOT)
    print(f"\n  {{color('▸', Colors.CYAN)}} Finalizing diagnostics for {{color(str(display_logd), Colors.BOLD)}}...")

    # Always write the JSON report first. The encrypted .logd is useful, but the
    # report is required even when the build failed before compilation started or
    # when encryptly itself is unavailable.
    write_diagnostic_report(metadata_path, build_diagnostic_report(results, commit_id))

    # Validate the generated diagnostic bundles
    validate_diagnostic_bundles()

    encryptly_bin = get_encryptly_bin()
    if encryptly_bin is None:
        error = f"encryptly binary not found ({{encryptly_platform_help()}}); cannot create {{display_logd}}"
        print(f"    {{color('✗', Colors.RED)}} {{error}}")
# ... (truncated) ...