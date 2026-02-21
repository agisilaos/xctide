#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

err() {
  echo "error: $*" >&2
  exit 1
}

required_files=(
  "README.md"
  "CHANGELOG.md"
  "Makefile"
  "scripts/docs-contract-check.py"
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

echo "[docs-check] validating shared docs contract"
python3 ./scripts/docs-contract-check.py

for target in release-check release-dry-run release; do
  if ! grep -qE "^${target}:" Makefile; then
    err "Makefile missing target: $target"
  fi
done

echo "[docs-check] validating release command references"
grep -Fq "make release-check VERSION=vX.Y.Z" README.md || err "README missing make release-check usage"
grep -Fq "make release-dry-run VERSION=vX.Y.Z" README.md || err "README missing make release-dry-run usage"
grep -Fq "make release VERSION=vX.Y.Z" README.md || err "README missing make release usage"
grep -Fq "scripts/release-check.sh" README.md || err "README missing scripts/release-check.sh reference"
grep -Fq "scripts/release.sh" README.md || err "README missing scripts/release.sh reference"

echo "docs-check passed"
