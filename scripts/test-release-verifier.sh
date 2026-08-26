#!/usr/bin/env bash
# Offline black-box contract for the post-publish release verifier.
set -uo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
VERIFIER="$ROOT/scripts/verify-release.sh"
WORKFLOW="$ROOT/.github/workflows/release.yml"
MAKEFILE="$ROOT/Makefile"
CI="$ROOT/.github/workflows/ci.yml"
failures=0
checks=0

pass() { checks=$((checks + 1)); }
fail() { checks=$((checks + 1)); failures=$((failures + 1)); printf 'release verifier: %s\n' "$1" >&2; }

contains() {
  local file="$1" text="$2" label="$3"
  if [ -f "$file" ] && grep -Fq -- "$text" "$file"; then pass; else fail "$label"; fi
}

excludes() {
  local file="$1" pattern="$2" label="$3"
  if [ -f "$file" ] && ! grep -Eq -- "$pattern" "$file"; then pass; else fail "$label"; fi
}

if [ -f "$VERIFIER" ]; then pass; else fail "scripts/verify-release.sh is missing"; fi
contains "$VERIFIER" '#!/usr/bin/env bash' "verifier is not explicitly Bash"
contains "$VERIFIER" 'set -euo pipefail' "verifier does not fail closed"
contains "$VERIFIER" 'github.com/onembyte/kolkrabbi/releases' "release origin drifted"
contains "$VERIFIER" 'checksums.txt.sigstore.json' "Sigstore bundle name drifted"
contains "$VERIFIER" 'cosign verify-blob' "verifier never authenticates the checksum manifest"
contains "$VERIFIER" 'https://token.actions.githubusercontent.com' "GitHub OIDC issuer is not pinned"
contains "$VERIFIER" '.github/workflows/release.yml@refs/tags/' "release workflow identity is not pinned"
contains "$VERIFIER" 'main "$@"' "verifier has no final execution sentinel"
excludes "$VERIFIER" 'certificate-identity-regexp|certificate-oidc-issuer-regexp|insecure-ignore' "verifier weakens exact identity or transparency checks"
# The single quotes preserve the literal workflow expression required by this contract.
# shellcheck disable=SC2016
contains "$WORKFLOW" 'run: ./scripts/verify-release.sh "$GITHUB_REF_NAME"' "release workflow does not verify published assets"
contains "$MAKEFILE" 'release-verifier-check:' "Makefile has no release-verifier-check target"
contains "$CI" 'run: make release-verifier-check' "ordinary CI does not enforce the verifier contract"

if [ ! -f "$VERIFIER" ]; then
  fail "black-box matrix cannot run without the verifier"
  printf 'release verifier: %d/%d checks failed\n' "$failures" "$checks" >&2
  exit 1
fi

if bash -n "$VERIFIER"; then pass; else fail "verifier fails bash -n"; fi
if [ "$(tail -n 1 "$VERIFIER")" = 'main "$@"' ]; then pass; else fail "main call is not the final line"; fi

WORK="$(mktemp -d "${TMPDIR:-/tmp}/kolk-release-verifier-test.XXXXXX")"
trap 'rm -rf "$WORK"' EXIT
RELEASE="$WORK/release"
FAKEBIN="$WORK/fakebin"
mkdir -p "$RELEASE" "$FAKEBIN"
VERSION=1.1.13
TAG="v$VERSION"

host_os="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$(uname -m)" in
  x86_64|amd64) host_arch=amd64 ;;
  arm64|aarch64) host_arch=arm64 ;;
  *) host_arch=unsupported ;;
esac

for target in darwin_amd64 darwin_arm64 linux_amd64 linux_arm64; do
  payload="$WORK/payload-$target"
  mkdir -p "$payload"
  cat >"$payload/kolk" <<FIXTURE
#!/usr/bin/env bash
if [ "\${1:-}" = version ]; then
  printf 'kolk $VERSION (abc123, 2026-08-23T00:00:00Z) go1.26.4 ${target/_//}\\n'
else
  exit 2
fi
FIXTURE
  chmod 0755 "$payload/kolk"
  printf 'fixture readme\n' >"$payload/README.md"
  printf 'fixture license\n' >"$payload/LICENSE"
  tar -czf "$RELEASE/kolk_${VERSION}_${target}.tar.gz" -C "$payload" kolk README.md LICENSE
done

