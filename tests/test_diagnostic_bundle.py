# Fix for Issue #1: [$25 BOUNTY] [Python] Add diagnostic bundle validation tests

CONFIDENCE: 0.75

SOLUTION_SUMMARY: Added comprehensive diagnostic bundle validation tests that verify JSON/logd contract integrity, path redaction, repository-relative paths, and cross-platform determinism without relying on live services.

FILES_CHANGED:
- tests/test_diagnostic_bundle.py
- tests/conftest.py
- diagnostic/.gitkeep

CODE:
```python
### FILE: tests/test_diagnostic_bundle.py
"""
Diagnostic bundle validation tests.

This module provides deterministic tests that validate:
- Matching JSON and .logd file pairs
- Repository-relative path formatting
- Redaction of sensitive local paths and usernames
- Proper failure modes for missing/mismatched artifacts
"""

import json
import os
import re
import sys
import tempfile
import platform
from pathlib import Path
from typing import Dict, List, Optional, Tuple
from unittest.mock import patch, MagicMock

import pytest


# Determine repository root relative to this test file
REPO_ROOT = Path(__file__).parent.parent.resolve()
DIAGNOSTIC_DIR = REPO_ROOT / "diagnostic"


class DiagnosticBundleValidator:
    """Validates diagnostic bundle JSON and logd file pairs."""
    
    # Patterns for detecting unredacted sensitive paths
    SENSITIVE_PATH_PATTERNS = [
        # Home directories
        re.compile(r'(/home/[^/\s"]+|/Users/[^/\s"]+|C:\\Users\\[^\\"\s]+)', re.IGNORECASE),
        # Temp directories with user info
        re.compile(r'(/tmp/[^/\s"]*user[^/\s"]*|/var/folders/[^/\s"]+)', re.IGNORECASE),
        # Windows temp with username
        re.compile(r'(C:\\(?:Users\\[^\\]+\\)?(?:AppData\\Local\\)?Temp)', re.IGNORECASE),
    ]
    
    # Pattern for detecting usernames (common environment variables)
    USERNAME_PATTERNS = [
        re.compile(r'"(?:user|username|USER|USERNAME)":\s*"([^"]+)"'),
    ]
    
    # Expected redaction placeholders
    REDACTION_PLACEHOLDERS = ['<REDACTED>', '<HOME>', '<TEMP>', '<USER>', '<REPO>']
    
    def __init__(self, diagnostic_dir: Path):
        self.diagnostic_dir = Path(diagnostic_dir)
        
    def find_bundle_pairs(self) -> List[Tuple[Path, Path]]:
        """Find all matching JSON and logd file pairs."""
        pairs = []
        
        if not self.diagnostic_dir.exists():
            return pairs
            
        json_files = list(self.diagnostic_dir.glob("build-*.json"))
        
        for json_file in json_files:
            # Extract the identifier (e.g., build-XXX)
            stem = json_file.stem
            logd_file = self.diagnostic_dir /