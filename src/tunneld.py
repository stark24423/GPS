from __future__ import annotations

import ctypes
import subprocess
from pathlib import Path

from src.models import BridgeResult


def _run_elevated_script(project_root: Path, script_name: str, success_message: str) -> BridgeResult:
    script_path = project_root / "scripts" / script_name
    if not script_path.exists():
        return BridgeResult(ok=False, message="tunneld script was not found.", detail=str(script_path))

    params = f'-WindowStyle Hidden -ExecutionPolicy Bypass -File "{script_path}"'
    result = ctypes.windll.shell32.ShellExecuteW(None, "runas", "powershell.exe", params, str(project_root), 0)
    if result <= 32:
        return BridgeResult(
            ok=False,
            message="Failed to request Administrator tunneld action.",
            detail=f"ShellExecuteW returned {result}",
        )

    return BridgeResult(ok=True, message=success_message, detail=str(script_path))


def start_tunneld_admin(project_root: Path) -> BridgeResult:
    return _run_elevated_script(
        project_root,
        "launch_tunneld_background.ps1",
        "Requested background tunneld. Approve UAC; no PowerShell window needs to stay open.",
    )


def stop_tunneld_admin(project_root: Path) -> BridgeResult:
    return _run_elevated_script(
        project_root,
        "stop_tunneld_background.ps1",
        "Requested background tunneld stop. Approve UAC if prompted.",
    )


def get_tunneld_pid(project_root: Path) -> int | None:
    pid_file = project_root / "output" / "tunneld.pid"
    if not pid_file.exists():
        return None

    try:
        pid = int(pid_file.read_text(encoding="ascii").strip())
    except ValueError:
        return None

    result = subprocess.run(
        [
            "powershell.exe",
            "-NoProfile",
            "-Command",
            f"if (Get-Process -Id {pid} -ErrorAction SilentlyContinue) {{ 'running' }}",
        ],
        capture_output=True,
        text=True,
        timeout=3,
        check=False,
    )
    if "running" not in result.stdout:
        return None

    return pid
