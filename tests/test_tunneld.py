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
    (scripts / "start_tunneld_admin.ps1").write_text("Write-Host test", encoding="utf-8")

    with patch("src.tunneld.subprocess.Popen") as popen:
        result = start_tunneld_admin(Path(tmp_path))

    assert result.ok is True
    popen.assert_called_once()
