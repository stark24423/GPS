from src.models import Coordinate
from src.route import build_timed_route, haversine_distance_meters


def test_haversine_distance_is_positive_for_different_points():
    distance = haversine_distance_meters(Coordinate(25.033, 121.5654), Coordinate(25.034, 121.5664))

    assert distance > 0


def test_slower_speed_generates_more_timed_points():
    points = [Coordinate(25.033, 121.5654), Coordinate(25.04, 121.57)]

    slow_route = build_timed_route(points, speed_kmh=3)
    fast_route = build_timed_route(points, speed_kmh=30)

    assert len(slow_route) > len(fast_route)
    assert slow_route[0][0] == points[0]
    assert slow_route[-1][0] == points[-1]
