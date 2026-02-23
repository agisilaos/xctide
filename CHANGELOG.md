# Changelog

## [v0.2.1] - 2026-02-23
- Fixed release/version metadata wiring so `--version` is reliably injected via ldflags in release builds.
- Added release checks that assert injected `--version` matches the target tag.

## [v0.2.0] - 2026-02-23
- Added `diagnose` build preflight support with surfaced environment snapshots.
- Added machine-output contract improvements, including explicit `schema_version` and event sequencing metadata.
- Improved destination-selection UX and plain-mode guidance for CLI runs.
- Continued internal command/runtime refactors with expanded parser, integration, and mode-combination test coverage.

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
