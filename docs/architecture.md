# Architecture Overview

This document describes the high-level structure of the `xctide` codebase after the refactor split.

## Module Map

- `main.go`
  - Runtime orchestration and command dispatch.
  - Top-level model/session types and shared build/runtime constants.
- `commands.go`
  - Command-mode handlers for `doctor`, `diagnose build`, `plan`, and `destinations`.
  - Shared stdout/stderr + exit-code wiring for command subflows.
- `contracts_types.go`
  - Shared domain types and machine-contract structures.
  - Exit/version constants and event/build payload definitions.
  - Structured config groups (`projectOptions`, `destinationOptions`, `outputOptions`) embedded in `buildConfig`.
- `cli_surface.go`
  - CLI surface and command helpers.
  - Usage/help output, destinations command helpers, passthrough (`xcrun`/`xctest`), doctor/plan helpers.
- `cli_registry.go`
  - Central registry for subcommands and flags.
  - Shared source for usage text and shell-completion candidates.
- `config_resolve.go`
  - Flag/env/default precedence and progress-mode resolution.
  - Project/workspace/scheme auto-detection and selection prompts.
  - Terminal detection and `xcodebuild -list` JSON decoding helpers.
- `build_modes.go`
  - Build execution paths (`raw`, `plain`, `json`, `ndjson`).
  - Event stream finalization and mode-specific output assembly.
- `event_tracker.go`
  - Build event stream state machine (`run_started`, `step_started`, `step_finished`, diagnostics, `run_finished`).
  - Step transition/duration tracking and summary stats accumulation.
- `machine_events.go`
  - Machine-output annotation utilities for `schema_version` and monotonic `seq`.
  - Shared sequencing logic used by both JSON and NDJSON outputs.
- `target_timing.go`
  - Target timing extraction from build logs.
  - Dependency target filtering/sorting for slow non-primary targets.
  - Conversion helpers for completed timing rows.
- `reporting.go`
  - Human-readable plain report rendering.
  - Timeline helpers, top error extraction, duration formatting, and timing summary parsing.
- `simulator_run.go`
  - Simulator/device destination parsing helpers.
  - Post-build simulator boot/install/launch pipeline.
  - Build settings extraction for run mode.
- `tui_model.go`
  - Bubble Tea model state transitions and event application.
  - Per-phase/test/file target capture helpers.
- `tui_view.go`
  - TUI rendering helpers and layout composition.
  - Shared view formatting utilities (`renderLines`, duration/progress helpers).

## Test Suite Layout

- `main_test.go`: core runtime/config smoke tests.
- `cli_surface_behavior_test.go`: CLI argument normalization, passthrough, destinations, and parsing behavior.
- `cli_parse_table_test.go`: table-driven normalization and progress-mode flag combination coverage.
- `cli_snapshot_test.go`: CLI surface snapshots for root help and completion scripts.
- `cli_registry_contract_test.go`: registry consistency checks ensuring usage/help/completion surfaces stay synchronized with `cliCommands`/`cliFlags`.
- `contract_golden_test.go`: machine contract and plain-output golden fixtures.
- `contract_integration_test.go`: end-to-end CLI contract tests with stubbed toolchain (json/ndjson/plain, completion output, passthrough fidelity, doctor warn/fail paths).
- `reporting_test.go`, `completion_test.go`, `cli_registry_test.go`: focused unit coverage for their modules.

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
