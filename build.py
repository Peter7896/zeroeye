#!/usr/bin/env python3
from __future__ import annotations

import argparse
import datetime
import getpass
import hashlib
import json
import os
import platform
import re
import tempfile
import shutil
import subprocess
import sys
import time
from dataclasses import dataclass
from pathlib import Path, PurePosixPath, PureWindowsPath
from typing import Optional

ROOT = Path(__file__).resolve().parent
DIAGNOSTIC_DIR = ROOT / "diagnostic"
DIAGNOSTIC_CHUNK_SIZE = 40 * 1024 * 1024


def current_commit_id() -> str:
    """Return the first 4 bytes (8 hex chars) of HEAD for stable per-commit diagnostics."""
    try:
        result = subprocess.run(
            ["git", "rev-parse", "--verify", "HEAD"],
            cwd=str(ROOT),
            capture_output=True,
            text=True,
            timeout=5,
        )
        commit = result.stdout.strip()
        if result.returncode == 0 and len(commit) >= 8:
            return commit[:8]
    except Exception:
        pass
    return "00000000"


def diagnostic_paths_for_commit() -> tuple[Path, Path, str]:
    """Return stable diagnostic artifact paths under diagnostic/ for the current commit."""
    DIAGNOSTIC_DIR.mkdir(parents=True, exist_ok=True)
    commit_id = current_commit_id()
    logd_path = DIAGNOSTIC_DIR / f"build-{commit_id}.logd"
    metadata_path = DIAGNOSTIC_DIR / f"build-{commit_id}.json"
    return logd_path, metadata_path, commit_id


def split_diagnostic_logd(logd_path: Path, chunk_size: int = DIAGNOSTIC_CHUNK_SIZE) -> list[Path]:
    """Split an oversized .logd into numbered .logd chunks and remove the original."""
    if logd_path.stat().st_size <= chunk_size:
        return [logd_path]

    chunks: list[Path] = []
    stem = logd_path.stem
    with logd_path.open("rb") as source:
        index = 1
        while True:
            data = source.read(chunk_size)
            if not data:
                break
            chunk_path = logd_path.with_name(f"{stem}-part{index:03d}.logd")
            with chunk_path.open("xb") as destination:
                destination.write(data)
            chunks.append(chunk_path)
            index += 1

    logd_path.unlink()
    return chunks


@dataclass
class Module:
    name: str
    language: str
    dir: Path
    build_cmd: list[str]
    clean_cmd: list[str]
    build_dir: Optional[Path] = None
    env: Optional[dict[str, str]] = None

MODULES = [
    Module(
        name="backend",
        language="Rust",
        dir=ROOT / "backend",
        build_cmd=["cargo", "build"],
        clean_cmd=["cargo", "clean"],
        build_dir=ROOT / "backend" / "target",
        env={"CARGO_TERM_COLOR": "always"},
    ),
    Module(
        name="frontend",
        language="TypeScript",
        dir=ROOT / "frontend",
        build_cmd=["npm", "run", "build"],
        clean_cmd=["rm", "-rf", "node_modules", "dist"],
        build_dir=ROOT / "frontend" / "dist",
        env={"NODE_ENV": "production"},
    ),
    Module(
        name="market",
        language="Go",
        dir=ROOT / "market",
        build_cmd=["go", "build", "-o", "market", "."],
        clean_cmd=["rm", "-f", "market"],
        build_dir=ROOT / "market" / "market",
    ),
    Module(
        name="frailbox",
        language="C",
        dir=ROOT / "frailbox",
        build_cmd=["make"],
        clean_cmd=["make", "distclean"],
        build_dir=ROOT / "frailbox" / "frailbox",
    ),
    Module(
        name="engine",
        language="C++",
        dir=ROOT / "frailbox" / "engine",
        build_cmd=["cmake", "--build", "build"],
        clean_cmd=["rm", "-rf", "build"],
        build_dir=ROOT / "frailbox" / "engine" / "build" / "trial-engine",
    ),
    Module(
        name="compliance",
        language="Java",
        dir=ROOT / "compliance",
        build_cmd=["javac", "-d", "build", "ComplianceAuditor.java"],
        clean_cmd=["rm", "-rf", "build"],
        build_dir=ROOT / "compliance" / "build",
    ),
    Module(
        name="v2-market-stream",
        language="Ruby",
        dir=ROOT / "v2" / "services",
        build_cmd=["ruby", "-c", "market_stream.rb"],
        clean_cmd=["echo", "Ruby has no build artifacts to clean"],
        build_dir=None,
    ),
    Module(
        name="nfc-scanner",
        language="Lua",
        dir=ROOT / "frailbox" / "nfc",
        build_cmd=["luac", "-p", "scanner.lua"],
        clean_cmd=["echo", "Lua has no build artifacts to clean"],
        build_dir=None,
    ),
    Module(
        name="openapi-haskell",
        language="Haskell",
        dir=ROOT / "docs" / "openapi",
        build_cmd=["ghc", "-fno-code", "Types.hs", "Server.hs", "Validate.hs", "Generate.hs"],
        clean_cmd=["rm", "-f", "*.hi", "*.o", "*.hie"],
        build_dir=None,
    ),
    Module(
        name="openapi-tools",
        language="Lua",
        dir=ROOT / "tools",
        build_cmd=["luac", "-p", "openapi_diff.lua", "openapi_mock.lua", "openapi_pact.lua"],
        clean_cmd=["echo", "Nothing to clean"],
        build_dir=None,
    ),
]

