# Diagnose Build

`xctide diagnose build` is a preflight command for build readiness.

It combines:

- `doctor` checks (toolchain/simulator/project context)
- config resolution (`project`/`workspace`/`scheme`)
- resolved command preview (same command that `build` would run)

## Usage

```bash
xctide diagnose build --scheme Subsmind
xctide diagnose build --project App.xcodeproj --scheme App --json
```

## Output

Human mode:

- readiness status (`ready` / `not_ready`)
- doctor pass/warn/fail counts
- resolved plan command
- issues + next steps when not ready

JSON mode (`--json`):

- `ready` (bool)
- `doctor` (`success` + checks array)
- `plan` (when config resolves)
- `issues[]`
- `next_steps[]`

## Exit codes

- `0`: ready
- `1`: not ready (doctor failure or unresolved config)
