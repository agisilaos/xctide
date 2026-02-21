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
xctide run --destination "platform=iOS Simulator,id=<UDID>"
xctide plan --scheme Subsmind -- test
xctide doctor
xctide destinations --scheme Subsmind
xctide xcrun simctl list devices available
xctide xctest -h
xctide --scheme "Subsmind" --destination "platform=iOS Simulator,name=iPhone 16"
xctide --plain -- -showBuildSettings
xctide --progress plain -- test
xctide --progress json -- test
xctide --progress ndjson -- test
xctide --json -- test
```

## Flags

- `--scheme` (auto-detected if omitted)
- `--workspace` / `--project` (auto-detected if omitted)
- `--configuration` (default: `Debug`)
- `--destination` (optional)
- `--platform` (destination filter for `xctide destinations`)
- `--simulator-only` / `--device-only` (destination filters for `xctide destinations`)
- `--progress` (`auto|tui|plain|json|ndjson`; default `auto`)
- `--result-bundle` (optional)
- `--quiet` (passes `-quiet` to `xcodebuild`)
- `--verbose` (wrapper diagnostics to stderr)
- `--plain` (disable TUI, stream raw output)
- `--json` (print structured summary to stdout)
- `--no-input` (disable interactive selection prompts)
- `--no-color` (disable color output)
- `--version`

## Subcommands

- `build`: default behavior, runs `xcodebuild` with resolved config.
- `run`: build then launch app on simulator (requires simulator `id=` destination).
- `plan`: resolve config and print exact `xcodebuild` command without executing.
- `doctor`: validate local build prerequisites (`xcodebuild`, `xcrun`, simulators, project context).
- `destinations`: list valid `xcodebuild` destinations for the resolved scheme.
- `xcrun`: explicit passthrough to `xcrun` for simulator/runtime tooling flows.
- `xctest`: explicit passthrough to `xcrun xctest` for direct test runner use.

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
- `XCTIDE_PROGRESS`
- `NO_COLOR`

Precedence: flags > env > auto-detect/defaults.

## Notes

- Pass additional `xcodebuild` args after `--`.
- When stdout/stderr is not a TTY, `xctide` automatically falls back to plain output.
- `xctide run` performs build + simulator launch + install + app launch (requires simulator destination with `id=`).
- Preflight docs for `doctor` and `plan`: `docs/doctor-and-plan.md`.
- Tool passthrough docs for `xcrun` and `xctest`: `docs/tooling-passthrough.md`.
- Destination discovery docs: `docs/destinations.md`.
- Real-machine validation matrix notes: `docs/validation-matrix.md`.
- Release check and dry-run results: `docs/release-validation.md`.

## Progress Event Model (v1)

`xctide` emits one internal event stream used by all progress renderers (`tui`, `plain`, `json`):

- `run_started`
- `step_started`
- `step_finished` (`done`, `failed`, `skipped`)
- `diagnostic` (`warning`, `error`)
- `completed_item`
- `diagnostic_summary`
- `action_finished` / `action_failed`
- `run_finished`

In `--progress json`, events are returned in `events[]`, phase order in `phase_timeline`, completion rows in `completed[]`, execution rows in `executed[]`, and summary errors in `top_errors`.
In `--progress ndjson`, each event is emitted as one JSON object per line in real time (including `completed_item` and `diagnostic_summary`), with `run_finished` emitted last.
Formal contract: `docs/machine-contract.md`.

When available, plain output includes a `Dependencies` section with slow non-primary build targets (typically SPM/package targets), and JSON includes `dependency_targets[]`.

## Release

1. `make release-check VERSION=vX.Y.Z`
2. `make release-dry-run VERSION=vX.Y.Z`
3. `make release VERSION=vX.Y.Z`

Release readiness checklist: `docs/release-readiness.md`