class Colors:
    GREEN = "\033[92m"
    YELLOW = "\033[93m"
    RED = "\033[91m"
    CYAN = "\033[96m"
    BOLD = "\033[1m"
    RESET = "\033[0m"
    GRAY = "\033[90m"

def color(text: str, code: str) -> str:
    if not sys.stdout.isatty():
        return text
    return f"{code}{text}{Colors.RESET}"

def check_prerequisites() -> list[str]:
    required = {
        "cargo": "Rust",
        "npm": "Node.js",
        "go": "Go",
        "gcc": "C (GCC)",
        "g++": "C++ (GCC)",
        "cmake": "CMake",
        "make": "Make",
        "python3": "Python",
        "javac": "Java (JDK)",
        "ruby": "Ruby",
        "luac": "Lua",
        "ghc": "GHC (Haskell)",
    }

    missing = []
    for cmd, label in required.items():
        if shutil.which(cmd) is None:
            missing.append(f"{label} ({cmd})")

    return missing

def build_module(
    module: Module,
    release: bool = False,
    verbose: bool = False,
) -> tuple[bool, float, str]:

    print(f"\n  {color('▸', Colors.CYAN)} Building {color(module.name, Colors.BOLD)} ({module.language})...")

    env = os.environ.copy()
    if module.env:
        env.update(module.env)

    start = time.time()

    if module.name == "frontend":
        node_modules = module.dir / "node_modules"
        if not node_modules.exists():
            print(f"       {color('npm install...', Colors.GRAY)}")
            try:
                install_result = subprocess.run(
                    ["npm", "install"],
                    cwd=str(module.dir),
                    capture_output=not verbose,
                    text=True,
                    timeout=120,
                    env={k: v for k, v in env.items() if k != "NODE_ENV"},
                )
                if install_result.returncode != 0:
                    return False, time.time() - start, f"npm install failed:\n{install_result.stderr}"
            except subprocess.TimeoutExpired:
                return False, time.time() - start, "npm install TIMEOUT (120s)"

    if module.name == "engine":

        build_type = "Release" if release else "Debug"
        try:
            cfg_result = subprocess.run(
                ["cmake", "-S", ".", "-B", "build",
                 f"-DCMAKE_BUILD_TYPE={build_type}"],
                cwd=str(module.dir),
                capture_output=True,
                text=True,
                timeout=120,
                env=env,
            )
        except subprocess.TimeoutExpired:
            return False, time.time() - start, "CMake configure TIMEOUT (120s)"
        except FileNotFoundError as e:
            return False, 0, f"Command not found: {e}"
        if cfg_result.returncode != 0:
            output_lines = []
            if cfg_result.stdout:
                output_lines.append(cfg_result.stdout.strip())
            if cfg_result.stderr:
                output_lines.append(cfg_result.stderr.strip())
            output = "\n".join(output_lines)
            return False, time.time() - start, (
                f"CMake configure failed:\n{output}")
        if verbose:
            print(f"       {color('cmake configured', Colors.GRAY)}")
        cmd = ["cmake", "--build", "build"]
        if release:
            cmd.append("--config")
            cmd.append("Release")
    else:
        cmd = list(module.build_cmd)
        if release and module.name == "backend":
            cmd.append("--release")

    try:
        result = subprocess.run(
            cmd,
            cwd=str(module.dir),
            capture_output=True,
            text=True,
            env=env,
            timeout=300,
        )
    except subprocess.TimeoutExpired:
        return False, time.time() - start, "BUILD TIMEOUT (300s)"
    except FileNotFoundError as e:
        return False, 0, f"Command not found: {e}"

    elapsed = time.time() - start
    output_lines = []

    if result.stdout:
        output_lines.append(result.stdout.strip())
    if result.stderr:
        output_lines.append(result.stderr.strip())

    output = "\n".join(output_lines)
    success = result.returncode == 0

    return success, elapsed, output

