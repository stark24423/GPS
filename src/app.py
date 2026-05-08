from __future__ import annotations

import sys
from datetime import datetime
from pathlib import Path

from PySide6.QtCore import Qt
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
from src.gpx import write_gpx
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

        self.map_view = MapView()
        self.map_view.point_added.connect(self._add_point)
        self.map_view.points_cleared.connect(self._clear_points)

        self.mode_combo = QComboBox()
        self.mode_combo.addItems(["Single point", "Route"])

        self.bridge_combo = QComboBox()
        self.bridge_combo.addItems(["Dry-run", "iPhone"])
        self.bridge_combo.currentTextChanged.connect(self._bridge_mode_changed)

        self.speed_input = QDoubleSpinBox()
        self.speed_input.setRange(0.1, 300.0)
        self.speed_input.setValue(5.0)
        self.speed_input.setSuffix(" km/h")
        self.speed_input.setDecimals(1)

        self.device_label = QLabel("Device: dry-run / no bridge configured")
        self.points_label = QLabel("Points: 0")

        self.start_button = QPushButton("Start")
        self.stop_button = QPushButton("Stop")
        self.clear_button = QPushButton("Clear points")
        self.refresh_button = QPushButton("Refresh devices")
        self.tunneld_button = QPushButton("Start tunneld")

        self.start_button.clicked.connect(self._start)
        self.stop_button.clicked.connect(self._stop)
        self.clear_button.clicked.connect(self._clear_points)
        self.refresh_button.clicked.connect(self._refresh_devices)
        self.tunneld_button.clicked.connect(self._start_tunneld)

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
        layout.addWidget(self.points_label)
        layout.addWidget(self.device_label)

        buttons = QHBoxLayout()
        buttons.addWidget(self.start_button)
        buttons.addWidget(self.stop_button)
        layout.addLayout(buttons)
        layout.addWidget(self.clear_button)
        layout.addWidget(self.tunneld_button)
        layout.addWidget(self.refresh_button)
        layout.addWidget(QLabel("Log"))
        layout.addWidget(self.log, stretch=1)

        return panel

    def _add_point(self, point: Coordinate) -> None:
        self._points.append(point)
        self.points_label.setText(f"Points: {len(self._points)}")
        self.map_view.set_points(self._points)
        self._log(f"Added point: {point.lat:.6f}, {point.lon:.6f}")

    def _clear_points(self) -> None:
        self._points.clear()
        self.points_label.setText("Points: 0")
        self.map_view.clear_points()
        self._log("Cleared points.")

    def _selected_points(self) -> list[Coordinate]:
        if self.mode_combo.currentText() == "Single point":
            return self._points[-1:] if self._points else []
        return list(self._points)

    def _start(self) -> None:
        points = self._selected_points()
        if not points:
            QMessageBox.warning(self, "Missing location", "Click the map to add at least one point first.")
            return
        if self.mode_combo.currentText() == "Route" and len(points) < 2:
            QMessageBox.warning(self, "Missing route", "Route mode requires at least two points.")
            return

        timestamp = datetime.now().strftime("%Y%m%d_%H%M%S")
        path = self._output_dir / f"simulation_{timestamp}.gpx"
        write_gpx(path, points, speed_kmh=float(self.speed_input.value()))

        if self.bridge_combo.currentText() == "iPhone" and self.mode_combo.currentText() == "Single point":
            point = points[0]
            result = self._iphone_bridge.set_location(point.lat, point.lon)
        else:
            result = self._bridge.start_location(str(path))

        self._log(result.message)
        if result.detail:
            self._log(f"Detail: {result.detail}")
        self._log(f"GPX: {path}")

    def _stop(self) -> None:
        result = self._bridge.stop_location()
        self._log(result.message)
        if result.detail:
            self._log(f"Detail: {result.detail}")

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
