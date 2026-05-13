from __future__ import annotations

import math

from PySide6.QtCore import QPointF, Qt, Signal
from PySide6.QtGui import QColor, QPainter, QPen, QRadialGradient
from PySide6.QtWidgets import QWidget


class JoystickWidget(QWidget):
    """Circular joystick. Emits direction vector in [-1, 1] for x (east) and y (north)."""

    direction_changed = Signal(float, float)

    DEAD_ZONE = 0.06

    def __init__(self, parent=None) -> None:
        super().__init__(parent)
        self.setFixedSize(120, 120)
        self.setCursor(Qt.OpenHandCursor)
        self._dragging = False
        self._knob_offset = QPointF(0.0, 0.0)
        self._dx = 0.0
        self._dy = 0.0

    # ----- Geometry -----
    def _center(self) -> QPointF:
        return QPointF(self.width() / 2, self.height() / 2)

    def _outer_radius(self) -> float:
        return min(self.width(), self.height()) / 2 - 6

    def _knob_radius(self) -> float:
        return self._outer_radius() * 0.36

    # ----- Painting -----
    def paintEvent(self, _event) -> None:
        p = QPainter(self)
        p.setRenderHint(QPainter.Antialiasing)

        center = self._center()
        outer_r = self._outer_radius()
        knob_r = self._knob_radius()

        # Outer ring with subtle gradient
        gradient = QRadialGradient(center, outer_r)
        gradient.setColorAt(0.0, QColor("#f8fafc"))
        gradient.setColorAt(1.0, QColor("#e2e8f0"))
        p.setBrush(gradient)
        p.setPen(QPen(QColor("#cbd5e1"), 1.5))
        p.drawEllipse(center, outer_r, outer_r)

        # Center crosshair guide
        p.setPen(QPen(QColor("#cbd5e1"), 1, Qt.DashLine))
        p.drawLine(QPointF(center.x() - outer_r * 0.6, center.y()),
                   QPointF(center.x() + outer_r * 0.6, center.y()))
        p.drawLine(QPointF(center.x(), center.y() - outer_r * 0.6),
                   QPointF(center.x(), center.y() + outer_r * 0.6))

        # Knob
        knob_center = center + self._knob_offset
        knob_grad = QRadialGradient(knob_center - QPointF(0, knob_r * 0.3), knob_r)
        knob_grad.setColorAt(0.0, QColor("#3b82f6"))
        knob_grad.setColorAt(1.0, QColor("#1d4ed8"))
        p.setBrush(knob_grad)
        p.setPen(QPen(QColor("#1e3a8a"), 1.5))
        p.drawEllipse(knob_center, knob_r, knob_r)

        # Compass labels
        p.setPen(QColor("#94a3b8"))
        f = p.font()
        f.setPointSize(8)
        f.setBold(True)
        p.setFont(f)
        p.drawText(int(center.x() - 4), int(center.y() - outer_r + 12), "N")
        p.drawText(int(center.x() - 4), int(center.y() + outer_r - 4), "S")
        p.drawText(int(center.x() + outer_r - 10), int(center.y() + 4), "E")
        p.drawText(int(center.x() - outer_r + 4), int(center.y() + 4), "W")

    # ----- Mouse handling -----
    def mousePressEvent(self, event) -> None:
        if event.button() == Qt.LeftButton:
            self._dragging = True
            self.setCursor(Qt.ClosedHandCursor)
            self._move_knob(event.position())

    def mouseMoveEvent(self, event) -> None:
        if self._dragging:
            self._move_knob(event.position())

    def mouseReleaseEvent(self, event) -> None:
        if event.button() == Qt.LeftButton and self._dragging:
            self._dragging = False
            self.setCursor(Qt.OpenHandCursor)
            self._knob_offset = QPointF(0.0, 0.0)
            self._emit_direction(0.0, 0.0)
            self.update()

    def _move_knob(self, pos: QPointF) -> None:
        offset = pos - self._center()
        outer_r = self._outer_radius()
        distance = math.hypot(offset.x(), offset.y())
        if distance > outer_r:
            offset = QPointF(offset.x() / distance * outer_r, offset.y() / distance * outer_r)
        self._knob_offset = offset

        nx = offset.x() / outer_r
        ny = -offset.y() / outer_r  # screen y inverted, north is positive
        magnitude = math.hypot(nx, ny)
        if magnitude < self.DEAD_ZONE:
            nx, ny = 0.0, 0.0
        self._emit_direction(nx, ny)
        self.update()

    def _emit_direction(self, dx: float, dy: float) -> None:
        if dx == self._dx and dy == self._dy:
            return
        self._dx = dx
        self._dy = dy
        self.direction_changed.emit(dx, dy)

    def reset(self) -> None:
        self._dragging = False
        self._knob_offset = QPointF(0.0, 0.0)
        self._emit_direction(0.0, 0.0)
        self.update()