def clean_module(module: Module, verbose: bool = False) -> bool:
    print(f"  {color('▸', Colors.YELLOW)} Cleaning {module.name}...")
    try:
        subprocess.run(
            module.clean_cmd,
            cwd=str(module.dir),
            capture_output=not verbose,
            text=True,
            timeout=60,
            env=os.environ.copy(),
        )
        return True
    except Exception as e:
        print(f"    {color('✗', Colors.RED)} Clean failed: {e}")
        return False

def verify_binary(module: Module) -> Optional[str]:
    if module.build_dir is None:
        return None
    path = module.build_dir
    if module.name == "backend":

        target = path / "debug" / module.name
        if not target.exists():
            target = path / "release" / module.name
        if target.exists():
            return str(target)
    if path.exists():
        return str(path)
    return None

def run_cmd(cmd: list[str], **kwargs) -> tuple[bool, str]:
    try:
        result = subprocess.run(
            cmd, capture_output=True, text=True, check=False, **kwargs
        )
        output = result.stdout
        if result.stderr:
            output += "\n" + result.stderr
        return result.returncode == 0, output.strip()
    except Exception as e:
        return False, str(e)


def collect_system_info() -> str:
    """Describe the build runtime without collecting HOME, mounts or credentials."""
    return "\n".join([
        "Zeroeye build runtime", f"python: {platform.python_version()}",
        f"system: {platform.system()}", f"release: {platform.release()}",
        f"machine: {platform.machine()}", f"cpu_count: {os.cpu_count()}", "",
    ])


def source_state() -> dict:
    """Capture the exact checkout identity; unknown state is never called clean."""
    state = {"commit": None, "tree": None, "dirty": None}
    for field, arguments in (
        ("commit", ["rev-parse", "--verify", "HEAD"]),
        ("tree", ["rev-parse", "HEAD^{tree}"]),
        ("dirty", ["status", "--porcelain", "--untracked-files=normal"]),
    ):
        try:
            result = subprocess.run(["git", *arguments], cwd=str(ROOT),
                                    capture_output=True, text=True, timeout=5)
            if result.returncode == 0:
                value = result.stdout.strip()
                if field == "dirty":
                    state[field] = bool(value)
                elif re.fullmatch(r"[0-9a-f]{40}", value):
                    state[field] = value
        except (OSError, subprocess.TimeoutExpired):
            pass
    return state


