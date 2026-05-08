$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent $PSScriptRoot
Set-Location $Root

Write-Host "Starting pymobiledevice3 tunneld. Keep this Administrator window open while testing."
.\.venv\Scripts\python.exe -m pymobiledevice3 remote tunneld --protocol tcp
