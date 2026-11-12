#!/usr/bin/env bash
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT="$HERE/../update-vendor-hash.sh"
OLD_HASH='sha256-OLDoldOLDoldOLDoldOLDoldOLDoldOLDoldOLDoldOLD0='
NEW_HASH='sha256-New+hash/with+base64/chars='

TMP_DIRS=()
cleanup() {
  if [[ ${#TMP_DIRS[@]} -gt 0 ]]; then rm -rf "${TMP_DIRS[@]}"; fi
}
trap cleanup EXIT

fail() { echo "FAIL: $*" >&2; exit 1; }
pass() { echo "ok - $*"; }

new_case() {
  CASE_DIR="$(mktemp -d)"
  TMP_DIRS+=("$CASE_DIR")
  mkdir -p "$CASE_DIR/bin"
  cat > "$CASE_DIR/flake.nix" <<EOF
{
  description = "test";
  outputs = { self }: {
    vendorHash = "$OLD_HASH";
  };
}
EOF
  cat > "$CASE_DIR/bin/nix" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
mode="${FAKE_NIX_MODE:?FAKE_NIX_MODE is required}"
flake="$PWD/flake.nix"
case "$mode" in
  ok) exit 0 ;;
  stale)
    if grep -qF 'sha256-New+hash/with+base64/chars=' "$flake"; then exit 0; fi
    echo "error: hash mismatch in fixed-output derivation '/nix/store/test.drv':" >&2
    echo "  specified: sha256-OLDoldOLDoldOLDoldOLDoldOLDoldOLDoldOLDoldOLD0=" >&2
    echo "  got:    sha256-New+hash/with+base64/chars=" >&2
    exit 1
    ;;
  fail)
    echo "error: builder for '/nix/store/test.drv' failed with exit code 1:" >&2
    echo "compile error" >&2
    exit 1
    ;;
  stale-then-fail)
    if grep -qF 'sha256-New+hash/with+base64/chars=' "$flake"; then
      echo "error: builder for '/nix/store/test.drv' failed with exit code 1:" >&2
      echo "compile error after update" >&2
      exit 1
    fi
    echo "error: hash mismatch in fixed-output derivation '/nix/store/test.drv':" >&2
    echo "  specified: sha256-OLDoldOLDoldOLDoldOLDoldOLDoldOLDoldOLDoldOLD0=" >&2
    echo "  got:    sha256-New+hash/with+base64/chars=" >&2
    exit 1
    ;;
  stale-same)
    echo "error: hash mismatch in fixed-output derivation '/nix/store/test.drv':" >&2
    echo "  specified: sha256-OLDoldOLDoldOLDoldOLDoldOLDoldOLDoldOLDoldOLD0=" >&2
    echo "  got:    sha256-OLDoldOLDoldOLDoldOLDoldOLDoldOLDoldOLDoldOLD0=" >&2
    exit 1
    ;;
  *) echo "unknown mode: $mode" >&2; exit 99 ;;
esac
EOF
  chmod +x "$CASE_DIR/bin/nix"
}

run_script() {
  local mode="$1"; shift
  (cd "$CASE_DIR" && FAKE_NIX_MODE="$mode" PATH="$CASE_DIR/bin:$PATH" bash "$SCRIPT" "$@")
}

test_update_rewrites_hash() {
  new_case
  run_script stale >/dev/null
  grep -qF "vendorHash = \"$NEW_HASH\"" "$CASE_DIR/flake.nix" || fail "hash not updated"
  pass "update rewrites vendorHash (base64 +, /, = preserved)"
}

test_check_stale_fails_without_edit() {
  new_case
  if run_script stale --check >"$CASE_DIR/out" 2>&1; then fail "--check should fail"; fi
  grep -qF "$NEW_HASH" "$CASE_DIR/out" || fail "expected hash not reported"
  grep -qF "$OLD_HASH" "$CASE_DIR/flake.nix" || fail "check modified flake.nix"
  pass "--check reports stale hash and leaves file untouched"
}

test_check_ok() {
  new_case
  run_script ok --check >/dev/null
  pass "--check succeeds when hash is current"
}

test_update_ok_is_noop() {
  new_case
  run_script ok >/dev/null
  grep -qF "$OLD_HASH" "$CASE_DIR/flake.nix" || fail "no-op run changed flake.nix"
  pass "update is a no-op when hash is current"
}

test_build_failure_is_reported() {
  new_case
  if run_script fail >"$CASE_DIR/out" 2>&1; then fail "build failure should exit non-zero"; fi
  grep -qF "compile error" "$CASE_DIR/out" || fail "build log not shown"
  grep -qF "$OLD_HASH" "$CASE_DIR/flake.nix" || fail "build failure changed flake.nix"
  pass "non-hash build failure is reported and file is untouched"
}

test_same_hash_build_failure_is_reported() {
  new_case
  if run_script stale-same >/dev/null 2>&1; then fail "build failure with unchanged hash should exit non-zero"; fi
  grep -qF "$OLD_HASH" "$CASE_DIR/flake.nix" || fail "file changed"
  pass "build failure with matching hash is reported"
}

test_failed_rebuild_restores_flake() {
  new_case
  cp "$CASE_DIR/flake.nix" "$CASE_DIR/orig.nix"
  if run_script stale-then-fail >"$CASE_DIR/out" 2>&1; then fail "post-update build failure should exit non-zero"; fi
  cmp -s "$CASE_DIR/orig.nix" "$CASE_DIR/flake.nix" || fail "flake.nix not restored after failed rebuild"
  grep -qF "compile error after update" "$CASE_DIR/out" || fail "rebuild log not shown"
  pass "failed post-update rebuild restores flake.nix"
}

test_missing_vendor_hash_is_reported() {
  new_case
  sed -i.bak 's/.*vendorHash.*/  description = "no hash";/' "$CASE_DIR/flake.nix"
  rm -f "$CASE_DIR/flake.nix.bak"
  if run_script stale >"$CASE_DIR/out" 2>&1; then fail "missing vendorHash should exit non-zero"; fi
  grep -qF "vendorHash" "$CASE_DIR/out" || fail "missing vendorHash not reported"
  pass "missing vendorHash is reported"
}

test_update_rewrites_hash
test_check_stale_fails_without_edit
test_check_ok
test_update_ok_is_noop
test_build_failure_is_reported
test_same_hash_build_failure_is_reported
test_failed_rebuild_restores_flake
test_missing_vendor_hash_is_reported
echo "all tests passed"
