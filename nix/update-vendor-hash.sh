#!/usr/bin/env bash
set -euo pipefail

usage() {
  echo "usage: $0 [--check]" >&2
}

mode="update"
if [[ $# -gt 0 ]]; then
  if [[ $# -eq 1 && "$1" == "--check" ]]; then
    mode="check"
  else
    usage
    exit 2
  fi
fi

flake="flake.nix"
if [[ ! -f "$flake" ]]; then
  echo "error: $flake not found in $PWD" >&2
  exit 1
fi

log="$(mktemp)"
trap 'rm -f "$log"' EXIT

if nix build .#default --no-link >"$log" 2>&1; then
  echo "vendorHash is up to date"
  exit 0
fi

got="$(grep -oE 'got:[[:space:]]+sha256-[A-Za-z0-9+/=]+' "$log" | tail -n1 | sed -E 's/got:[[:space:]]+//' || true)"
if [[ -z "$got" ]]; then
  echo "error: nix build failed without a hash mismatch" >&2
  cat "$log" >&2
  exit 1
fi

current="$(grep -oE 'vendorHash = "sha256-[A-Za-z0-9+/=]+"' "$flake" | head -n1 | sed -E 's/.*"(sha256-[^"]+)"/\1/' || true)"
if [[ -z "$current" ]]; then
  echo "error: could not find vendorHash in $flake" >&2
  exit 1
fi

if [[ "$got" == "$current" ]]; then
  echo "error: nix build failed even though vendorHash matches ($got)" >&2
  cat "$log" >&2
  exit 1
fi

if [[ "$mode" == "check" ]]; then
  echo "error: vendorHash is stale" >&2
  echo "  current:  $current" >&2
  echo "  expected: $got" >&2
  exit 1
fi

sed -i.bak -E "s#vendorHash = \"sha256-[A-Za-z0-9+/=]+\"#vendorHash = \"$got\"#" "$flake"
rm -f "$flake.bak"

if ! grep -qF "vendorHash = \"$got\"" "$flake"; then
  echo "error: failed to update vendorHash in $flake" >&2
  exit 1
fi

if ! nix build .#default --no-link >"$log" 2>&1; then
  echo "error: nix build still fails after updating vendorHash to $got" >&2
  cat "$log" >&2
  exit 1
fi

echo "vendorHash updated: $current -> $got"
