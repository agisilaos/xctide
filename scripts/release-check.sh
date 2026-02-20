#!/usr/bin/env bash
set -euo pipefail

VERSION="${1:-}"

err() {
  echo "error: $*" >&2
  exit 1
}

if [[ -z "$VERSION" ]]; then
  err "usage: ./scripts/release-check.sh vX.Y.Z"
fi

if [[ ! "$VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  err "invalid VERSION '$VERSION' (expected vX.Y.Z)"
fi

for cmd in go git; do
  if ! command -v "$cmd" >/dev/null 2>&1; then
    err "required command not found: $cmd"
  fi
done

if [[ -n "$(git status --porcelain)" ]]; then
  err "working tree is not clean"
fi

if [[ ! -f CHANGELOG.md ]]; then
  err "CHANGELOG.md is missing"
fi

if grep -qE '^## \[Unreleased\]' CHANGELOG.md; then
  err "CHANGELOG.md must not contain ## [Unreleased]"
fi

if ! grep -qE "^## \[$VERSION\]" CHANGELOG.md; then
  echo "warning: CHANGELOG.md does not contain heading for $VERSION" >&2
fi

# Keep release-check CI portable on stock GitHub runners.
# Do not require non-default tooling such as rg/jq/yq/fd in checked scripts.
if grep -R -nE '(^|[[:space:]])(r[g]|j[q]|y[q]|f[d])([[:space:]]|$)' scripts >/dev/null; then
  err "scripts/ uses non-portable tooling (rg/jq/yq/fd). Use grep/sed/awk or install tools explicitly in workflow."
fi

./scripts/docs-check.sh
go test ./...

echo "release-check passed for $VERSION"