def _metadata_redact(text: str) -> str:
    """Remove known local identifiers from public build output, before JSON encoding."""
    paths = [(str(ROOT), "<REPO>"), (str(Path.home()), "<HOME>"),
             (tempfile.gettempdir(), "<TEMP>")]
    replacements = {}
    for value, label in paths:
        spellings = {value}
        if not PureWindowsPath(value).drive:
            try:
                spellings.add(str(Path(value).resolve()))
            except (OSError, RuntimeError):
                pass
        for spelling in spellings:
            if spelling and spelling not in ("/", "\\"):
                for variant in (spelling, spelling.replace("\\", "/"), spelling.replace("/", "\\")):
                    replacements[variant.rstrip("/\\")] = label
    for value in sorted(replacements, key=len, reverse=True):
        flags = re.IGNORECASE if PureWindowsPath(value).drive else 0
        text = re.sub(re.escape(value) + r"(?![\w.-])", lambda _: replacements[value], text, flags=flags)
    identity_flags = re.IGNORECASE if (PureWindowsPath(str(ROOT)).drive or platform.system() == "Windows") else 0
    for value, label in ((getpass.getuser(), "<USER>"), (platform.node(), "<HOST>")):
        if value:
            text = re.sub(r"(?<![\w.-])" + re.escape(value) + r"(?![\w.-])", lambda _: label, text, flags=identity_flags)
    # Known labels only; review plaintext before publishing for arbitrary secrets.
    text = re.sub(r"(?i)(\b(?:api[_-]?key|access[_-]?token|secret|password)\s*[:=]\s*)[^\s,;]+",
                  r"\1<REDACTED>", text)
    text = re.sub(r"(?i)(\bauthorization\s*:\s*bearer\s+)[^\s,;]+", r"\1<REDACTED>", text)
    return text


def _metadata_artifact(binary: Optional[str]) -> Optional[str]:
    """Report only paths inside the repository; omit outside artifacts."""
    if not binary:
        return None
    windows = bool(PureWindowsPath(binary).drive or "\\" in binary)
    path_class = PureWindowsPath if windows else PurePosixPath
    path = path_class(binary)
    if ".." in path.parts or (windows and path.anchor and not path.is_absolute()):
        return None
    if path.is_absolute():
        try:
            path = path.relative_to(path_class(str(ROOT)))
        except ValueError:
            return None
    if not path.parts or path.is_absolute() or path.drive:
        return None
    # Pure path fixtures allow Windows spelling tests on Unix. For native paths,
    # check actual containment too: a repository symlink can point outside it.
    if isinstance(ROOT, Path):
        try:
            (ROOT / path.as_posix()).resolve().relative_to(ROOT.resolve())
        except (OSError, RuntimeError, ValueError):
            return None
    return path.as_posix()


def _metadata_output(output: str) -> dict:
    """Bound public previews; the encrypted build.log retains the complete text."""
    redacted = _metadata_redact(output)
    encoded = redacted.encode("utf-8")
    limit = 16 * 1024
    return {
        "output": encoded[:limit].decode("utf-8", errors="ignore"),
        "output_byte_count": len(encoded),
        "output_truncated": len(encoded) > limit,
    }


