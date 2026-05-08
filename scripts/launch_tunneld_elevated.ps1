$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent $PSScriptRoot
$Command = "Set-Location -LiteralPath '$Root'; .\.venv\Scripts\python.exe -m pymobiledevice3 remote tunneld --protocol tcp"

Start-Process `
    -FilePath "powershell.exe" `
    -ArgumentList @("-NoExit", "-ExecutionPolicy", "Bypass", "-Command", $Command) `
    -WorkingDirectory $Root `
    -Verb RunAs
