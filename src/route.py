from __future__ import annotations

import math
import random
from datetime import datetime, timedelta, timezone

from src.models import Coordinate

EARTH_RADIUS_M = 6_371_000


def haversine_distance_meters(start: Coordinate, end: Coordinate) -> float:
    lat1 = math.radians(start.lat)
    lat2 = math.radians(end.lat)
    dlat = math.radians(end.lat - start.lat)
    dlon = math.radians(end.lon - start.lon)

    a = math.sin(dlat / 2) ** 2 + math.cos(lat1) * math.cos(lat2) * math.sin(dlon / 2) ** 2
    return 2 * EARTH_RADIUS_M * math.atan2(math.sqrt(a), math.sqrt(1 - a))


def interpolate_points(start: Coordinate, end: Coordinate, step_meters: float) -> list[Coordinate]:
    if step_meters <= 0:
        raise ValueError("step_meters must be greater than zero")

    distance = haversine_distance_meters(start, end)
    if distance == 0:
        return [start]

    segments = max(1, math.ceil(distance / step_meters))
    points: list[Coordinate] = []
    for index in range(segments + 1):
        ratio = index / segments
        points.append(
            Coordinate(
                lat=start.lat + (end.lat - start.lat) * ratio,
                lon=start.lon + (end.lon - start.lon) * ratio,
                elevation=start.elevation + (end.elevation - start.elevation) * ratio,
            )
        )
    return points


def build_timed_route(
    points: list[Coordinate],
    speed_kmh: float,
    start_time: datetime | None = None,
    tick_seconds: int = 1,
) -> list[tuple[Coordinate, datetime]]:
    if not points:
        raise ValueError("at least one coordinate is required")
    if speed_kmh <= 0:
        raise ValueError("speed_kmh must be greater than zero")
    if tick_seconds <= 0:
        raise ValueError("tick_seconds must be greater than zero")

    current_time = start_time or datetime.now(timezone.utc)
    if current_time.tzinfo is None:
        current_time = current_time.replace(tzinfo=timezone.utc)

    speed_mps = speed_kmh * 1000 / 3600
    step_meters = speed_mps * tick_seconds
    timed: list[tuple[Coordinate, datetime]] = [(points[0], current_time)]

    for segment_start, segment_end in zip(points, points[1:]):
        segment_points = interpolate_points(segment_start, segment_end, step_meters)
        for point in segment_points[1:]:
            current_time += timedelta(seconds=tick_seconds)
            timed.append((point, current_time))

    return timed


def jitter_coordinate(point: Coordinate, radius_meters: float, rng: random.Random | None = None) -> Coordinate:
    if radius_meters <= 0:
        return point

    generator = rng or random.Random()
    distance = generator.uniform(0, radius_meters)
    bearing = generator.uniform(0, 2 * math.pi)
    north_meters = math.cos(bearing) * distance
    east_meters = math.sin(bearing) * distance

    lat_offset = north_meters / 111_320
    lon_scale = max(0.01, math.cos(math.radians(point.lat)))
    lon_offset = east_meters / (111_320 * lon_scale)
    return Coordinate(lat=point.lat + lat_offset, lon=point.lon + lon_offset, elevation=point.elevation)


def apply_route_jitter(
    timed_points: list[tuple[Coordinate, datetime]],
    radius_meters: float,
    seed: int | None = None,
) -> list[tuple[Coordinate, datetime]]:
    if radius_meters <= 0 or len(timed_points) < 3:
        return timed_points

    rng = random.Random(seed)
    jittered: list[tuple[Coordinate, datetime]] = [timed_points[0]]
    for point, timestamp in timed_points[1:-1]:
        jittered.append((jitter_coordinate(point, radius_meters, rng), timestamp))
    jittered.append(timed_points[-1])
    return jittered
