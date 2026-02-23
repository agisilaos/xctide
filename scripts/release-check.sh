#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

die() {
  echo "error: $*" >&2
  exit 1
}

if [[ "$(uname -s)" != "Darwin" ]]; then
  die "release-check.sh must be run on macOS (Darwin)"
fi

if [[ $# -ne 1 ]]; then
  echo "usage: scripts/release-check.sh vX.Y.Z" >&2
  exit 2
fi

version="$1"
if [[ ! "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  die "version must match vX.Y.Z (got: $version)"
fi

for tool in go git python3; do
  command -v "$tool" >/dev/null 2>&1 || die "$tool is required"
done

git rev-parse --is-inside-work-tree >/dev/null 2>&1 || die "not inside a git work tree"
git diff --quiet || die "working tree has unstaged changes"
git diff --cached --quiet || die "index has staged changes"

if git rev-parse "$version" >/dev/null 2>&1; then
  die "tag already exists: $version"
fi

[[ -f README.md ]] || die "README.md not found"
[[ -f CHANGELOG.md ]] || die "CHANGELOG.md not found"

if grep -qE '^## \[Unreleased\]' CHANGELOG.md; then
  die "CHANGELOG.md must not contain ## [Unreleased]"
fi

if grep -Fq "## [$version]" CHANGELOG.md; then
  die "CHANGELOG.md already contains $version"
fi

# Keep release-check CI portable on stock GitHub runners.
# Do not require non-default tooling such as rg/jq/yq/fd in checked scripts.
if grep -R -nE '(^|[[:space:]])(r[g]|j[q]|y[q]|f[d])([[:space:]]|$)' scripts >/dev/null; then
  die "scripts/ uses non-portable tooling (rg/jq/yq/fd). Use grep/sed/awk or install tools explicitly in workflow."
fi

echo "[release-check] running tests"
go test ./...

echo "[release-check] running vet"
go vet ./...

echo "[release-check] running docs check"
./scripts/docs-check.sh

echo "[release-check] checking go module metadata"
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
    die "go.mod/go.sum drift detected; run go mod tidy"
  fi
fi

echo "[release-check] checking format"
if [[ -n "$(gofmt -l .)" ]]; then
  die "gofmt reported formatting drift"
fi

echo "[release-check] verifying --version output wiring"
build_pkg="./cmd/xctide"
if [[ ! -d "$build_pkg" ]]; then
  build_pkg="."
fi
tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT
bin_path="$tmp_dir/xctide-release-check"
go build -ldflags "-X main.version=${version}" -o "$bin_path" "$build_pkg"
reported_version="$("$bin_path" --version | tr -d '\r\n')"
expected_version="${version#v}"
if [[ "$reported_version" != "$expected_version" ]]; then
  die "--version mismatch: got '$reported_version', want '$expected_version'"
fi

echo "[release-check] ok"
echo "  version: $version"
