# Changelog

## v0.1.8 - 2026-07-28

### Fixed

- Waited for active keepalive and joystick location streams to stop before clearing simulated location.
- Ignored stale location-operation callbacks so they cannot restart keepalive after reset.
- Required device confirmation when stopping location simulation instead of reporting success immediately.

### Changed

- Clarified that restoring location stops simulation first and then waits for iOS to obtain a fresh real-location update.

## v0.1.7 - 2026-07-12

### Changed

- Route speed can now be adjusted while route playback is running; movement continues smoothly from the current position.
- Unified the primary interface in Traditional Chinese and clarified stop, reset, and clear actions.
- Simplified location input into a mode-aware search flow with one primary action for single-point or route playback.
- Preserved route points when switching between single-point and route modes.
- Added visible iPhone and tunnel connection status, common speed presets, and route playback progress.

### Added

- Added undo-last-point actions to the route toolbar and header.
- Added Enter-to-search support for address and coordinate input.

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
