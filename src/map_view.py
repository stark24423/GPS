from __future__ import annotations

import json
from pathlib import Path

from PySide6.QtCore import QObject, QUrl, Signal, Slot
from PySide6.QtWebChannel import QWebChannel
from PySide6.QtWebEngineCore import QWebEngineSettings
from PySide6.QtWebEngineWidgets import QWebEngineView

from src.models import Coordinate


class MapBridge(QObject):
    point_added = Signal(float, float)
    points_cleared = Signal()

    @Slot(float, float)
    def addPoint(self, lat: float, lon: float) -> None:
        self.point_added.emit(lat, lon)

    @Slot()
    def clearPoints(self) -> None:
        self.points_cleared.emit()


class MapView(QWebEngineView):
    point_added = Signal(Coordinate)
    points_cleared = Signal()

    def __init__(self, parent=None) -> None:
        super().__init__(parent)
        self.settings().setAttribute(QWebEngineSettings.WebAttribute.LocalContentCanAccessFileUrls, True)
        self.settings().setAttribute(QWebEngineSettings.WebAttribute.LocalContentCanAccessRemoteUrls, True)

        self._bridge = MapBridge()
        self._channel = QWebChannel(self.page())
        self._channel.registerObject("pythonBridge", self._bridge)
        self.page().setWebChannel(self._channel)

        self._bridge.point_added.connect(self._on_point_added)
        self._bridge.points_cleared.connect(self.points_cleared.emit)

        html_path = Path(__file__).resolve().parent.parent / "assets" / "map.html"
        self.load(QUrl.fromLocalFile(str(html_path)))

    def set_points(self, points: list[Coordinate]) -> None:
        payload = json.dumps([{"lat": point.lat, "lon": point.lon} for point in points])
        self.page().runJavaScript(f"window.setRoutePoints({payload});")

    def clear_points(self) -> None:
        self.page().runJavaScript("window.clearRoutePoints();")

    def set_current_position(self, point: Coordinate, status: str = "Current") -> None:
        payload = json.dumps({"lat": point.lat, "lon": point.lon})
        status_payload = json.dumps(status)
        self.page().runJavaScript(f"window.setCurrentPosition({payload}, {status_payload});")

    def start_current_animation(self, points: list[Coordinate], step_ms: int = 1000) -> None:
        payload = json.dumps([{"lat": point.lat, "lon": point.lon} for point in points])
        self.page().runJavaScript(f"window.startCurrentAnimation({payload}, {step_ms});")

    def stop_current_animation(self, status: str = "Stopped") -> None:
        status_payload = json.dumps(status)
        self.page().runJavaScript(f"window.stopCurrentAnimation({status_payload});")

    def set_editing_locked(self, locked: bool) -> None:
        self.page().runJavaScript(f"window.setEditingLocked({str(locked).lower()});")

    def set_follow_mode(self, enabled: bool) -> None:
        self.page().runJavaScript(f"window.setFollowMode({str(enabled).lower()});")

    def clear_current_position(self) -> None:
        self.page().runJavaScript("window.clearCurrentPosition();")

    def _on_point_added(self, lat: float, lon: float) -> None:
        self.point_added.emit(Coordinate(lat=lat, lon=lon))
