# Release Readiness Checklist

Use this checklist before cutting a release.

## Reliability

- [ ] Run `xctide doctor` and resolve all failing checks.
- [ ] Validate `xctide plan` matches expected `xcodebuild` invocation before real builds.
- [ ] Validate `xctide build` on at least 3 real projects.
- [ ] Validate `xctide run` on simulator (booted and shutdown states).
- [ ] Validate `--progress plain|json|ndjson` produce consistent success/failure semantics.
- [ ] Confirm no hangs and correct exit codes (`0`, `1`, `2`, `3`, `4`, `130`).

## Machine Contract

- [ ] Freeze JSON/NDJSON event schema for this release.
- [ ] Add/update schema and golden tests for `events`, `completed`, `executed`, `top_errors`.
- [ ] Document compatibility policy for machine-readable modes.

## UX

- [ ] Confirm plain report is readable for success and failure paths.
- [ ] Keep plain output golden fixtures current (`testdata/plain/*.golden`).
- [ ] Confirm destination details are human-friendly (device/simulator + OS).
- [ ] Confirm failure summaries highlight top actionable errors.

## Quality Gates

- [ ] `go test ./...`
- [ ] `go build ./...`
- [ ] `go vet ./...`
- [ ] `make release-check VERSION=vX.Y.Z` on clean worktree.
- [ ] `make release-dry-run VERSION=vX.Y.Z` on clean worktree.

## Distribution

- [ ] Verify GitHub release flow with artifacts and checksums.
- [ ] Verify Homebrew formula update in dry-run and real path.

## Suggested Versioning

- `v0.1.0`: feature-complete beta with explicit machine contract caveats.
- `v1.0.0`: only after machine contracts are declared stable.