def build_diagnostic_report(
    results: list[tuple[str, bool, float, str, Optional[str]]],
    commit_id: str,
    logd_relpaths: Optional[list[str]] = None,
    password: Optional[str] = None,
    logd_error: Optional[str] = None,
    chunked: bool = False,
    provenance: Optional[dict] = None,
) -> dict:
    # Keep references and every derived public instruction portable.
    if logd_relpaths:
        logd_relpaths = [value.replace("\\", "/") for value in logd_relpaths]
    diagnostic_logd: Optional[str | list[str]]
    if not logd_relpaths:
        diagnostic_logd = None
    elif len(logd_relpaths) == 1:
        diagnostic_logd = logd_relpaths[0]
    else:
        diagnostic_logd = logd_relpaths

    decrypt_target = logd_relpaths[0] if logd_relpaths and len(logd_relpaths) == 1 else None
    if logd_relpaths and len(logd_relpaths) > 1:
        decrypt_target = (DIAGNOSTIC_DIR / f"build-{commit_id}.logd").relative_to(ROOT).as_posix()

    report = {
        "generated_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
        "commit": commit_id,
        "format": "safe-logd-v1",
        "provenance": provenance,
        "source_commit": provenance.get("before", {}).get("commit") if provenance else None,
        "source_tree": provenance.get("before", {}).get("tree") if provenance else None,
        "diagnostic_logd": diagnostic_logd,
        "diagnostic_logd_error": _metadata_redact(logd_error) if logd_error else None,
        "chunked": chunked,
        "chunk_size_bytes": DIAGNOSTIC_CHUNK_SIZE if chunked else None,
        "password": password,
        "decrypt_command": (
            f"python3 tools/safe_logd.py unpack {decrypt_target} <outdir> --metadata diagnostic/build-{commit_id}.json"
            if decrypt_target and password else None
        ),
        "total_modules": len(results),
        "passed": sum(1 for _, s, _, _, _ in results if s),
        "failed": sum(1 for _, s, _, _, _ in results if not s),
        "modules": [
            {
                "name": name,
                "status": "PASS" if success else "FAIL",
                "elapsed_seconds": round(elapsed, 3),
                "artifact": _metadata_artifact(binary),
                **_metadata_output(output),
                "full_output": "build.log" if logd_relpaths else None,
            }
            for name, success, elapsed, output, binary in results
        ],
        "pr_note": (
            (f"Include the encrypted diagnostic logd artifact(s): {', '.join(logd_relpaths)}. " if logd_relpaths else "Encrypted diagnostic logd artifact was not created; include this JSON report showing why. ")
            + "This safe-logd-v1 archive is incompatible with Encryptly; use tools/safe_logd.py to decrypt it. This JSON is public metadata including the decryption key, not a signature or confidentiality guarantee. "
            + "Maintainers may ask you to remove these diagnostic artifacts before merging."
        ),
    }
    return report


def write_diagnostic_report(metadata_path: Path, report: dict) -> None:
    metadata_path.write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
    print(f"    {color('✓', Colors.GREEN)} {metadata_path.relative_to(ROOT)} created")


def generate_logd(
    results: list[tuple[str, bool, float, str, Optional[str]]],
    verbose: bool = False,
    provenance: Optional[dict] = None,
) -> bool:
    """Encrypt exactly the three redacted texts from this build, without discovery."""
    if provenance is None:
        current = source_state()
        provenance = {"before": current, "after": current}
    before, after = provenance.get("before", {}), provenance.get("after", {})
    source_commit = before.get("commit")
    source_tree = before.get("tree")
    if (not isinstance(source_commit, str) or not re.fullmatch(r"[0-9a-f]{40}", source_commit)
            or not isinstance(source_tree, str) or not re.fullmatch(r"[0-9a-f]{40}", source_tree)
            or after.get("commit") != source_commit or after.get("tree") != source_tree
            or before.get("dirty") is not False or after.get("dirty") is not False):
        print("    Diagnostic generation refused: source commit/tree must be known, unchanged and clean before/after building.")
        return False
    commit_id = source_commit[:8]
    logd_path = DIAGNOSTIC_DIR / f"build-{commit_id}.logd"
    metadata_path = DIAGNOSTIC_DIR / f"build-{commit_id}.json"
    # Never traverse a diagnostic directory symlink or overwrite an earlier run.
    if (DIAGNOSTIC_DIR.is_symlink()
            or not DIAGNOSTIC_DIR.resolve().is_relative_to(ROOT.resolve())):
        print("    Diagnostic generation refused: diagnostic/ must stay inside the repository.")
        return False
    DIAGNOSTIC_DIR.mkdir(parents=True, exist_ok=True)
    if any(DIAGNOSTIC_DIR.glob(f"build-{commit_id}*")):
        print("    Diagnostic generation refused: preserve the existing bundle before retrying this commit.")
        return False
    summary = [
        "Zeroeye - actual build results",
        f"source_commit: {source_commit}", f"source_tree: {before['tree']}",
        f"dirty_before: {before.get('dirty')}", f"dirty_after: {after.get('dirty')}",
        f"generated_at: {datetime.datetime.now(datetime.timezone.utc).isoformat()}",
        f"total_modules: {len(results)}",
        f"passed: {sum(1 for _, success, _, _, _ in results if success)}",
        f"failed: {sum(1 for _, success, _, _, _ in results if not success)}", "",
    ]
    logs = []
    for name, success, elapsed, output, binary in results:
        status = "PASS" if success else "FAIL"
        summary.append(f"{name}: {status} ({elapsed:.3f}s)")
        logs.append(f"{name}: {status} ({elapsed:.3f}s)\n{output}")
        artifact = _metadata_artifact(binary)
        if artifact:
            summary.append(f"artifact: {artifact}")
    texts = {
        "system-info.txt": _metadata_redact(collect_system_info()),
        "build-summary.txt": _metadata_redact("\n".join(summary) + "\n"),
        "build.log": _metadata_redact("\n\n".join(logs) + "\n"),
    }
    report = build_diagnostic_report(results, commit_id, provenance=provenance)
    try:
        from tools.safe_logd import pack_diagnostic_texts, unpack_diagnostic_texts
        password = pack_diagnostic_texts(logd_path, texts)
        if unpack_diagnostic_texts(logd_path, password) != texts:
            raise ValueError("generated diagnostic round-trip verification failed")
        logd_files = split_diagnostic_logd(logd_path)
    except (ImportError, OSError, ValueError) as error:
        report["diagnostic_logd_error"] = _metadata_redact(f"safe-logd-v1 packaging failed: {error}")
        write_diagnostic_report(metadata_path, report)
        print("    Safe diagnostic packaging failed; see diagnostic JSON for the reason.")
        return False
    report = build_diagnostic_report(
        results, commit_id, [path.relative_to(ROOT).as_posix() for path in logd_files],
        password=password, provenance=provenance, chunked=len(logd_files) > 1,
    )
    report["files"] = {name: {"bytes": len(value.encode("utf-8")),
                              "sha256": hashlib.sha256(value.encode("utf-8")).hexdigest()}
                       for name, value in texts.items()}
    report["artifact_sha256"] = {path.relative_to(ROOT).as_posix(): hashlib.sha256(path.read_bytes()).hexdigest()
                                 for path in logd_files}
    write_diagnostic_report(metadata_path, report)
    print(f"    Safe diagnostic created: {logd_path.relative_to(ROOT)}")
    print("    This format is incompatible with Encryptly; use the JSON decrypt_command.")
    return True


