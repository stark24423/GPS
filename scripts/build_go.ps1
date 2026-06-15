$ErrorActionPreference = "Stop"

$Root = Split-Path -Parent $PSScriptRoot
Set-Location $Root

Write-Host "Running Go core tests..."
go test .\internal\core .\internal\ioscore\... .\internal\iostunnel\... .\internal\ioslocation .\internal\iosbridge

$gcc = Get-Command gcc -ErrorAction SilentlyContinue
if (-not $gcc) {
    $KnownGccPaths = @(
        "C:\msys64\ucrt64\bin\gcc.exe",
        "C:\msys64\mingw64\bin\gcc.exe",
        "C:\Qt\Tools\mingw1310_64\bin\gcc.exe"
    )
    foreach ($Path in $KnownGccPaths) {
        if (Test-Path $Path) {
            $BinDir = Split-Path -Parent $Path
            $env:PATH = "$BinDir;$env:PATH"
            $gcc = Get-Command gcc -ErrorAction SilentlyContinue
            break
        }
    }
}
if (-not $gcc) {
    throw "gcc was not found in PATH. Install MSYS2 MinGW-w64 and add its bin directory before building the Fyne desktop exe."
}
Write-Host "Using gcc: $($gcc.Source)"

$env:CGO_ENABLED = "1"
New-Item -ItemType Directory -Force -Path .\dist | Out-Null

Write-Host "Building gps-simulator-go.exe..."
go build -v -o .\dist\gps-simulator-go.exe .\cmd\gps-simulator

if (Test-Path .\wintun.dll) {
    Copy-Item -LiteralPath .\wintun.dll -Destination .\dist\wintun.dll -Force
    Write-Host "Copied wintun.dll to dist."
}

Write-Host "Build complete: dist\gps-simulator-go.exe"
