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
  - lockdown pair-record session TLS,
  - CoreDeviceProxy handshake with RSD address/port discovery,
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
  - iOS 18 or newer for the current development target,
  - Developer Mode enabled,
  - Trust this computer accepted,
  - Administrator privileges,
  - `wintun.dll` available next to the exe or in the DLL search path.
  - Python launcher `py` with `pymobiledevice3` installed for the temporary DVT LocationSimulation backend:
    ```powershell
    py -m pip install --user pymobiledevice3
    ```

The built-in CoreDeviceProxy handshake is working against a USB iPhone and can discover the RSD IPv6 address/port. The Go app now creates the Wintun tunnel and temporarily shells out to `pymobiledevice3 developer dvt simulate-location` to send set/clear location commands through that RSD endpoint. This is a development shortcut until the DVT LocationSimulation client is ported to Go.

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

## iPhone Probe

Use `iosprobe` to test the low-level iPhone path without opening the GUI. The default probe lists USB devices, reads lockdown values, starts CoreDeviceProxy, and verifies the CoreDevice tunnel handshake:

```powershell
go run .\cmd\iosprobe
```

Expected success output includes:

```text
CoreDevice handshake OK: server=<ipv6> rsd=<port> client=<ipv6> mtu=1280
```

To test the same preflight path used by the GUI tunnel manager, run:

```powershell
go run .\cmd\iosprobe -manager
```

To directly send Taipei 101 as a hardware smoke test, run from an elevated PowerShell:

```powershell
go run .\cmd\iosprobe -set101
```

The manager path requires Administrator privileges and `wintun.dll` to be loadable. If it is run from a normal terminal, the expected error is:

```text
Administrator privileges are required to create the tunnel adapter
```
