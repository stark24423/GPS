from pathlib import Path
from unittest.mock import patch

from src.tunneld import get_tunneld_pid, start_tunneld_admin, stop_tunneld_admin


def test_start_tunneld_reports_missing_script(tmp_path):
    result = start_tunneld_admin(tmp_path)

    assert result.ok is False
    assert "not found" in result.message


def test_start_tunneld_launches_powershell_when_script_exists(tmp_path):
    scripts = tmp_path / "scripts"
    scripts.mkdir()
    (scripts / "launch_tunneld_background.ps1").write_text("Write-Host test", encoding="utf-8")

    with patch("src.tunneld.ctypes.windll.shell32.ShellExecuteW", return_value=33) as shell_execute:
        result = start_tunneld_admin(Path(tmp_path))

    assert result.ok is True
    shell_execute.assert_called_once()


def test_stop_tunneld_launches_powershell_when_script_exists(tmp_path):
    scripts = tmp_path / "scripts"
    scripts.mkdir()
    (scripts / "stop_tunneld_background.ps1").write_text("Write-Host test", encoding="utf-8")

    with patch("src.tunneld.ctypes.windll.shell32.ShellExecuteW", return_value=33) as shell_execute:
        result = stop_tunneld_admin(Path(tmp_path))

    assert result.ok is True
    shell_execute.assert_called_once()


def test_get_tunneld_pid_reads_pid_file(tmp_path):
    output = tmp_path / "output"
    output.mkdir()
    (output / "tunneld.pid").write_text("1234", encoding="ascii")

    with patch("src.tunneld.subprocess.run") as run:
        run.return_value.stdout = "running"
        assert get_tunneld_pid(tmp_path) == 1234


def test_get_tunneld_pid_ignores_stale_pid_file(tmp_path):
    output = tmp_path / "output"
    output.mkdir()
    (output / "tunneld.pid").write_text("1234", encoding="ascii")

    with patch("src.tunneld.subprocess.run") as run:
        run.return_value.stdout = ""
        assert get_tunneld_pid(tmp_path) is None
