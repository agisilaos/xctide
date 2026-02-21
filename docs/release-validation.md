# Release Validation (2026-02-21)

Release flow was validated on a clean worktree without publishing a release.

## Commands Run

```bash
make release-check VERSION=v0.1.0
make release-dry-run VERSION=v0.1.0
```

## Results

- `release-check`: passed
- `release-dry-run`: passed
- Dry-run artifacts prepared:
  - `dist/xctide_0.1.0_darwin_amd64.tar.gz`
  - `dist/xctide_0.1.0_darwin_arm64.tar.gz`
  - `dist/SHA256SUMS`
- Dry-run confirmed intended actions:
  - create tag `v0.1.0`
  - create GitHub release
  - upload artifacts/checksums
  - update Homebrew formula `xctide.rb`

## Notes

- This validation does **not** publish a release.
- Real release remains gated on final UX polish and release decision.