def print_summary(results: list[tuple[str, bool, float, str, Optional[str]]]):
    print(f"  {color('Build Summary', Colors.BOLD)}")

    total = len(results)
    passed = sum(1 for _, s, _, _, _ in results if s)
    failed = total - passed
    total_time = sum(t for _, _, t, _, _ in results)

    for name, success, elapsed, output, binary in results:
        status_icon = color("✓", Colors.GREEN) if success else color("✗", Colors.RED)
        status_text = color("PASS", Colors.GREEN) if success else color("FAIL", Colors.RED)
        time_str = f"{elapsed:.1f}s" if elapsed < 60 else f"{elapsed / 60:.1f}m"

        print(f"\n  {status_icon}  {color(name + ':', Colors.BOLD)} {status_text}  ({time_str})")
        if binary:
            print(f"       artifact: {color(binary, Colors.GRAY)}")
        if not success and output:

            lines = output.strip().split("\n")
            print(f"       {color('last output:', Colors.RED)}")
            for line in lines[-5:]:
                print(f"       {color(line, Colors.GRAY)}")

    print(f"\n  {color('─' * 40, Colors.GRAY)}")
    print(f"  {color('Total:', Colors.BOLD)} {total} modules, "
          f"{color(str(passed) + ' passed', Colors.GREEN)}, "
          f"{color(str(failed) + ' failed', Colors.RED)}, "
          f"{total_time:.1f}s total")

