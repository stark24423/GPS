from __future__ import annotations

from datetime import datetime, timezone
from pathlib import Path
from xml.etree.ElementTree import Element, SubElement, tostring
from xml.dom import minidom

from src.models import Coordinate
from src.route import apply_route_jitter, build_timed_route


def _format_time(value: datetime) -> str:
    if value.tzinfo is None:
        value = value.replace(tzinfo=timezone.utc)
    return value.astimezone(timezone.utc).isoformat(timespec="seconds").replace("+00:00", "Z")


def _prettify_xml(root: Element) -> str:
    rough = tostring(root, encoding="utf-8")
    return minidom.parseString(rough).toprettyxml(indent="  ", encoding="utf-8").decode("utf-8")


def generate_gpx(
    points: list[Coordinate],
    speed_kmh: float = 5.0,
    name: str = "GPS Simulation",
    jitter_meters: float = 0.0,
) -> str:
    timed_points = build_timed_route(points, speed_kmh=speed_kmh)
    timed_points = apply_route_jitter(timed_points, radius_meters=jitter_meters)

    root = Element(
        "gpx",
        {
            "version": "1.1",
            "creator": "Python iPhone GPS Simulator Prototype",
            "xmlns": "http://www.topografix.com/GPX/1/1",
        },
    )
    metadata = SubElement(root, "metadata")
    SubElement(metadata, "name").text = name
    SubElement(metadata, "time").text = _format_time(timed_points[0][1])

    if len(timed_points) == 1:
        point, timestamp = timed_points[0]
        waypoint = SubElement(root, "wpt", {"lat": f"{point.lat:.8f}", "lon": f"{point.lon:.8f}"})
        SubElement(waypoint, "ele").text = f"{point.elevation:.2f}"
        SubElement(waypoint, "time").text = _format_time(timestamp)
        SubElement(waypoint, "name").text = name
        return _prettify_xml(root)

    track = SubElement(root, "trk")
    SubElement(track, "name").text = name
    segment = SubElement(track, "trkseg")
    for point, timestamp in timed_points:
        track_point = SubElement(segment, "trkpt", {"lat": f"{point.lat:.8f}", "lon": f"{point.lon:.8f}"})
        SubElement(track_point, "ele").text = f"{point.elevation:.2f}"
        SubElement(track_point, "time").text = _format_time(timestamp)

    return _prettify_xml(root)


def write_gpx(
    path: str | Path,
    points: list[Coordinate],
    speed_kmh: float = 5.0,
    jitter_meters: float = 0.0,
) -> Path:
    output_path = Path(path)
    output_path.parent.mkdir(parents=True, exist_ok=True)
    output_path.write_text(generate_gpx(points, speed_kmh=speed_kmh, jitter_meters=jitter_meters), encoding="utf-8")
    return output_path
