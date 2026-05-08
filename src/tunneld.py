from __future__ import annotations

import ctypes
from pathlib import Path

from src.models import BridgeResult


def start_tunneld_admin(project_root: Path) -> BridgeResult:
    script_path = project_root / "scripts" / "launch_tunneld_elevated.ps1"
    if not script_path.exists():
        return BridgeResult(ok=False, message="tunneld startup script was not found.", detail=str(script_path))

    params = f'-NoExit -ExecutionPolicy Bypass -File "{script_path}"'
    result = ctypes.windll.shell32.ShellExecuteW(None, "runas", "powershell.exe", params, str(project_root), 1)
    if result <= 32:
        return BridgeResult(
            ok=False,
            message="Failed to request Administrator tunneld window.",
            detail=f"ShellExecuteW returned {result}",
        )

    return BridgeResult(
        ok=True,
        message="Requested Administrator tunneld window. Approve UAC and keep that window open.",
        detail=str(script_path),
    )
