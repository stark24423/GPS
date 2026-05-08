from src.gpx import build_gpx_points, generate_gpx
from src.models import Coordinate


def test_generate_single_point_gpx_contains_waypoint():
    xml = generate_gpx([Coordinate(25.033, 121.5654)])

    assert "<wpt lat=\"25.03300000\" lon=\"121.56540000\">" in xml
    assert "<time>" in xml


def test_generate_route_gpx_contains_track_points():
    xml = generate_gpx(
        [
            Coordinate(25.033, 121.5654),
            Coordinate(25.034, 121.5664),
        ],
        speed_kmh=5,
    )

    assert "<trk>" in xml
    assert "<trkpt lat=\"25.03300000\" lon=\"121.56540000\">" in xml
    assert "25.03400000" in xml


def test_generate_route_gpx_can_apply_jitter_without_moving_endpoints():
    xml = generate_gpx(
        [
            Coordinate(25.033, 121.5654),
            Coordinate(25.04, 121.57),
        ],
        speed_kmh=30,
        jitter_meters=5,
    )

    assert "<trkpt lat=\"25.03300000\" lon=\"121.56540000\">" in xml
    assert "25.04000000" in xml


def test_build_gpx_points_returns_interpolated_preview_points():
    points = build_gpx_points(
        [
            Coordinate(25.033, 121.5654),
            Coordinate(25.04, 121.57),
        ],
        speed_kmh=30,
        jitter_meters=5,
    )

    assert len(points) > 2
    assert points[0] == Coordinate(25.033, 121.5654)
    assert points[-1] == Coordinate(25.04, 121.57)
