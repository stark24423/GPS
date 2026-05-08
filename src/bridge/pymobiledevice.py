from __future__ import annotations

import json
import subprocess
from pathlib import Path

from src.bridge.base import Bridge
from src.models import BridgeResult, DeviceInfo, RequirementStatus


class PymobileDeviceBridge(Bridge):
    def __init__(self, executable: str = "pymobiledevice3") -> None:
        self.executable = executable
        self._active_process: subprocess.Popen[str] | None = None

    def list_devices(self) -> list[DeviceInfo]:
        result = self._run(["usbmux", "list"], timeout=15)
        if result.returncode != 0:
            return []

        try:
            payload = json.loads(result.stdout)
        except json.JSONDecodeError:
            return []

        return [
            DeviceInfo(
                id=item.get("UniqueDeviceID") or item.get("Identifier") or "unknown",
                name=item.get("DeviceName") or "iPhone",
                kind="ios",
                connected=item.get("ConnectionType") == "USB",
            )
            for item in payload
            if item.get("DeviceClass") == "iPhone"
        ]

    def start_location(self, gpx_path: str) -> BridgeResult:
        path = Path(gpx_path)
        if not path.exists():
            return BridgeResult(ok=False, message="GPX file does not exist.", detail=str(path))

        return self._start_interactive(
            ["developer", "dvt", "simulate-location", "play", str(path)],
            success_message="Started iPhone GPX location playback.",
        )

    def set_location(self, latitude: float, longitude: float) -> BridgeResult:
        return self._start_interactive(
            ["developer", "dvt", "simulate-location", "set", "--", str(latitude), str(longitude)],
            success_message="Set iPhone simulated location.",
        )

    def stop_location(self, device_id: str | None = None) -> BridgeResult:
        if self._active_process and self._active_process.poll() is None:
            try:
                if self._active_process.stdin:
                    self._active_process.stdin.write("\n")
                    self._active_process.stdin.flush()
                self._active_process.wait(timeout=5)
            except (OSError, subprocess.TimeoutExpired):
                self._active_process.terminate()
            finally:
                self._active_process = None

        result = self._run(["developer", "dvt", "simulate-location", "clear"], timeout=30)
        return self._to_bridge_result(result, success_message="Cleared iPhone simulated location.")

    def check_requirements(self) -> list[RequirementStatus]:
        checks: list[RequirementStatus] = []

        version = self._run(["version"], timeout=10)
        checks.append(
            RequirementStatus(
                name="pymobiledevice3",
                ok=version.returncode == 0,
                message=version.stdout.strip() or version.stderr.strip() or "Not available.",
            )
        )

        developer_mode = self._run(["amfi", "developer-mode-status"], timeout=15)
        checks.append(
            RequirementStatus(
                name="Developer Mode",
                ok="true" in developer_mode.stdout.lower(),
                message=developer_mode.stdout.strip() or developer_mode.stderr.strip() or "Unknown.",
            )
        )

        return checks

    def _run(self, args: list[str], timeout: int) -> subprocess.CompletedProcess[str]:
        return subprocess.run(
            [self.executable, *args],
            capture_output=True,
            check=False,
            text=True,
            timeout=timeout,
        )

    def _start_interactive(self, args: list[str], success_message: str) -> BridgeResult:
        if self._active_process and self._active_process.poll() is None:
            self.stop_location()

        try:
            process = subprocess.Popen(
                [self.executable, *args],
                stdin=subprocess.PIPE,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                text=True,
            )
        except OSError as exc:
            return BridgeResult(ok=False, message="Failed to start pymobiledevice3.", detail=str(exc))

        try:
            process.wait(timeout=2)
        except subprocess.TimeoutExpired:
            self._active_process = process
            return BridgeResult(ok=True, message=success_message, detail="Process is running.")

        output = ""
        if process.stdout:
            output += process.stdout.read()
        if process.stderr:
            output += process.stderr.read()
        return self._to_bridge_result(
            subprocess.CompletedProcess(args, process.returncode or 0, output, ""),
            success_message=success_message,
        )

    @staticmethod
    def _to_bridge_result(result: subprocess.CompletedProcess[str], success_message: str) -> BridgeResult:
        output = (result.stdout + "\n" + result.stderr).strip()
        if result.returncode == 0:
            return BridgeResult(ok=True, message=success_message, detail=output)

        if "Unable to connect to Tunneld" in output or "requires admin privileges" in output:
            return BridgeResult(
                ok=False,
                message="iPhone developer tunnel is not running. Start tunneld as Administrator, then retry.",
                detail=output,
            )

        return BridgeResult(ok=False, message="pymobiledevice3 command failed.", detail=output)
