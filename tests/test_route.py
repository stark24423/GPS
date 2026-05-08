from src.models import Coordinate
from src.route import apply_route_jitter, build_timed_route, haversine_distance_meters


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


def test_route_jitter_keeps_endpoints_and_moves_middle_points():
    points = [Coordinate(25.033, 121.5654), Coordinate(25.04, 121.57)]
    route = build_timed_route(points, speed_kmh=30)

    jittered = apply_route_jitter(route, radius_meters=5, seed=1)

    assert jittered[0][0] == route[0][0]
    assert jittered[-1][0] == route[-1][0]
    assert any(original[0] != changed[0] for original, changed in zip(route[1:-1], jittered[1:-1]))
