from __future__ import annotations

from dataclasses import dataclass


@dataclass(frozen=True)
class Coordinate:
    lat: float
    lon: float
    elevation: float = 0.0


@dataclass(frozen=True)
class DeviceInfo:
    id: str
    name: str
    kind: str
    connected: bool


@dataclass(frozen=True)
class BridgeResult:
    ok: bool
    message: str
    detail: str = ""


@dataclass(frozen=True)
class RequirementStatus:
    name: str
    ok: bool
    message: str
