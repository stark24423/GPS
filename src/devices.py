from __future__ import annotations

import shutil
import subprocess

from src.models import DeviceInfo, RequirementStatus


def check_windows_iphone_requirements() -> list[RequirementStatus]:
    return [
        RequirementStatus(
            name="iPhone bridge",
            ok=False,
            message="No real iPhone bridge is configured. Dry-run mode will generate GPX only.",
        ),
        RequirementStatus(
            name="Apple Mobile Device tooling",
            ok=shutil.which("idevice_id") is not None,
            message="Optional libimobiledevice tool 'idevice_id' was found."
            if shutil.which("idevice_id")
            else "Optional libimobiledevice tool 'idevice_id' was not found.",
        ),
    ]


def list_detected_devices() -> list[DeviceInfo]:
    idevice_id = shutil.which("idevice_id")
    if not idevice_id:
        return []

    try:
        result = subprocess.run(
            [idevice_id, "-l"],
            capture_output=True,
            check=False,
            text=True,
            timeout=5,
        )
    except OSError:
        return []

    devices: list[DeviceInfo] = []
    for line in result.stdout.splitlines():
        device_id = line.strip()
        if device_id:
            devices.append(DeviceInfo(id=device_id, name="iPhone", kind="ios", connected=True))
    return devices
