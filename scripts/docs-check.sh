#!/usr/bin/env bash
set -euo pipefail

err() {
  echo "error: $*" >&2
  exit 1
}

required_files=(
  "README.md"
  "CHANGELOG.md"
  "Makefile"
  "scripts/release-check.sh"
  "scripts/release.sh"
  "scripts/docs-check.sh"
  ".github/workflows/release-check.yml"
)

for f in "${required_files[@]}"; do
  if [[ ! -f "$f" ]]; then
    err "missing required file: $f"
  fi
done

if grep -qE '^## \[Unreleased\]' CHANGELOG.md; then
  err "CHANGELOG.md must not contain ## [Unreleased]"
fi

if ! grep -q "make release-check VERSION=vX.Y.Z" README.md; then
  err "README.md missing release-check command"
fi

if ! grep -q "make release-dry-run VERSION=vX.Y.Z" README.md; then
  err "README.md missing release-dry-run command"
fi

if ! grep -q "make release VERSION=vX.Y.Z" README.md; then
  err "README.md missing release command"
fi

for target in release-check release-dry-run release; do
  if ! grep -qE "^${target}:" Makefile; then
    err "Makefile missing target: $target"
  fi
done

echo "docs-check passed"
