from __future__ import annotations

import os
import sys
from datetime import datetime
from pathlib import Path

from PySide6.QtCore import QTimer, Qt
from PySide6.QtWidgets import (
    QApplication,
    QComboBox,
    QDoubleSpinBox,
    QHBoxLayout,
    QLabel,
    QMainWindow,
    QMessageBox,
    QPushButton,
    QPlainTextEdit,
    QSplitter,
    QVBoxLayout,
    QWidget,
)

from src.bridge import DryRunBridge, PymobileDeviceBridge
from src.gpx import build_gpx_points, write_gpx
from src.map_view import MapView
from src.models import Coordinate
from src.tunneld import start_tunneld_admin


class MainWindow(QMainWindow):
    def __init__(self) -> None:
        super().__init__()
        self.setWindowTitle("Python iPhone GPS Simulator Prototype")
        self.resize(1200, 760)

        self._points: list[Coordinate] = []
        self._dry_run_bridge = DryRunBridge()
        self._iphone_bridge = PymobileDeviceBridge(str(Path.cwd() / ".venv" / "Scripts" / "pymobiledevice3.exe"))
        self._bridge = self._dry_run_bridge
        self._output_dir = Path.cwd() / "output"
        self._simulation_points: list[Coordinate] = []
        self._simulation_index = 0

        self.map_view = MapView()
        self.map_view.point_added.connect(self._add_point)
        self.map_view.points_cleared.connect(self._clear_points)

        self.mode_combo = QComboBox()
        self.mode_combo.addItems(["Single point", "Route"])
        self.mode_combo.currentTextChanged.connect(self._mode_changed)

        self.bridge_combo = QComboBox()
        self.bridge_combo.addItems(["Dry-run", "iPhone"])
        self.bridge_combo.currentTextChanged.connect(self._bridge_mode_changed)

        self.speed_input = QDoubleSpinBox()
        self.speed_input.setRange(0.1, 300.0)
        self.speed_input.setValue(5.0)
        self.speed_input.setSuffix(" km/h")
        self.speed_input.setDecimals(1)

        self.jitter_input = QDoubleSpinBox()
        self.jitter_input.setRange(0.0, 50.0)
        self.jitter_input.setValue(5.0)
        self.jitter_input.setSuffix(" m")
        self.jitter_input.setDecimals(1)

        self.device_label = QLabel("Device: dry-run / no bridge configured")
        self.points_label = QLabel("Points: 0")
        self.current_label = QLabel("Current: -")
        self.status_label = QLabel("Status: idle")

        self.start_button = QPushButton("Start")
        self.stop_button = QPushButton("Stop")
        self.clear_button = QPushButton("Clear points")
        self.undo_button = QPushButton("Remove last point")
        self.output_button = QPushButton("Open output folder")
        self.refresh_button = QPushButton("Refresh devices")
        self.tunneld_button = QPushButton("Start tunneld")
        self.stop_button.setEnabled(False)

        self.start_button.clicked.connect(self._start)
        self.stop_button.clicked.connect(self._stop)
        self.clear_button.clicked.connect(self._clear_points)
        self.undo_button.clicked.connect(self._remove_last_point)
        self.output_button.clicked.connect(self._open_output_folder)
        self.refresh_button.clicked.connect(self._refresh_devices)
        self.tunneld_button.clicked.connect(self._start_tunneld)

        self.simulation_timer = QTimer(self)
        self.simulation_timer.setInterval(1000)
        self.simulation_timer.timeout.connect(self._advance_simulation_marker)

        self.log = QPlainTextEdit()
        self.log.setReadOnly(True)
        self.log.setMaximumBlockCount(1000)

        root = QWidget()
        root_layout = QHBoxLayout(root)

        splitter = QSplitter(Qt.Horizontal)
        splitter.addWidget(self.map_view)
        splitter.addWidget(self._build_side_panel())
        splitter.setStretchFactor(0, 4)
        splitter.setStretchFactor(1, 1)
        root_layout.addWidget(splitter)

        self.setCentralWidget(root)
        self._log("Application started in dry-run mode.")
        self._refresh_devices()
        self._log_requirements()

    def _build_side_panel(self) -> QWidget:
        panel = QWidget()
        layout = QVBoxLayout(panel)

        layout.addWidget(QLabel("Mode"))
        layout.addWidget(self.mode_combo)
        layout.addWidget(QLabel("Bridge"))
        layout.addWidget(self.bridge_combo)
        layout.addWidget(QLabel("Speed"))
        layout.addWidget(self.speed_input)
        layout.addWidget(QLabel("GPS jitter"))
        layout.addWidget(self.jitter_input)
        layout.addWidget(self.points_label)
        layout.addWidget(self.current_label)
        layout.addWidget(self.status_label)
        layout.addWidget(self.device_label)

        buttons = QHBoxLayout()
        buttons.addWidget(self.start_button)
        buttons.addWidget(self.stop_button)
        layout.addLayout(buttons)
        layout.addWidget(self.undo_button)
        layout.addWidget(self.clear_button)
        layout.addWidget(self.output_button)
        layout.addWidget(self.tunneld_button)
        layout.addWidget(self.refresh_button)
        layout.addWidget(QLabel("Log"))
        layout.addWidget(self.log, stretch=1)

        return panel

    def _add_point(self, point: Coordinate) -> None:
        if self.mode_combo.currentText() == "Single point":
            self._points = [point]
        else:
            self._points.append(point)
        self.points_label.setText(f"Points: {len(self._points)}")
        self.current_label.setText(f"Current: {point.lat:.6f}, {point.lon:.6f}")
        self.map_view.set_points(self._points)
        self.map_view.set_current_position(point, "Selected")
        self._log(f"Added point: {point.lat:.6f}, {point.lon:.6f}")

    def _clear_points(self) -> None:
        self.simulation_timer.stop()
        self.map_view.stop_current_animation("Stopped")
        self._points.clear()
        self.points_label.setText("Points: 0")
        self.current_label.setText("Current: -")
        self.map_view.clear_points()
        self.map_view.clear_current_position()
        self.status_label.setText("Status: idle")
        self._log("Cleared points.")

    def _remove_last_point(self) -> None:
        if not self._points:
            return

        removed = self._points.pop()
        self.points_label.setText(f"Points: {len(self._points)}")
        self.map_view.set_points(self._points)
        if self._points:
            point = self._points[-1]
            self.current_label.setText(f"Current: {point.lat:.6f}, {point.lon:.6f}")
            self.map_view.set_current_position(point, "Selected")
        else:
            self.current_label.setText("Current: -")
            self.map_view.clear_current_position()
        self._log(f"Removed point: {removed.lat:.6f}, {removed.lon:.6f}")

    def _open_output_folder(self) -> None:
        self._output_dir.mkdir(parents=True, exist_ok=True)
        os.startfile(self._output_dir)

    def _selected_points(self) -> list[Coordinate]:
        if self.mode_combo.currentText() == "Single point":
            return self._points[-1:] if self._points else []
        return list(self._points)

    def _start(self) -> None:
        if self.stop_button.isEnabled():
            QMessageBox.information(self, "Simulation active", "Stop the current simulation before starting another one.")
            return

        points = self._selected_points()
        if not points:
            QMessageBox.warning(self, "Missing location", "Click the map to add at least one point first.")
            return
        if self.mode_combo.currentText() == "Route" and len(points) < 2:
            QMessageBox.warning(self, "Missing route", "Route mode requires at least two points.")
            return

        timestamp = datetime.now().strftime("%Y%m%d_%H%M%S")
        path = self._output_dir / f"simulation_{timestamp}.gpx"
        speed_kmh = float(self.speed_input.value())
        jitter_meters = float(self.jitter_input.value()) if self.mode_combo.currentText() == "Route" else 0.0
        write_gpx(path, points, speed_kmh=speed_kmh, jitter_meters=jitter_meters)
        simulation_points = build_gpx_points(points, speed_kmh=speed_kmh, jitter_meters=jitter_meters)

        if self.bridge_combo.currentText() == "iPhone" and self.mode_combo.currentText() == "Single point":
            point = points[0]
            result = self._iphone_bridge.set_location(point.lat, point.lon)
        else:
            result = self._bridge.start_location(str(path))

        self._log(result.message)
        if result.detail:
            self._log(f"Detail: {result.detail}")
        self._log(f"GPX: {path}")
        if result.ok:
            self._set_running(True)
            self._start_simulation_marker(simulation_points)
        if jitter_meters > 0:
            self._log(f"Route GPS jitter: up to {jitter_meters:.1f} m")

    def _stop(self) -> None:
        self.simulation_timer.stop()
        self.map_view.stop_current_animation("Stopped")
        result = self._bridge.stop_location()
        self._log(result.message)
        if result.detail:
            self._log(f"Detail: {result.detail}")
        self.current_label.setText("Current: stopped")
        if self._points:
            self.map_view.set_current_position(self._points[-1], "Stopped")
        self._set_running(False)

    def _refresh_devices(self) -> None:
        devices = self._bridge.list_devices()
        if devices:
            names = ", ".join(f"{device.name} ({device.id})" for device in devices)
            self.device_label.setText(f"Device: {names}")
            self._log(f"Detected device(s): {names}")
        else:
            self.device_label.setText("Device: dry-run / no iPhone bridge detected")
            self._log("No iPhone bridge device detected. Dry-run remains available.")

    def _start_tunneld(self) -> None:
        result = start_tunneld_admin(Path.cwd())
        self._log(result.message)
        if result.detail:
            self._log(f"Detail: {result.detail}")
        if not result.ok:
            QMessageBox.warning(self, "tunneld", result.message)

    def _log_requirements(self) -> None:
        for requirement in self._bridge.check_requirements():
            status = "OK" if requirement.ok else "WARN"
            self._log(f"[{status}] {requirement.name}: {requirement.message}")

    def _bridge_mode_changed(self, value: str) -> None:
        self._bridge = self._iphone_bridge if value == "iPhone" else self._dry_run_bridge
        self._log(f"Bridge mode changed to {value}.")
        self._refresh_devices()
        self._log_requirements()

    def _mode_changed(self, value: str) -> None:
        if value == "Single point" and len(self._points) > 1:
            self._points = self._points[-1:]
            self.points_label.setText(f"Points: {len(self._points)}")
            point = self._points[0]
            self.current_label.setText(f"Current: {point.lat:.6f}, {point.lon:.6f}")
            self.map_view.set_points(self._points)
            self.map_view.set_current_position(point, "Selected")
            self._log("Single point mode keeps only the latest point.")

    def _start_simulation_marker(self, points: list[Coordinate]) -> None:
        self.simulation_timer.stop()
        self._simulation_points = points
        self._simulation_index = 0
        self._show_current_position(points[0], "Simulating")

        if self.mode_combo.currentText() == "Route" and len(points) > 1:
            self.map_view.start_current_animation(points, step_ms=self.simulation_timer.interval())
            self.simulation_timer.start()

    def _advance_simulation_marker(self) -> None:
        if not self._simulation_points:
            self.simulation_timer.stop()
            return

        if self._simulation_index >= len(self._simulation_points) - 1:
            self.simulation_timer.stop()
            self.map_view.stop_current_animation("Arrived")
            self._show_current_position(self._simulation_points[-1], "Arrived")
            self.status_label.setText("Status: arrived; press Stop to clear")
            return

        self._simulation_index += 1
        point = self._simulation_points[self._simulation_index]
        self.current_label.setText(f"Current: {point.lat:.6f}, {point.lon:.6f}")

    def _show_current_position(self, point: Coordinate, status: str) -> None:
        self.current_label.setText(f"Current: {point.lat:.6f}, {point.lon:.6f}")
        self.map_view.set_current_position(point, status)

    def _set_running(self, running: bool) -> None:
        self.start_button.setEnabled(not running)
        self.stop_button.setEnabled(running)
        self.clear_button.setEnabled(not running)
        self.undo_button.setEnabled(not running)
        self.mode_combo.setEnabled(not running)
        self.bridge_combo.setEnabled(not running)
        self.speed_input.setEnabled(not running)
        self.jitter_input.setEnabled(not running)
        self.map_view.set_editing_locked(running)
        self.status_label.setText("Status: running" if running else "Status: idle")

    def _log(self, message: str) -> None:
        self.log.appendPlainText(f"{datetime.now().strftime('%H:%M:%S')}  {message}")


def run() -> None:
    try:
        app = QApplication(sys.argv)
        window = MainWindow()
        window.show()
        sys.exit(app.exec())
    except ImportError as exc:
        raise SystemExit(
            "PySide6 is required for the GUI. Install dependencies with: "
            ".\\.venv\\Scripts\\python.exe -m pip install -r requirements.txt"
        ) from exc
