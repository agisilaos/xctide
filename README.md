# xctide

A small TUI wrapper around `xcodebuild` for a cleaner, local build experience.

## Install (local)

```bash
go build -o xctide
```

## Usage

```bash
xctide
xctide build
xctide --scheme "Subsmind" --destination "platform=iOS Simulator,name=iPhone 16"
xctide --plain -- -showBuildSettings
xctide --json -- test
```

## Flags

- `--scheme` (auto-detected if omitted)
- `--workspace` / `--project` (auto-detected if omitted)
- `--configuration` (default: `Debug`)
- `--destination` (optional)
- `--result-bundle` (optional)
- `--quiet` (passes `-quiet` to `xcodebuild`)
- `--verbose` (wrapper diagnostics to stderr)
- `--plain` (disable TUI, stream raw output)
- `--json` (print structured summary to stdout)
- `--no-input` (disable interactive selection prompts)
- `--no-color` (disable color output)
- `--version`

## Exit codes

- `0`: success
- `1`: runtime/internal failure
- `2`: invalid usage
- `3`: config resolution failure (project/workspace/scheme)
- `4`: build/test failure from `xcodebuild`
- `130`: interrupted

## Environment variables

- `XCTIDE_SCHEME`
- `XCTIDE_WORKSPACE`
- `XCTIDE_PROJECT`
- `XCTIDE_CONFIGURATION`
- `XCTIDE_DESTINATION`
- `NO_COLOR`

Precedence: flags > env > auto-detect/defaults.

## Notes

- Pass additional `xcodebuild` args after `--`.
- When stdout/stderr is not a TTY, `xctide` automatically falls back to plain output.

## Release

1. `make release-check VERSION=vX.Y.Z`
2. `make release-dry-run VERSION=vX.Y.Z`
3. `make release VERSION=vX.Y.Z`
