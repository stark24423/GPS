# Changelog

## Unreleased

### Changed

- Route speed can now be adjusted while route playback is running; movement continues smoothly from the current position.

## v0.1.6 - 2026-07-05

### Fixed

- Updated joystick and keyboard movement to refresh the location keepalive target with the latest coordinate, preventing the device from snapping back to an older fixed location.
- Stopped stale location keepalive streams during simulation, reset, stop, and device-switch workflows.

### Changed

- Improved iPhone location flow logging, operation tracking, and timeout handling.
- Added latest-only streaming behavior for manual and route location updates to reduce lag from queued stale points.
- Improved route preview tracking so the UI follows timed playback progress more accurately.

### Added

- Added generated GPX retention handling.
- Added route planning, route speed, device action, and route action test coverage.
