$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent $PSScriptRoot
Set-Location $Root

.\.venv\Scripts\pymobiledevice3.exe developer dvt simulate-location clear
