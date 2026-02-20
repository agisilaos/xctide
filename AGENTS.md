# AGENTS

## Project purpose
`xctide` is a Go CLI/TUI wrapper around `xcodebuild` for local iOS/macOS build and test workflows.

## Local workflow
- Build: `go build ./...`
- Test: `go test ./...`
- Vet: `go vet ./...`
- Run help: `go run . --help`

## Release workflow
- Run checks first: `make release-check VERSION=vX.Y.Z`
- Exercise end-to-end without remote mutation: `make release-dry-run VERSION=vX.Y.Z`
- Perform release: `make release VERSION=vX.Y.Z`
- Release scripts require:
  - clean git worktree
  - at least one commit
  - `VERSION` matching `vX.Y.Z`
- Optional release env:
  - `HOMEBREW_TAP_REPO`, `HOMEBREW_TAP_BRANCH`, `HOMEBREW_FORMULA_PATH`
  - `GITHUB_REPO`, `HOMEBREW_DESC`, `HOMEBREW_LICENSE`, `RELEASE_LDFLAGS`

## CLI compatibility contract
- Keep root invocation `xctide` working.
- Keep explicit `xctide build` supported.
- Preserve passthrough args after `--` to `xcodebuild`.
- Preserve machine-safe modes (`--plain`, `--json`) for automation.
- Keep diagnostics on stderr and primary output on stdout.

## Editing guidelines
- Prefer additive changes and backwards compatibility for flags.
- Update `README.md` when adding/changing flags, output, or exit codes.
- Run `gofmt` on touched Go files.
- Ensure `go test ./...` and `go vet ./...` pass before finalizing.

## Non-goals
- Do not change xcodebuild semantics beyond wrapping and UX improvements.
- Do not add network-dependent behavior in core build path.
