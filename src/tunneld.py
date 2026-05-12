from __future__ import annotations

import ctypes
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


_PROCESS_QUERY_LIMITED_INFORMATION = 0x1000
_STILL_ACTIVE = 259


def get_tunneld_pid(project_root: Path) -> int | None:
    pid_file = project_root / "output" / "tunneld.pid"
    if not pid_file.exists():
        return None

    try:
        pid = int(pid_file.read_text(encoding="ascii").strip())
    except ValueError:
        return None

    kernel32 = ctypes.windll.kernel32
    handle = kernel32.OpenProcess(_PROCESS_QUERY_LIMITED_INFORMATION, False, pid)
    if not handle:
        return None
    try:
        exit_code = ctypes.c_ulong()
        if not kernel32.GetExitCodeProcess(handle, ctypes.byref(exit_code)):
            return None
        return pid if exit_code.value == _STILL_ACTIVE else None
    finally:
        kernel32.CloseHandle(handle)