def main():
    parser = argparse.ArgumentParser(
        description="Tent of Trials  -  Multi-Language Build System",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Examples:
  python3 build.py                    Build all modules
  python3 build.py -m backend         Build only backend
  python3 build.py -m frontend,market Build frontend and market
  python3 build.py --clean            Clean all artifacts
  python3 build.py --release          Release build (Rust only)
  python3 build.py --verbose          Verbose output

Diagnostic bundle:
  python3 build.py
        """,
    )
    parser.add_argument(
        "-m", "--module",
        help="Module(s) to build (comma-separated, or 'all')",
        default="all",
    )
    parser.add_argument(
        "--clean", action="store_true",
        help="Clean build artifacts instead of building",
    )
    parser.add_argument(
        "--release", action="store_true",
        help="Build in release mode (Rust backend)",
    )
    parser.add_argument(
        "--verbose", "-v", action="store_true",
        help="Show detailed build output",
    )
    parser.add_argument(
        "--list", action="store_true",
        help="List available modules and exit",
    )

    args = parser.parse_args()

    print(f"\n  {color('Tent of Trials: building', Colors.CYAN)}")
    print(f"  Working directory: {ROOT}")
    print()

    if args.list:
        print(f"  {color('Available modules:', Colors.BOLD)}")
        for m in MODULES:
            print(f"    {color(m.name, Colors.CYAN)} ({m.language})")
            print(f"      dir: {m.dir.relative_to(ROOT)}")
            print(f"      build: {' '.join(m.build_cmd)}")
        return 0

    print(f"  {color('Checking prerequisites...', Colors.GRAY)}")
    missing = check_prerequisites()
    if missing:
        print(f"\n  {color('⚠ Some tools missing  -  will try anyway:', Colors.YELLOW)}")
        for m in missing:
            print(f"    {m}")
        print(f"  {color('Not all modules will build. That is fine.', Colors.GRAY)}")
    else:
        print(f"  {color('✓ All prerequisites found', Colors.GREEN)}")

    if args.module == "all":
        selected = MODULES
    else:
        names = [n.strip() for n in args.module.split(",")]
        selected = [m for m in MODULES if m.name in names]
        not_found = set(names) - {m.name for m in MODULES}
        if not_found:
            print(f"  {color('✗ Unknown modules:', Colors.RED)} {', '.join(not_found)}")
            print(f"    Available: {', '.join(m.name for m in MODULES)}")
            return 1

    if not selected:
        print(f"  No modules selected.")
        return 0

    if args.clean:
        if (DIAGNOSTIC_DIR.is_symlink()
                or not DIAGNOSTIC_DIR.resolve().is_relative_to(ROOT.resolve())):
            print("    Cleaning refused: diagnostic/ must stay inside the repository.")
            return 1
        diagnostic_artifacts = [ROOT / "build.logd"]
        if DIAGNOSTIC_DIR.exists():
            diagnostic_artifacts.extend(DIAGNOSTIC_DIR.glob("build-[0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f].logd"))
            diagnostic_artifacts.extend(DIAGNOSTIC_DIR.glob("build-[0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f]-part*.logd"))
            diagnostic_artifacts.extend(DIAGNOSTIC_DIR.glob("build-[0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f].json"))
            diagnostic_artifacts.extend(DIAGNOSTIC_DIR.glob("build-[0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f]-metadata.json"))
        if any(artifact.is_symlink() or (artifact.exists() and not artifact.is_file())
               for artifact in diagnostic_artifacts):
            print("    Cleaning refused: diagnostic targets must be regular files, not symlinks or directories.")
            return 1
        print(f"\n  {color('Cleaning build artifacts...', Colors.YELLOW)}")
        for module in selected:
            clean_module(module, args.verbose)
        for artifact in diagnostic_artifacts:
            if artifact.exists():
                artifact.unlink()
                print(f"  {color('▸', Colors.YELLOW)} Removed {artifact.relative_to(ROOT)}")
        print(f"\n  {color('Clean complete.', Colors.GREEN)}")
        return 0

    print(f"\n  {color(f'Building {len(selected)} module(s) | release={args.release}', Colors.GRAY)}")

    provenance = {"before": source_state()}
    results: list[tuple[str, bool, float, str, Optional[str]]] = []

    for module in selected:
        success, elapsed, output = build_module(module, args.release, args.verbose)
        binary = verify_binary(module) if success else None
        results.append((module.name, success, elapsed, output, binary))

    print_summary(results)

    provenance["after"] = source_state()
    packaged = generate_logd(results, args.verbose, provenance=provenance)

    return 0 if packaged and all(r[1] for r in results) else 1

if __name__ == "__main__":
    sys.exit(main())
