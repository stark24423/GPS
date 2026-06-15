# Go/Fyne GPS Simulator

This is the Go implementation of the existing Python GPS Simulator.

## Features

- Fyne desktop UI.
- Native Go route interpolation, GPS jitter, and GPX generation.
- OpenStreetMap tile preview with click-to-add points, route line, follow mode, zoom, and drag pan.
- Single point and route simulation modes.
- Dry-run mode that only writes GPX files.
- iPhone mode is being migrated to a built-in Go iOS core:
  - self-contained usbmux plist framing,
  - lockdown device queries,
  - Windows USB iOS 17.4+ tunnel manager scaffold,
  - explicit Wintun/admin/iOS-version checks.
- Joystick movement that can stream location updates to the selected iPhone.

## Requirements

- Go 1.22 or newer.
- A Windows C compiler available as `gcc` for Fyne/GLFW builds.
  - MSYS2 MinGW-w64 is the usual Windows setup.
- Ensure the MinGW `bin` directory is on `PATH`.
- `CGO_ENABLED=1` for desktop builds.
- For built-in iPhone tunnel testing:
  - Windows,
  - USB iPhone,
  - iOS 17.4 or newer,
  - Developer Mode enabled,
  - Trust this computer accepted,
  - Administrator privileges,
  - `wintun.dll` available next to the exe or in the DLL search path.

The built-in CoreDeviceProxy tunnel transport and DVT location protocol are still under implementation. Current builds are suitable for UI/core/device-scan testing and will return explicit errors for unfinished tunnel/location stages.

## Run

```powershell
$env:CGO_ENABLED = "1"
go run .\cmd\gps-simulator
```

## Build

```powershell
$env:PATH = "C:\msys64\ucrt64\bin;$env:PATH"
$env:CGO_ENABLED = "1"
go build -o .\dist\gps-simulator-go.exe .\cmd\gps-simulator
```

Or use:

```powershell
.\scripts\build_go.ps1
```

### GoLand

Fyne depends on OpenGL/GLFW through CGO. If GoLand shows:

```text
github.com/go-gl/gl/v2.1/gl: build constraints exclude all Go files
```

the run configuration is building with CGO disabled. Configure the GoLand run/build environment:

```text
CGO_ENABLED=1
PATH=C:\msys64\ucrt64\bin;%PATH%
```

Use the package/directory target `.\cmd\gps-simulator` rather than only treating `main.go` as a standalone scratch file.

## Tests

The non-UI core and iOS protocol scaffold can be checked without a local iPhone:

```powershell
go test .\internal\core .\internal\ioscore\... .\internal\iostunnel\... .\internal\ioslocation .\internal\iosbridge
```

Full `go test ./...` requires the same Fyne build prerequisites as the desktop app.
