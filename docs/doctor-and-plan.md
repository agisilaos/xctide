# Doctor And Plan Commands

`xctide` now includes two preflight commands for safer local and agentic workflows.

## `xctide doctor`

Purpose: validate machine prerequisites before running builds.

Checks:

- `xcodebuild` exists and responds to `-version`
- `xcrun` exists and responds to `--version`
- simulator availability via `xcrun simctl list devices available -j`
- project context (`.xcworkspace` / `.xcodeproj` in current directory, or explicit flags/env)

Output:

- human summary by default
- `--json` for machine consumption

Exit behavior:

- exits `0` when no failing checks are found
- exits `1` when at least one check has `status=fail`
- warning checks do not fail the command

## `xctide plan`

Purpose: preview exactly what `xctide` will run, without executing a build.

Behavior:

- resolves workspace/project/scheme/configuration using normal precedence
- prints resolved configuration and full `xcodebuild` command
- supports `--json` for machine consumers

Use cases:

- CI/agent preflight before expensive builds
- debugging config resolution issues
- reproducible command previews in docs/issues
