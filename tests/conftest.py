import pytest
import json
from pathlib import Path

FIXTURES_DIR = Path(__file__).parent / "fixtures"


@pytest.fixture
def example_metadata() -> dict:
    """加载示例诊断元数据 JSON。"""
    path = FIXTURES_DIR / "example-metadata.json"
    with open(path, "r") as f:
        return json.load(f)


@pytest.fixture
def example_logd_path() -> Path:
    """返回示例加密捆绑文件的路径。"""
    return FIXTURES_DIR / "example-build.logd"
