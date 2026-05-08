$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent $PSScriptRoot
$OutputDir = Join-Path $Root "output"
$PidFile = Join-Path $OutputDir "tunneld.pid"
$OutLog = Join-Path $OutputDir "tunneld.out.log"

if (!(Test-Path $PidFile)) {
    Add-Content -Path $OutLog -Value "$(Get-Date -Format s) no tunneld pid file found"
    exit 0
}

$PidValue = Get-Content $PidFile -ErrorAction SilentlyContinue
if ($PidValue) {
    $Process = Get-Process -Id ([int]$PidValue) -ErrorAction SilentlyContinue
    if ($Process) {
        Stop-Process -Id $Process.Id -Force
        Add-Content -Path $OutLog -Value "$(Get-Date -Format s) stopped tunneld PID $PidValue"
    }
}

Remove-Item -LiteralPath $PidFile -Force -ErrorAction SilentlyContinue
