# Fix for Issue #1: [$25 BOUNTY] [Python] Add diagnostic bundle validation tests

# tests/conftest.py
import pytest
import os
import sys
import tempfile
import shutil

@pytest.fixture
def temp_diagnostic_dir():
    """Create a temporary diagnostic directory for testing."""
    temp_dir = tempfile.mkdtemp(prefix="diagnostic_test_")
    yield temp_dir
    shutil.rmtree(temp_dir, ignore_errors=True)

@pytest.fixture
def mock_build_context(temp_diagnostic_dir):
    """Provide a mock build context with known paths."""
    return {
        "diagnostic_dir": temp_diagnostic_dir,
        "repo_root": os.path.dirname(os.path.dirname(os.path.abspath(__file__))),
        "home_dir": os.path.expanduser("~"),
        "temp_dir": tempfile.gettempdir(),
        "username": os.environ.get("USER", os.environ.get("USERNAME", "testuser")),
    }