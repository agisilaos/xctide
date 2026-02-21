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

for cmd in go git python3; do
  if ! command -v "$cmd" >/dev/null 2>&1; then
    err "required command not found: $cmd"
  fi
done

if ! git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  err "must run inside a git repository"
fi

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

echo "checking go module metadata"
if go help mod tidy 2>/dev/null | grep -Fq -- "-diff"; then
  go mod tidy -diff
else
  before_mod="$(mktemp)"
  before_sum="$(mktemp)"
  had_sum=0
  changed=0
  cp go.mod "$before_mod"
  if [[ -f go.sum ]]; then
    cp go.sum "$before_sum"
    had_sum=1
  fi

  go mod tidy

  if ! diff -u "$before_mod" go.mod >/dev/null; then
    diff -u "$before_mod" go.mod >&2 || true
    changed=1
  fi
  if [[ "$had_sum" -eq 1 ]]; then
    if ! diff -u "$before_sum" go.sum >/dev/null; then
      diff -u "$before_sum" go.sum >&2 || true
      changed=1
    fi
  elif [[ -f go.sum ]]; then
    diff -u /dev/null go.sum >&2 || true
    changed=1
  fi

  cp "$before_mod" go.mod
  if [[ "$had_sum" -eq 1 ]]; then
    cp "$before_sum" go.sum
  else
    rm -f go.sum
  fi
  rm -f "$before_mod" "$before_sum"

  if [[ "$changed" -eq 1 ]]; then
    err "go.mod/go.sum drift detected; run go mod tidy"
  fi
fi

go test ./...

echo "release-check passed for $VERSION"
