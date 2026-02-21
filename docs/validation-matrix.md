# Validation Matrix (2026-02-21)

This document captures real-machine validation runs executed on macOS with local projects.

## Scope

- Validate `doctor`/`plan`/`build` across at least 3 real projects.
- Validate `run` on simulator for shutdown and booted paths.

## Projects Used

- `/Users/agis/projects/Subsmind`
- `/Users/agis/projects/Chrono`
- `/Users/agis/projects/CalmCompass`

## Simulator IDs Used

- Initial attempt (invalid for Subsmind destinations): `39C03BA8-2C40-4E4B-8FF9-5EACA911E66A`
- Valid Subsmind run destination: `973281EF-824E-43BB-915F-DBD755A1291A` (`iPhone 17 Pro`, iOS 26.2)

## Results

### Subsmind

- `doctor`: pass
- `plan --no-input`: failed due multiple schemes (`Subsmind`, `Subsmind - dev`) without explicit `--scheme` (expected)
- `build --scheme Subsmind` on valid simulator ID: pass
- `run --scheme Subsmind` on valid simulator ID:
  - shutdown path: pass (build succeeded)
  - booted path: pass (launch simulator, install app, launch app)

### Chrono

- `doctor`: pass
- `plan --no-input`: pass
- `build` with selected simulator ID: failed destination match (project/runtime mismatch for that ID)

### CalmCompass

- `doctor`: pass
- `plan --no-input`: initially failed due `xcodebuild -list -json` prefixed non-JSON output
- Improvement implemented: robust extraction of JSON object from noisy `xcodebuild` output

## Product Feedback

- Good:
  - `doctor` quickly surfaces machine readiness.
  - `run` execution rows are clear and useful.
  - explicit `--scheme` requirement under `--no-input` is correct and predictable.
- Improve next:
  - destination auto-suggestion when provided simulator ID is invalid for selected scheme.
  - optional `xctide destinations` command to list valid destinations directly from `xcodebuild -showdestinations`.
