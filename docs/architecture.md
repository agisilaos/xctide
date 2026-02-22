# Architecture Overview

This document describes the high-level structure of the `xctide` codebase after the refactor split.

## Module Map

- `main.go`
  - Runtime orchestration and command execution flow.
  - Bubble Tea model/update/view plumbing.
  - Build process wiring (`runJSONBuild`, `runNDJSONBuild`, `runProgressPlainBuild`, raw/TUI mode dispatch).
- `cli_surface.go`
  - CLI surface and command helpers.
  - Usage/help output, destinations command helpers, passthrough (`xcrun`/`xctest`), doctor/plan helpers.
  - Env default resolution and progress mode normalization.
- `event_tracker.go`
  - Build event stream state machine (`run_started`, `step_started`, `step_finished`, diagnostics, `run_finished`).
  - Step transition/duration tracking and summary stats accumulation.
- `target_timing.go`
  - Target timing extraction from build logs.
  - Dependency target filtering/sorting for slow non-primary targets.
  - Conversion helpers for completed timing rows.
- `reporting.go`
  - Human-readable plain report rendering.
  - Timeline helpers, top error extraction, duration formatting, and timing summary parsing.

## Why This Split

- Keeps orchestration separate from formatting and parsing concerns.
- Reduces `main.go` cognitive load and merge conflict surface.
- Makes future feature work (new output modes, richer diagnostics, dependency reporting tweaks) easier to test in isolation.

## Core Quality Gates

Run these before any commit touching behavior:

```bash
gofmt -w .
go test ./...
go build ./...
go vet ./...
```

## Refactor Rule of Thumb

For behavior-preserving refactors:

- Move types/functions in cohesive units (state + methods + helpers).
- Keep public/CLI behavior unchanged unless intentionally documented.
- Commit in small, verifiable steps with green tests at each checkpoint.
