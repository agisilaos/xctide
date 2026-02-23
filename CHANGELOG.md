# Changelog

## [v0.2.0] - 2026-02-23
- Add `xctide diagnose build` preflight command (doctor + config resolution + plan preview) with JSON and human outputs.
- Improve `destinations` UX with new filters: `--name`, `--os`, `--limit`, `--latest`.
- Improve plain build UX with clearer invocation context, compact summaries by default, optional `--details`, and stronger failure hints.
- Extend machine contract with `schema_version` and monotonic per-event `seq`.
- Normalize/deduplicate `top_errors` and ensure `completed_item.duration_ms` is always present in NDJSON.
- Expand reliability coverage with integration, contract, snapshot, table-driven, and registry-consistency tests.
- Refactor internals for maintainability: command handlers extraction, grouped config options, shared machine-event sequencing.

## [v0.1.2] - 2026-02-22
- Add shell completion subcommand for `bash`, `zsh`, and `fish`.
- Add explicit `xcrun`/`xctest` passthrough docs and integration coverage (including `xctrace`).
- Continue runtime refactor split (CLI surface, event tracker, target timing, reporting) with architecture docs and stronger tests.
- Align release/check scripts and help snapshot docs contract.

## [v0.1.1] - 2026-02-21
- Fix TUI elapsed timer so it freezes at completion instead of continuing to increment.
- Clarify TUI progress denominator by excluding skipped phases from completion count.
- Add tests for progress counting and elapsed freeze behavior.

## [v0.1.0] - 2026-02-20
- Initial release.
