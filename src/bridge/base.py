from __future__ import annotations

from abc import ABC, abstractmethod

from src.models import BridgeResult, DeviceInfo, RequirementStatus


class Bridge(ABC):
    @abstractmethod
    def list_devices(self) -> list[DeviceInfo]:
        raise NotImplementedError

    @abstractmethod
    def start_location(self, gpx_path: str) -> BridgeResult:
        raise NotImplementedError

    @abstractmethod
    def stop_location(self, device_id: str | None = None) -> BridgeResult:
        raise NotImplementedError

    @abstractmethod
    def check_requirements(self) -> list[RequirementStatus]:
        raise NotImplementedError
