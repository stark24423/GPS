from src.bridge.dry_run import DryRunBridge


def test_dry_run_bridge_reports_missing_gpx():
    result = DryRunBridge().start_location("missing-file.gpx")

    assert result.ok is False
    assert "does not exist" in result.message


def test_dry_run_stop_succeeds():
    result = DryRunBridge().stop_location()

    assert result.ok is True
    assert "Dry-run stop" in result.message
