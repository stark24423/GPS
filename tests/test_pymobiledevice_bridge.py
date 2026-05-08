from pathlib import Path
import subprocess
from unittest.mock import Mock, patch

from src.bridge.pymobiledevice import PymobileDeviceBridge


def test_pymobiledevice_set_location_keeps_running_process():
    bridge = PymobileDeviceBridge("pymobiledevice3")
    process = Mock()
    process.poll.return_value = None
    process.wait.side_effect = TimeoutError

    with patch("src.bridge.pymobiledevice.subprocess.Popen", return_value=process):
        with patch("src.bridge.pymobiledevice.subprocess.TimeoutExpired", TimeoutError):
            result = bridge.set_location(25.033964, 121.564468)

    assert result.ok is True
    assert "Set iPhone simulated location" in result.message


def test_pymobiledevice_start_location_requires_existing_gpx():
    bridge = PymobileDeviceBridge("pymobiledevice3")

    result = bridge.start_location(str(Path("missing.gpx")))

    assert result.ok is False
    assert "does not exist" in result.message


def test_pymobiledevice_stop_times_out_without_hanging():
    bridge = PymobileDeviceBridge("pymobiledevice3")

    with patch(
        "src.bridge.pymobiledevice.subprocess.run",
        side_effect=subprocess.TimeoutExpired(["pymobiledevice3"], 8),
    ):
        result = bridge.stop_location()

    assert result.ok is False
    assert "timed out" in result.detail
