from __future__ import annotations

from pathlib import Path

from src.bridge.base import Bridge
from src.devices import check_windows_iphone_requirements, list_detected_devices
from src.models import BridgeResult, DeviceInfo, RequirementStatus


class DryRunBridge(Bridge):
    def list_devices(self) -> list[DeviceInfo]:
        return list_detected_devices()

    def start_location(self, gpx_path: str) -> BridgeResult:
        path = Path(gpx_path)
        if not path.exists():
            return BridgeResult(ok=False, message="GPX file does not exist.", detail=str(path))

        return BridgeResult(
            ok=True,
            message="Dry-run complete. GPX was generated, but no iPhone location was changed.",
            detail=str(path),
        )

    def stop_location(self, device_id: str | None = None) -> BridgeResult:
        target = device_id or "default device"
        return BridgeResult(
            ok=True,
            message=f"Dry-run stop complete for {target}. No iPhone location was changed.",
        )

    def check_requirements(self) -> list[RequirementStatus]:
        return check_windows_iphone_requirements()
