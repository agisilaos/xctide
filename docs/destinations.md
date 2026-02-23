# Destinations Command

`xctide destinations` lists valid run/build destinations for the resolved scheme.

## Why

Projects can expose many simulator/device targets, and destination IDs can differ by runtime.
This command avoids trial-and-error by showing valid destination specs you can copy directly into `--destination`.

## Usage

```bash
xctide destinations --scheme Subsmind
xctide destinations --scheme Subsmind --json
xctide destinations --scheme Subsmind --simulator-only
xctide destinations --scheme Subsmind --platform iOS
xctide destinations --scheme Subsmind --platform "iOS Simulator" --name "iPhone 17" --latest --limit 10
xctide destinations --scheme Subsmind --os 26.2
xctide destinations --workspace App.xcworkspace --scheme App
```

## Output

Human mode:

- project/workspace + scheme header
- destination list with platform/name/OS (when present)
- copy-ready destination spec line (`platform=...,id=...,name=...`)

JSON mode (`--json`):

- `project` / `workspace`
- `scheme`
- `destinations[]` with: `platform`, `arch`, `id`, `os`, `name`, `spec`

## Notes

- Uses `xcodebuild -showdestinations` under the hood.
- Respects normal config resolution (`--workspace`/`--project`/`--scheme`, env vars, autodetect).
- Optional filters:
  - `--platform` exact platform match (case-insensitive), e.g. `iOS Simulator`
  - `--name` case-insensitive substring match on destination name
  - `--os` case-insensitive substring match on destination OS
  - `--simulator-only`
  - `--device-only`
  - `--latest` keeps the newest OS entry per platform/name combination
  - `--limit` limits returned rows (in human mode, output is compacted to 25 rows by default)
- If multiple schemes exist and `--no-input` is set, provide `--scheme` explicitly.
- On destination mismatch failures during build/run, `xctide` now suggests a ready-to-run `xctide destinations ...` hint.