: >"$RELEASE/checksums.txt"
for archive in "$RELEASE"/*.tar.gz; do
  shasum -a 256 "$archive" | awk '{print $1 "  " name}' name="$(basename "$archive")" >>"$RELEASE/checksums.txt"
done
printf '{"fixture":"sigstore bundle"}\n' >"$RELEASE/checksums.txt.sigstore.json"

cat >"$FAKEBIN/curl" <<'FAKE_CURL'
#!/usr/bin/env bash
set -eu
output=""
url=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    -o) output="$2"; shift 2 ;;
    --proto|--retry|--retry-delay|--connect-timeout) shift 2 ;;
    --tlsv1.2|-f|-s|-S|-L|-fsSL) shift ;;
    *) url="$1"; shift ;;
  esac
done
name="${url##*/}"
printf 'curl %s\n' "$name" >>"${FAKE_EVENT_LOG:?}"
[ -n "$output" ]
/bin/cp "${FAKE_RELEASE_DIR:?}/$name" "$output"
FAKE_CURL

cat >"$FAKEBIN/cosign" <<'FAKE_COSIGN'
#!/usr/bin/env bash
set -eu
printf 'cosign\n' >>"${FAKE_EVENT_LOG:?}"
[ "${1:-}" = verify-blob ]
shift
bundle=""
identity=""
issuer=""
blob=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --bundle) bundle="$2"; shift 2 ;;
    --certificate-identity) identity="$2"; shift 2 ;;
    --certificate-oidc-issuer) issuer="$2"; shift 2 ;;
    *) blob="$1"; shift ;;
  esac
done
[ -f "$bundle" ]
[ -f "$blob" ]
[ "$identity" = "${FAKE_EXPECT_IDENTITY:?}" ]
[ "$issuer" = 'https://token.actions.githubusercontent.com' ]
[ "${FAKE_COSIGN_FAIL:-0}" -eq 0 ]
FAKE_COSIGN
chmod 0755 "$FAKEBIN/curl" "$FAKEBIN/cosign"

run_verifier() {
  local release_dir="$1" event_log="$2"
  shift 2
  PATH="$FAKEBIN:/usr/bin:/bin" \
    FAKE_RELEASE_DIR="$release_dir" \
    FAKE_EVENT_LOG="$event_log" \
    FAKE_EXPECT_IDENTITY="https://github.com/onembyte/kolkrabbi/.github/workflows/release.yml@refs/tags/$TAG" \
    "$@" "$VERIFIER" "$TAG"
}

events="$WORK/events-success.log"
if run_verifier "$RELEASE" "$events" env >"$WORK/out-success" 2>"$WORK/err-success"; then
  pass
else
  fail "valid release failed: $(<"$WORK/err-success")"
fi
if grep -Fq "release $TAG verified" "$WORK/out-success"; then pass; else fail "success output does not name the verified tag"; fi
if [ "$(sed -n '1p' "$events")" = 'curl checksums.txt' ] &&
   [ "$(sed -n '2p' "$events")" = 'curl checksums.txt.sigstore.json' ] &&
   [ "$(sed -n '3p' "$events")" = 'cosign' ]; then
  pass
else
  fail "signature was not verified before archive downloads"
fi
if [ "$(grep -c '^curl kolk_.*\.tar\.gz$' "$events")" -eq 4 ]; then pass; else fail "verifier did not download exactly four archives"; fi

invalid_events="$WORK/events-invalid-tag.log"
if PATH="$FAKEBIN:/usr/bin:/bin" FAKE_EVENT_LOG="$invalid_events" FAKE_RELEASE_DIR="$RELEASE" \
  "$VERIFIER" proto-0 >/dev/null 2>"$WORK/err-invalid-tag"; then
  fail "invalid tag was accepted"
else
  pass
fi
if [ ! -s "$invalid_events" ]; then pass; else fail "invalid tag contacted the release origin"; fi

signature_events="$WORK/events-bad-signature.log"
if run_verifier "$RELEASE" "$signature_events" env FAKE_COSIGN_FAIL=1 >/dev/null 2>"$WORK/err-bad-signature"; then
  fail "invalid signature was accepted"
else
  pass
fi
if [ "$(grep -c '^curl ' "$signature_events")" -eq 2 ]; then pass; else fail "signature failure downloaded archives"; fi

bad_checksum="$WORK/release-bad-checksum"
/bin/cp -R "$RELEASE" "$bad_checksum"
printf 'tampered\n' >>"$bad_checksum/kolk_${VERSION}_darwin_arm64.tar.gz"
if run_verifier "$bad_checksum" "$WORK/events-bad-checksum.log" env >/dev/null 2>"$WORK/err-bad-checksum"; then
  fail "checksum mismatch was accepted"
else
  pass
fi

