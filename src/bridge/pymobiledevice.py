from __future__ import annotations

import asyncio
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
        try:
            return asyncio.run(self._list_devices_async())
        except Exception:
            return []

    @staticmethod
    async def _list_devices_async() -> list[DeviceInfo]:
        from pymobiledevice3.lockdown import create_using_usbmux
        from pymobiledevice3.usbmux import list_devices as usbmux_list_devices

        devices: list[DeviceInfo] = []
        for mux in await usbmux_list_devices():
            name = "iPhone"
            kind = "ios"
            try:
                lockdown = await create_using_usbmux(serial=mux.serial)
                name = lockdown.all_values.get("DeviceName") or name
                device_class = lockdown.all_values.get("DeviceClass", "")
                if device_class and device_class.lower() != "iphone":
                    continue
            except Exception:
                pass
            devices.append(
                DeviceInfo(
                    id=mux.serial,
                    name=name,
                    kind=kind,
                    connected=str(getattr(mux, "connection_type", "")).upper() == "USB",
                )
            )
        return devices

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
                self._active_process.wait(timeout=1)
            except (OSError, subprocess.TimeoutExpired):
                self._active_process.terminate()
            finally:
                self._active_process = None

        result = self._run(["developer", "dvt", "simulate-location", "clear"], timeout=8)
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
        try:
            return subprocess.run(
                [self.executable, *args],
                capture_output=True,
                check=False,
                text=True,
                timeout=timeout,
            )
        except subprocess.TimeoutExpired as exc:
            return subprocess.CompletedProcess(
                [self.executable, *args],
                124,
                exc.stdout or "",
                exc.stderr or f"Command timed out after {timeout} seconds.",
            )
        except (FileNotFoundError, OSError) as exc:
            return subprocess.CompletedProcess(
                [self.executable, *args],
                127,
                "",
                f"pymobiledevice3 executable not available: {exc}",
            )

    def _start_interactive(self, args: list[str], success_message: str) -> BridgeResult:
        if self._active_process and self._active_process.poll() is None:
            self.stop_location()

        try:
            process = subprocess.Popen(
                [self.executable, *args],
                stdin=subprocess.PIPE,
                stdout=subprocess.DEVNULL,
                stderr=subprocess.DEVNULL,
                text=True,
            )
        except OSError as exc:
            return BridgeResult(ok=False, message="Failed to start pymobiledevice3.", detail=str(exc))

        try:
            process.wait(timeout=8)
        except subprocess.TimeoutExpired:
            self._active_process = process
            return BridgeResult(ok=True, message=success_message, detail="Process is running.")

        output = f"Command exited with return code {process.returncode}."
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
