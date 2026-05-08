from __future__ import annotations

import subprocess
from pathlib import Path

from src.models import BridgeResult


def start_tunneld_admin(project_root: Path) -> BridgeResult:
    script_path = project_root / "scripts" / "start_tunneld_admin.ps1"
    if not script_path.exists():
        return BridgeResult(ok=False, message="tunneld startup script was not found.", detail=str(script_path))

    command = [
        "powershell.exe",
        "-NoExit",
        "-ExecutionPolicy",
        "Bypass",
        "-File",
        str(script_path),
    ]

    try:
        subprocess.Popen(command, shell=True)
    except OSError as exc:
        return BridgeResult(ok=False, message="Failed to request Administrator tunneld window.", detail=str(exc))

    return BridgeResult(
        ok=True,
        message="Requested Administrator tunneld window. Approve UAC and keep that window open.",
        detail=str(script_path),
    )
