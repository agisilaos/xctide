# Machine Output Contract (v1)

This document defines the stable contract for machine consumers of `xctide`.

## Scope

- `--progress json` returns one JSON document.
- `--progress ndjson` streams one JSON event per line.
- Both modes use the same event `type` vocabulary.

## Compatibility Policy

- Existing keys and event types are never removed in a v1.x release.
- New event types or keys may be added; consumers must ignore unknown fields.
- Ordering and required keys in this document are treated as compatibility commitments.

## Event Types

- `run_started`
- `step_started`
- `step_finished`
- `diagnostic`
- `completed_item`
- `diagnostic_summary`
- `action_finished`
- `action_failed`
- `run_finished`

## Required Fields By Event Type

All events include:

- `type` (string)
- `at` (RFC3339 timestamp)

Additional required fields:

- `step_started`: `step_name`, `step_index`, `step_total`
- `step_finished`: `step_name`, `step_index`, `step_total`, `step_status`
- `diagnostic`: `level`, `message`
- `completed_item`: `message`, `duration_ms`
- `diagnostic_summary`: `stats`
- `action_finished`: `message`, `duration_ms`
- `action_failed`: `level`, `message`
- `run_finished`: `success`, `exit_code`, `duration_ms`, `stats`

Optional fields:

- `task_count` on `completed_item`
- `top_errors` on `diagnostic_summary` and `run_finished`

## Ordering Guarantees (`--progress ndjson`)

- Events are emitted in process order as observed during execution.
- `run_finished` is emitted exactly once and is always the final NDJSON event.
- `diagnostic_summary` is emitted before `run_finished`.

## JSON Mode Shape (`--progress json`)

Top-level summary includes:

- `success`, `exit_code`, `duration_ms`
- `command`, `scheme`, `configuration`
- `stats`
- `events[]`
- `phase_timeline[]`
- `completed[]`
- `executed[]` (for `xctide run`)
- `dependency_targets[]` (when dependency/package target timing data is available)
- `top_errors[]`

Consumers should treat `events[]` as the canonical timeline and use summary arrays as convenience projections.

## Golden Fixtures

Contract fixtures are stored in:

- `testdata/contracts/sample.json.golden`
- `testdata/contracts/sample.ndjson.golden`
- `testdata/integration/build-success.log`
- `testdata/integration/build-failure.log`
- `testdata/integration/plain-success.golden`

Tests compare generated payloads against these fixtures:

- `TestContractGoldenJSON`
- `TestContractGoldenNDJSON`
- `TestContractFixtureLock` (acknowledgement guard)
- `TestCLIJSONContractSuccessIntegration`
- `TestCLIJSONContractFailureIntegration`
- `TestCLINDJSONContractIntegration`
- `TestCLIPlainOutputIntegration`

Integration tests execute the built `xctide` binary with a stubbed toolchain (`xcodebuild`, `xcrun`)
injected via `PATH`, so contract checks run deterministically without depending on local Xcode state.

To intentionally refresh fixtures after a contract change:

```bash
UPDATE_GOLDEN=1 go test ./... -run 'TestContractGoldenJSON|TestContractGoldenNDJSON'
UPDATE_CONTRACT_LOCK=1 go test ./... -run TestContractFixtureLock
```

`TestContractFixtureLock` fails when fixture files change without a matching lock update in
`testdata/contracts/LOCK`. This enforces explicit acknowledgement of contract changes.