bad_manifest="$WORK/release-bad-manifest"
/bin/cp -R "$RELEASE" "$bad_manifest"
printf '%064d  surprise.tar.gz\n' 0 >>"$bad_manifest/checksums.txt"
if run_verifier "$bad_manifest" "$WORK/events-bad-manifest.log" env >/dev/null 2>"$WORK/err-bad-manifest"; then
  fail "manifest with an unknown fifth asset was accepted"
else
  pass
fi

duplicate_manifest="$WORK/release-duplicate-manifest"
/bin/cp -R "$RELEASE" "$duplicate_manifest"
sed -n '1p;1p;2p;3p' "$RELEASE/checksums.txt" >"$duplicate_manifest/checksums.txt"
if run_verifier "$duplicate_manifest" "$WORK/events-duplicate-manifest.log" env >/dev/null 2>"$WORK/err-duplicate-manifest"; then
  fail "manifest with a duplicate and missing asset was accepted"
else
  pass
fi

malformed_manifest="$WORK/release-malformed-manifest"
/bin/cp -R "$RELEASE" "$malformed_manifest"
awk 'NR == 1 {$1 = "not-a-sha256"} {print}' "$RELEASE/checksums.txt" >"$malformed_manifest/checksums.txt"
if run_verifier "$malformed_manifest" "$WORK/events-malformed-manifest.log" env >/dev/null 2>"$WORK/err-malformed-manifest"; then
  fail "manifest with a malformed digest was accepted"
else
  pass
fi

missing_asset="$WORK/release-missing-asset"
/bin/cp -R "$RELEASE" "$missing_asset"
rm "$missing_asset/kolk_${VERSION}_linux_arm64.tar.gz"
if run_verifier "$missing_asset" "$WORK/events-missing-asset.log" env >/dev/null 2>"$WORK/err-missing-asset"; then
  fail "missing release archive was accepted"
else
  pass
fi

bad_archive="$WORK/release-bad-archive"
/bin/cp -R "$RELEASE" "$bad_archive"
bad_payload="$WORK/bad-archive-payload"
mkdir -p "$bad_payload"
/bin/cp "$WORK/payload-linux_arm64/kolk" "$bad_payload/kolk"
printf 'fixture readme\n' >"$bad_payload/README.md"
printf 'fixture license\n' >"$bad_payload/LICENSE"
printf 'unexpected member\n' >"$bad_payload/surprise.txt"
tar -czf "$bad_archive/kolk_${VERSION}_linux_arm64.tar.gz" -C "$bad_payload" kolk README.md LICENSE surprise.txt
: >"$bad_archive/checksums.txt"
for archive in "$bad_archive"/*.tar.gz; do
  shasum -a 256 "$archive" | awk '{print $1 "  " name}' name="$(basename "$archive")" >>"$bad_archive/checksums.txt"
done
if run_verifier "$bad_archive" "$WORK/events-bad-archive.log" env >/dev/null 2>"$WORK/err-bad-archive"; then
  fail "signed archive with an unexpected member was accepted"
else
  pass
fi

if { [ "$host_os" = darwin ] || [ "$host_os" = linux ]; } && [ "$host_arch" != unsupported ]; then
  wrong_identity="$WORK/release-wrong-build"
  /bin/cp -R "$RELEASE" "$wrong_identity"
  wrong_payload="$WORK/wrong-payload"
  mkdir -p "$wrong_payload"
  printf '#!/usr/bin/env bash\nprintf "kolk dev go1.26.4 %s/%s\\n"\n' "$host_os" "$host_arch" >"$wrong_payload/kolk"
  chmod 0755 "$wrong_payload/kolk"
  printf 'fixture readme\n' >"$wrong_payload/README.md"
  printf 'fixture license\n' >"$wrong_payload/LICENSE"
  host_name="kolk_${VERSION}_${host_os}_${host_arch}.tar.gz"
  tar -czf "$wrong_identity/$host_name" -C "$wrong_payload" kolk README.md LICENSE
  : >"$wrong_identity/checksums.txt"
  for archive in "$wrong_identity"/*.tar.gz; do
    shasum -a 256 "$archive" | awk '{print $1 "  " name}' name="$(basename "$archive")" >>"$wrong_identity/checksums.txt"
  done
  if run_verifier "$wrong_identity" "$WORK/events-wrong-build.log" env >/dev/null 2>"$WORK/err-wrong-build"; then
    fail "unstamped host binary was accepted"
  else
    pass
  fi
fi

if [ "$failures" -ne 0 ]; then
  printf 'release verifier: %d/%d checks failed\n' "$failures" "$checks" >&2
  exit 1
fi
printf 'release verifier: %d checks passed\n' "$checks"
