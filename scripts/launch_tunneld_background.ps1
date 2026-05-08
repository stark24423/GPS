$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent $PSScriptRoot
$OutputDir = Join-Path $Root "output"
$PidFile = Join-Path $OutputDir "tunneld.pid"
$OutLog = Join-Path $OutputDir "tunneld.out.log"
$ErrLog = Join-Path $OutputDir "tunneld.err.log"
$Python = Join-Path $Root ".venv\Scripts\python.exe"

New-Item -ItemType Directory -Force -Path $OutputDir | Out-Null

if (Test-Path $PidFile) {
    $ExistingPid = Get-Content $PidFile -ErrorAction SilentlyContinue
    if ($ExistingPid) {
        $Existing = Get-Process -Id ([int]$ExistingPid) -ErrorAction SilentlyContinue
        if ($Existing) {
            Add-Content -Path $OutLog -Value "$(Get-Date -Format s) tunneld already running with PID $ExistingPid"
            exit 0
        }
    }
}

$Process = Start-Process `
    -FilePath $Python `
    -ArgumentList @("-m", "pymobiledevice3", "remote", "tunneld", "--protocol", "tcp") `
    -WorkingDirectory $Root `
    -WindowStyle Hidden `
    -RedirectStandardOutput $OutLog `
    -RedirectStandardError $ErrLog `
    -PassThru

$Process.Id | Set-Content -Path $PidFile -Encoding ascii
Add-Content -Path $OutLog -Value "$(Get-Date -Format s) started tunneld with PID $($Process.Id)"
