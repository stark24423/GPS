from pathlib import Path
from unittest.mock import patch

from src.tunneld import start_tunneld_admin


def test_start_tunneld_reports_missing_script(tmp_path):
    result = start_tunneld_admin(tmp_path)

    assert result.ok is False
    assert "not found" in result.message


def test_start_tunneld_launches_powershell_when_script_exists(tmp_path):
    scripts = tmp_path / "scripts"
    scripts.mkdir()
    (scripts / "launch_tunneld_elevated.ps1").write_text("Write-Host test", encoding="utf-8")

    with patch("src.tunneld.ctypes.windll.shell32.ShellExecuteW", return_value=33) as shell_execute:
        result = start_tunneld_admin(Path(tmp_path))

    assert result.ok is True
    shell_execute.assert_called_once()
