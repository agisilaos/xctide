# Changelog

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
