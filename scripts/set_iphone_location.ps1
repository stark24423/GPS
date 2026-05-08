param(
    [double]$Latitude = 25.033964,
    [double]$Longitude = 121.564468
)

$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent $PSScriptRoot
Set-Location $Root

.\.venv\Scripts\pymobiledevice3.exe developer dvt simulate-location set -- $Latitude $Longitude
