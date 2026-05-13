from __future__ import annotations

import math
import os
import sys
import threading
from datetime import datetime
from pathlib import Path

from PySide6.QtCore import QTimer, Qt, Signal
from PySide6.QtWidgets import (
    QApplication,
    QComboBox,
    QDoubleSpinBox,
    QFormLayout,
    QGroupBox,
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
from src.gpx import build_timed_points, write_gpx_from_timed
from src.iphone_controller import IPhoneController
from src.joystick import JoystickWidget
from src.map_view import MapView
from src.models import Coordinate
from src.tunneld import get_tunneld_pid, start_tunneld_admin


STYLE_SHEET = """
* { font-family: "Segoe UI", "Microsoft JhengHei UI", system-ui, sans-serif; font-size: 13px; color: #0f172a; }
QMainWindow, QWidget { background: #f1f5f9; }
#sidePanel { background: #ffffff; border-left: 1px solid #e2e8f0; }
#title { font-size: 16px; font-weight: 600; color: #0f172a; padding: 2px 0; }
#subtitle { font-size: 11px; color: #64748b; }
QGroupBox {
    background: #ffffff;
    border: 1px solid #e2e8f0;
    border-radius: 8px;
    margin-top: 10px;
    padding: 8px 10px 6px 10px;
    font-weight: 600;
    color: #334155;
}
QGroupBox::title { subcontrol-origin: margin; left: 10px; padding: 0 4px; color: #475569; font-size: 12px; }
QLabel { color: #334155; background: transparent; }
QLabel#muted { color: #64748b; font-size: 12px; }
QComboBox, QDoubleSpinBox {
    background: #ffffff; border: 1px solid #cbd5e1; border-radius: 6px;
    padding: 4px 8px; min-height: 18px;
    selection-background-color: #2563eb; selection-color: white;
}
QComboBox:focus, QDoubleSpinBox:focus { border-color: #2563eb; }
QComboBox::drop-down { border: none; width: 20px; }
QComboBox QAbstractItemView {
    background: white; border: 1px solid #cbd5e1; border-radius: 6px;
    selection-background-color: #2563eb; selection-color: white; outline: 0;
}
QDoubleSpinBox::up-button, QDoubleSpinBox::down-button { width: 16px; border: none; background: transparent; }
QPushButton {
    background: #ffffff; border: 1px solid #cbd5e1; border-radius: 6px;
    padding: 5px 12px; color: #0f172a; font-weight: 500;
}
QPushButton:hover { background: #f1f5f9; border-color: #94a3b8; }
QPushButton:pressed { background: #e2e8f0; }
QPushButton:disabled { color: #94a3b8; background: #f8fafc; border-color: #e2e8f0; }
QPushButton#primary { background: #2563eb; color: white; border: 1px solid #2563eb; }
QPushButton#primary:hover { background: #1d4ed8; border-color: #1d4ed8; }
QPushButton#primary:pressed { background: #1e40af; }
QPushButton#primary:disabled { background: #93c5fd; color: white; border-color: #93c5fd; }
QPushButton#danger { background: #ef4444; color: white; border: 1px solid #ef4444; }
QPushButton#danger:hover { background: #dc2626; border-color: #dc2626; }
QPushButton#danger:disabled { background: #fca5a5; color: white; border-color: #fca5a5; }
QLabel#statusDot { border-radius: 6px; min-width: 12px; max-width: 12px; min-height: 12px; max-height: 12px; }
QLabel#statusDot[state="idle"] { background: #94a3b8; }
QLabel#statusDot[state="running"] { background: #22c55e; }
QLabel#statusDot[state="arrived"] { background: #f59e0b; }
QLabel#statusDot[state="error"] { background: #ef4444; }
QPlainTextEdit {
    background: #0f172a; color: #cbd5e1;
    border: 1px solid #1e293b; border-radius: 8px; padding: 8px;
    font-family: "Cascadia Mono", "Consolas", "Courier New", monospace;
    font-size: 12px;
}
QSplitter::handle { background: #e2e8f0; width: 1px; }
QScrollBar:vertical { background: transparent; width: 10px; margin: 2px; }
QScrollBar::handle:vertical { background: #cbd5e1; border-radius: 5px; min-height: 24px; }
QScrollBar::handle:vertical:hover { background: #94a3b8; }
QScrollBar::add-line:vertical, QScrollBar::sub-line:vertical { height: 0; }
"""


PROJECT_ROOT = Path(__file__).resolve().parent.parent


class MainWindow(QMainWindow):
    iphone_scan_completed = Signal(list, str)
    iphone_op_completed = Signal(bool, str, str)

    def __init__(self) -> None:
        super().__init__()
        self.setWindowTitle("GPS Simulator")
        self.resize(1080, 680)
        self.setStyleSheet(STYLE_SHEET)

        self._points: list[Coordinate] = []
        self._dry_run_bridge = DryRunBridge()
        self._iphone_bridge = PymobileDeviceBridge(str(PROJECT_ROOT / ".venv" / "Scripts" / "pymobiledevice3.exe"))
        self._iphone_controller = IPhoneController()
        self._bridge = self._dry_run_bridge
        self._output_dir = PROJECT_ROOT / "output"
        self._simulation_points: list[Coordinate] = []
        self._simulation_index = 0
        self.iphone_op_completed.connect(self._on_iphone_op_completed)

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
        self.device_label.setObjectName("muted")
        self.device_label.setWordWrap(True)
        self.points_label = QLabel("Points: 0")
        self.current_label = QLabel("Current: -")
        self.current_label.setWordWrap(True)
        self.status_label = QLabel("Idle")
        self.tunneld_label = QLabel("tunneld: unknown")
        self.tunneld_label.setObjectName("muted")
        self.status_dot = QLabel()
        self.status_dot.setObjectName("statusDot")
        self.status_dot.setProperty("state", "idle")

        self.start_button = QPushButton("Start")
        self.start_button.setObjectName("primary")
        self.stop_button = QPushButton("Stop")
        self.stop_button.setObjectName("danger")
        self.clear_button = QPushButton("Clear points")
        self.undo_button = QPushButton("Undo last")
        self.output_button = QPushButton("Open output folder")
        self.refresh_button = QPushButton("Refresh devices")
        self.stop_button.setEnabled(False)

        self.start_button.clicked.connect(self._start)
        self.stop_button.clicked.connect(self._stop)
        self.clear_button.clicked.connect(self._clear_points)
        self.undo_button.clicked.connect(self._remove_last_point)
        self.output_button.clicked.connect(self._open_output_folder)
        self.refresh_button.clicked.connect(self._refresh_devices)

        self.simulation_timer = QTimer(self)
        self.simulation_timer.setInterval(1000)
        self.simulation_timer.setTimerType(Qt.PreciseTimer)
        self.simulation_timer.timeout.connect(self._advance_simulation_marker)

        self.joystick = JoystickWidget()
        self.joystick.direction_changed.connect(self._joystick_direction_changed)
        self._joystick_dx = 0.0
        self._joystick_dy = 0.0
        self._joystick_position: Coordinate | None = None
        self.joystick_timer = QTimer(self)
        self.joystick_timer.setInterval(80)
        self.joystick_timer.setTimerType(Qt.PreciseTimer)
        self.joystick_timer.timeout.connect(self._joystick_tick)
        self.joystick_speed_input = QDoubleSpinBox()
        self.joystick_speed_input.setRange(0.1, 500.0)
        self.joystick_speed_input.setValue(20.0)
        self.joystick_speed_input.setSuffix(" km/h")
        self.joystick_speed_input.setDecimals(1)

        self.log = QPlainTextEdit()
        self.log.setReadOnly(True)
        self.log.setMaximumBlockCount(1000)
        self.log.setMinimumHeight(80)

        root = QWidget()
        root_layout = QHBoxLayout(root)

        splitter = QSplitter(Qt.Horizontal)
        splitter.addWidget(self.map_view)
        splitter.addWidget(self._build_side_panel())
        splitter.setStretchFactor(0, 5)
        splitter.setStretchFactor(1, 1)
        splitter.setSizes([800, 280])
        root_layout.setContentsMargins(0, 0, 0, 0)
        root_layout.addWidget(splitter)

        self.setCentralWidget(root)
        self._log("Application started in dry-run mode.")
        self._refresh_tunneld_status()
        self._refresh_devices()
        self._log_requirements()
        QTimer.singleShot(300, self._auto_start_tunneld_if_iphone)

    def _build_side_panel(self) -> QWidget:
        panel = QWidget()
        panel.setObjectName("sidePanel")
        panel.setMinimumWidth(280)
        panel.setMaximumWidth(340)
        layout = QVBoxLayout(panel)
        layout.setContentsMargins(12, 12, 12, 12)
        layout.setSpacing(6)

        title = QLabel("GPS Simulator")
        title.setObjectName("title")
        layout.addWidget(title)

        status_group = QGroupBox("Status")
        status_v = QVBoxLayout(status_group)
        status_v.setSpacing(6)
        status_row = QHBoxLayout()
        status_row.setSpacing(8)
        status_row.addWidget(self.status_dot)
        status_row.addWidget(self.status_label, 1)
        status_v.addLayout(status_row)
        status_v.addWidget(self.points_label)
        status_v.addWidget(self.current_label)
        layout.addWidget(status_group)

        settings_group = QGroupBox("Settings")
        form = QFormLayout(settings_group)
        form.setLabelAlignment(Qt.AlignLeft)
        form.setSpacing(8)
        form.addRow("Mode", self.mode_combo)
        form.addRow("Bridge", self.bridge_combo)
        form.addRow("Speed", self.speed_input)
        form.addRow("GPS jitter", self.jitter_input)
        layout.addWidget(settings_group)

        joystick_group = QGroupBox("Joystick")
        joy_v = QVBoxLayout(joystick_group)
        joy_v.setContentsMargins(8, 8, 8, 8)
        joy_v.setSpacing(8)
        joy_v.addWidget(self.joystick, alignment=Qt.AlignHCenter)
        joy_speed_row = QHBoxLayout()
        joy_speed_row.setContentsMargins(0, 0, 0, 0)
        joy_speed_row.addWidget(QLabel("Speed"))
        joy_speed_row.addWidget(self.joystick_speed_input, 1)
        joy_v.addLayout(joy_speed_row)
        layout.addWidget(joystick_group)

        iphone_group = QGroupBox("iPhone")
        iphone_v = QVBoxLayout(iphone_group)
        iphone_v.setSpacing(6)
        iphone_v.addWidget(self.device_label)
        iphone_v.addWidget(self.tunneld_label)
        iphone_v.addWidget(self.refresh_button)
        layout.addWidget(iphone_group)

        action_row = QHBoxLayout()
        action_row.setSpacing(8)
        action_row.addWidget(self.start_button)
        action_row.addWidget(self.stop_button)
        layout.addLayout(action_row)

        tools_row = QHBoxLayout()
        tools_row.setSpacing(8)
        tools_row.addWidget(self.undo_button)
        tools_row.addWidget(self.clear_button)
        layout.addLayout(tools_row)
        layout.addWidget(self.output_button)

        log_label = QLabel("Log")
        log_label.setObjectName("muted")
        layout.addWidget(log_label)
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
        self._joystick_position = Coordinate(lat=point.lat, lon=point.lon)
        self._log(f"Added point: {point.lat:.6f}, {point.lon:.6f}")

    def _clear_points(self) -> None:
        self.simulation_timer.stop()
        self.joystick_timer.stop()
        self.joystick.reset()
        self._joystick_position = None
        self.map_view.stop_current_animation("Stopped")
        self.map_view.set_follow_mode(False)
        self._points.clear()
        self.points_label.setText("Points: 0")
        self.current_label.setText("Current: -")
        self.map_view.clear_points()
        self.map_view.clear_current_position()
        self._set_status("idle", "Idle")
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
        timed_points = build_timed_points(points, speed_kmh=speed_kmh, jitter_meters=jitter_meters)
        simulation_points = [pt for pt, _ in timed_points]
        threading.Thread(
            target=write_gpx_from_timed,
            args=(path, timed_points),
            daemon=True,
        ).start()
        self._log(f"GPX: {path}")
        if jitter_meters > 0:
            self._log(f"Route GPS jitter: up to {jitter_meters:.1f} m")

        self.joystick_timer.stop()
        self.joystick.reset()
        self._set_running(True)
        self._start_simulation_marker(simulation_points)

        if self.bridge_combo.currentText() == "iPhone":
            self._dispatch_iphone_start(points, simulation_points)
        else:
            result = self._bridge.start_location(str(path))
            self._log(result.message)
            if result.detail:
                self._log(f"Detail: {result.detail}")
            if not result.ok:
                self._set_running(False)

    def _dispatch_iphone_start(self, points: list[Coordinate], simulation_points: list[Coordinate]) -> None:
        if self.mode_combo.currentText() == "Single point":
            point = points[0]
            self._log(f"Sending location {point.lat:.6f}, {point.lon:.6f} to iPhone…")
            future = self._iphone_controller.set_location(point.lat, point.lon)
            op_name = "Set iPhone location"
        else:
            self._log(f"Playing route on iPhone ({len(simulation_points)} points)…")
            future = self._iphone_controller.play_route(simulation_points, tick_seconds=1.0)
            op_name = "Play iPhone route"

        def callback(f) -> None:
            try:
                f.result()
                self.iphone_op_completed.emit(True, op_name, "")
            except Exception as exc:
                self.iphone_op_completed.emit(False, op_name, str(exc))

        future.add_done_callback(callback)

    def _on_iphone_op_completed(self, ok: bool, op_name: str, detail: str) -> None:
        if ok:
            self._log(f"{op_name}: OK")
            return

        self._log(f"{op_name} failed: {detail}")
        self._set_status("error", "iPhone error — map preview only")
        QMessageBox.warning(
            self,
            op_name,
            (detail or "Operation failed.")
            + "\n\nLocal map preview will keep running. Press Stop to clear.",
        )

    def _stop(self) -> None:
        self.simulation_timer.stop()
        self.map_view.stop_current_animation("Stopped")
        self.map_view.set_follow_mode(False)

        if self.bridge_combo.currentText() == "iPhone":
            self._iphone_controller.stop()
            self._log("Stopping iPhone simulation…")
        else:
            result = self._bridge.stop_location()
            self._log(result.message)
            if result.detail:
                self._log(f"Detail: {result.detail}")

        self.current_label.setText("Current: stopped")
        if self._points:
            self.map_view.set_current_position(self._points[-1], "Stopped")
        self._set_running(False)

    def _refresh_devices(self) -> None:
        devices = self._iphone_bridge.list_devices()
        if devices:
            names = ", ".join(f"{device.name} ({device.id})" for device in devices)
            self.device_label.setText(f"Device: {names}")
            self._log(f"Detected device(s): {names}")
        else:
            self.device_label.setText("Device: no iPhone detected")
            self._log("No iPhone connected. Dry-run remains available.")

    def _start_tunneld(self) -> None:
        result = start_tunneld_admin(PROJECT_ROOT)
        self._log(result.message)
        if result.detail:
            self._log(f"Detail: {result.detail}")
        if not result.ok:
            QMessageBox.warning(self, "tunneld", result.message)
        self._refresh_tunneld_status()

    def _auto_start_tunneld_if_iphone(self) -> None:
        self.device_label.setText("Device: scanning for iPhone…")
        self._log("Startup scan: looking for connected iPhone…")
        self.iphone_scan_completed.connect(self._on_iphone_scan_completed)

        def worker() -> None:
            try:
                devices = self._iphone_bridge.list_devices()
                self.iphone_scan_completed.emit(devices, "")
            except Exception as exc:
                self.iphone_scan_completed.emit([], str(exc))

        threading.Thread(target=worker, daemon=True).start()

    def _on_iphone_scan_completed(self, devices: list, error: str) -> None:
        if error:
            self.device_label.setText("Device: scan failed")
            self._log(f"iPhone scan failed: {error}")
            return

        if not devices:
            self.device_label.setText("Device: no iPhone detected")
            self._log("Startup scan: no iPhone connected.")
            return

        names = ", ".join(f"{device.name} ({device.id})" for device in devices)
        self.device_label.setText(f"Device: {names}")
        self._log(f"Startup scan: detected iPhone {names}.")

        self.bridge_combo.setCurrentText("iPhone")

        if get_tunneld_pid(PROJECT_ROOT) is not None:
            self._log("tunneld already running; skipping auto-start.")
            self._warm_up_iphone_connection()
            return

        self._log("Auto-starting tunneld (UAC prompt may appear)…")
        self._start_tunneld()
        QTimer.singleShot(3000, self._warm_up_iphone_connection)

    def _warm_up_iphone_connection(self) -> None:
        self._log("Pre-warming iPhone DVT connection…")
        future = self._iphone_controller.warm_up()
        QTimer.singleShot(0, lambda: None)

        def done(f) -> None:
            try:
                f.result()
                QTimer.singleShot(0, lambda: self._log("Pre-warm: OK (first Start will be instant)."))
            except Exception as exc:
                msg = str(exc)
                QTimer.singleShot(0, lambda: self._log(f"Pre-warm skipped: {msg}"))

        future.add_done_callback(done)

    def _refresh_tunneld_status(self) -> None:
        pid = get_tunneld_pid(PROJECT_ROOT)
        self.tunneld_label.setText(f"tunneld: background PID {pid}" if pid else "tunneld: not started by GUI")

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
            self.map_view.set_follow_mode(True)
            self.map_view.start_current_animation(points, step_ms=self.simulation_timer.interval())
            self.simulation_timer.start()
        else:
            self.map_view.set_follow_mode(True)

    def _advance_simulation_marker(self) -> None:
        if not self._simulation_points:
            self.simulation_timer.stop()
            return

        if self._simulation_index >= len(self._simulation_points) - 1:
            self.simulation_timer.stop()
            self.map_view.stop_current_animation("Arrived")
            self._show_current_position(self._simulation_points[-1], "Arrived")
            self._set_status("arrived", "Arrived — press Stop to clear")
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
        if running:
            self._set_status("running", "Running")
        else:
            self._set_status("idle", "Idle")

    def _log(self, message: str) -> None:
        self.log.appendPlainText(f"{datetime.now().strftime('%H:%M:%S')}  {message}")

    def _joystick_direction_changed(self, dx: float, dy: float) -> None:
        self._joystick_dx = dx
        self._joystick_dy = dy
        if dx == 0.0 and dy == 0.0:
            if self.joystick_timer.isActive():
                self.joystick_timer.stop()
            self.map_view.set_follow_mode(False)
            if self._joystick_position is not None:
                self._set_status("idle", "Joystick paused")
            return

        if self.stop_button.isEnabled():
            self._log("Joystick disabled while simulation is running.")
            self.joystick.reset()
            return

        if self._joystick_position is None:
            if self._points:
                last = self._points[-1]
                self._joystick_position = Coordinate(lat=last.lat, lon=last.lon)
            else:
                self._joystick_position = Coordinate(lat=24.7808548, lon=121.0252718)
            self.map_view.set_current_position(self._joystick_position, "Joystick")

        self._set_status("running", "Joystick active")
        self.map_view.set_follow_mode(True)
        if not self.joystick_timer.isActive():
            self.joystick_timer.start()

    def _joystick_tick(self) -> None:
        if self._joystick_position is None:
            return
        dt = self.joystick_timer.interval() / 1000.0
        speed_mps = float(self.joystick_speed_input.value()) * 1000.0 / 3600.0
        east_m = self._joystick_dx * speed_mps * dt
        north_m = self._joystick_dy * speed_mps * dt

        lat = self._joystick_position.lat
        lon = self._joystick_position.lon
        lat_offset = north_m / 111_320.0
        lon_scale = max(0.01, math.cos(math.radians(lat)))
        lon_offset = east_m / (111_320.0 * lon_scale)
        new_pos = Coordinate(lat=lat + lat_offset, lon=lon + lon_offset)
        self._joystick_position = new_pos

        self.current_label.setText(f"Current: {new_pos.lat:.6f}, {new_pos.lon:.6f}")
        self.map_view.set_current_position(new_pos, "Joystick")

        if self.bridge_combo.currentText() == "iPhone":
            future = self._iphone_controller.set_location(new_pos.lat, new_pos.lon)
            future.add_done_callback(self._joystick_iphone_callback)

    def _joystick_iphone_callback(self, future) -> None:
        exc = future.exception()
        if exc is not None:
            msg = str(exc)
            QTimer.singleShot(0, lambda: self._on_joystick_iphone_error(msg))

    def _on_joystick_iphone_error(self, message: str) -> None:
        if not self.joystick_timer.isActive():
            return
        self.joystick_timer.stop()
        self.joystick.reset()
        self._log(f"Joystick iPhone error: {message}")
        self._set_status("error", "iPhone error")

    def _set_status(self, state: str, text: str) -> None:
        self.status_label.setText(text)
        self.status_dot.setProperty("state", state)
        self.status_dot.style().unpolish(self.status_dot)
        self.status_dot.style().polish(self.status_dot)

    def closeEvent(self, event) -> None:  # noqa: N802
        try:
            self._iphone_controller.shutdown()
        except Exception:
            pass
        super().closeEvent(event)


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
