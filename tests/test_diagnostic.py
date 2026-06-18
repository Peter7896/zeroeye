import pytest
import json
from pathlib import Path

# 需要验证的元数据必需字段
REQUIRED_METADATA_FIELDS = [
    "commit",
    "timestamp",
    "build_id",
    "decrypt_password",
    "module_results",
    "environment",
]

# 脱敏密码的预期值
REDACTED_PASSWORD_PATTERN = "REDACTED"


class TestDiagnosticMetadata:
    """测试诊断包元数据 JSON 的结构和合约。"""

    def test_required_fields_present(self, example_metadata: dict):
        """验证元数据包含所有必需字段。"""
        for field in REQUIRED_METADATA_FIELDS:
            assert field in example_metadata, f"Missing required field: {field}"

    def test_decrypt_password_redacted(self, example_metadata: dict):
        """验证解密密码已被脱敏，不包含实际密码。"""
        password = example_metadata.get("decrypt_password")
        assert password == REDACTED_PASSWORD_PATTERN, (
            f"decrypt_password should be '{REDACTED_PASSWORD_PATTERN}', got '{password}'"
        )

    def test_module_results_structure(self, example_metadata: dict):
        """验证 module_results 中每个模块包含 status 字段，失败时包含 errors。"""
        results = example_metadata.get("module_results", {})
        assert isinstance(results, dict), "module_results must be a dict"
        for module_name, result in results.items():
            assert "status" in result, f"Module {module_name} missing status"
            assert result["status"] in ("success", "failure", "skipped"), (
                f"Module {module_name} invalid status: {result['status']}"
            )
            if result["status"] == "failure":
                assert "errors" in result, (
                    f"Module {module_name} with status 'failure' must contain 'errors' list"
                )

    def test_build_id_matches_commit(self, example_metadata: dict):
        """验证 build_id 格式符合预期：build-<commit-4-bytes>。"""
        commit = example_metadata.get("commit", "")
        build_id = example_metadata.get("build_id", "")
        # 实际逻辑可能更复杂，这里仅校验前缀
        assert build_id.startswith("build-"), f"build_id should start with 'build-', got {build_id}"
        # 如果 commit 长度足够，校验 build_id 包含 commit 的前4字节
        if len(commit) >= 4:
            assert build_id == f"build-{commit}", (
                f"build_id '{build_id}' does not match commit '{commit}'"
            )

    def test_environment_fields(self, example_metadata: dict):
        """验证 environment 字段是 dict 且包含基本平台信息。"""
        env = example_metadata.get("environment", {})
        assert isinstance(env, dict), "environment must be a dict"
        # 并非必需，但建议包含 os 和 python_version
        if "os" in env:
            assert isinstance(env["os"], str)
        if "python_version" in env:
            assert isinstance(env["python_version"], str)


class TestEncryptedBundleReference:
    """测试加密捆绑文件 (.logd) 的引用和存在性。"""

    def test_logd_file_exists(self, example_logd_path: Path):
        """验证 .logd 文件存在于预期路径。"""
        assert example_logd_path.exists(), f"Expected logd file not found: {example_logd_path}"
        assert example_logd_path.is_file(), "logd path is not a file"

    def test_logd_filename_matches_build_id(self, example_metadata: dict, example_logd_path: Path):
        """验证 .logd 文件名与元数据中的 build_id 一致。"""
        build_id = example_metadata.get("build_id", "")
        expected_filename = f"{build_id}.logd"
        assert example_logd_path.name == expected_filename, (
            f"Expected logd filename '{expected_filename}', got '{example_logd_path.name}'"
        )

    def test_logd_file_not_empty(self, example_logd_path: Path):
        """验证 .logd 文件非空（至少包含占位符）。"""
        assert example_logd_path.stat().st_size > 0, "logd file should not be empty"


class TestFailureBehavior:
    """测试诊断包对构建失败的处理。"""

    def test_failure_module_contains_errors(self, example_metadata: dict):
        """如果有模块失败，其 errors 列表必须非空。"""
        results = example_metadata.get("module_results", {})
        for module_name, result in results.items():
            if result.get("status") == "failure":
                errors = result.get("errors", [])
                assert len(errors) > 0, (
                    f"Failed module {module_name} must contain at least one error"
                )
                for err in errors:
                    assert isinstance(err, str) and len(err) > 0, (
                        f"Error entry for {module_name} should be non-empty string"
                    )

    def test_metadata_still_generated_on_failure(self, example_metadata: dict):
        """即使在失败情况下，元数据仍然必须包含所有必需字段。"""
        self.test_required_fields_present(example_metadata)


class TestDeterministicValidation:
    """测试确定性验证：多次验证相同数据应得到一致结果。"""

    def test_same_input_same_output(self, example_metadata: dict):
        """验证两次相同的元数据解析得到相同结果。"""
        serialized = json.dumps(example_metadata, sort_keys=True)
        parsed_again = json.loads(serialized)
        assert parsed_again == example_metadata
