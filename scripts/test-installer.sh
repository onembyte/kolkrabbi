#!/usr/bin/env bash
# Offline black-box contract for site/install.sh. No network and no sudo.
set -uo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
INSTALLER="$ROOT/site/install.sh"
MAKEFILE="$ROOT/Makefile"
CI="$ROOT/.github/workflows/ci.yml"
failures=0
checks=0

pass() { checks=$((checks + 1)); }
fail() { checks=$((checks + 1)); failures=$((failures + 1)); printf 'installer: %s\n' "$1" >&2; }

contains() {
  local text="$1" label="$2"
  if [ -f "$INSTALLER" ] && grep -Fq -- "$text" "$INSTALLER"; then pass; else fail "$label"; fi
}

excludes() {
  local pattern="$1" label="$2"
  if [ -f "$INSTALLER" ] && ! grep -Eq -- "$pattern" "$INSTALLER"; then pass; else fail "$label"; fi
}

if [ -f "$INSTALLER" ]; then pass; else fail "site/install.sh is missing"; fi
contains '#!/usr/bin/env bash' "installer is not explicitly Bash"
contains 'set -euo pipefail' "installer does not fail closed"
contains 'github.com/onembyte/kolkrabbi/releases' "release origin drifted"
contains 'KOLK_VERSION' "pinned-version override is missing"
contains 'KOLK_INSTALL_DIR' "explicit install directory is missing"
contains 'checksums.txt' "installer never downloads the checksum manifest"
contains 'shasum -a 256' "portable macOS SHA-256 path is missing"
contains 'sha256sum' "Linux SHA-256 path is missing"
contains 'mktemp -d' "installer has no private staging directory"
contains 'main "$@"' "installer has no end-of-file execution sentinel"
excludes 'eval|curl[^\n]*\|[^\n]*(sh|bash)' "installer contains dynamic evaluation or a nested curl pipe"
if grep -Fq 'installer:' "$MAKEFILE"; then pass; else fail "Makefile has no installer target"; fi
if grep -Fq 'run: make installer' "$CI"; then pass; else fail "CI does not enforce the installer contract"; fi

if [ ! -f "$INSTALLER" ]; then
  fail "black-box matrix cannot run without the installer"
  printf 'installer: %d/%d checks failed\n' "$failures" "$checks" >&2
  exit 1
fi

if bash -n "$INSTALLER"; then pass; else fail "installer fails bash -n"; fi
if [ "$(tail -n 1 "$INSTALLER")" = 'main "$@"' ]; then
  pass
else
  fail "main call is not the final line; a truncated curl stream could execute partially"
fi

WORK="$(mktemp -d "${TMPDIR:-/tmp}/kolk-installer-test.XXXXXX")"
trap 'rm -rf "$WORK"' EXIT
RELEASES="$WORK/releases"
FAKEBIN="$WORK/fakebin"
HOME_DIR="$WORK/home"
STAGE_ROOT="$WORK/stage"
mkdir -p "$RELEASES" "$FAKEBIN" "$HOME_DIR" "$STAGE_ROOT"

VERSION=1.1.13
for target in darwin_amd64 darwin_arm64 linux_amd64 linux_arm64; do
  payload="$WORK/payload-$target"
  mkdir -p "$payload"
  printf '#!/usr/bin/env bash\nprintf "kolk %s test %s\\n"\n' "$VERSION" "$target" >"$payload/kolk"
  chmod 0755 "$payload/kolk"
  printf 'fixture readme\n' >"$payload/README.md"
  printf 'fixture license\n' >"$payload/LICENSE"
  tar -czf "$RELEASES/kolk_${VERSION}_${target}.tar.gz" -C "$payload" kolk README.md LICENSE
done

: >"$RELEASES/checksums.txt"
for archive in "$RELEASES"/*.tar.gz; do
  shasum -a 256 "$archive" | awk '{print $1 "  " name}' name="$(basename "$archive")" >>"$RELEASES/checksums.txt"
done

cat >"$FAKEBIN/uname" <<'FAKE_UNAME'
#!/usr/bin/env bash
case "${1:-}" in
  -s) printf '%s\n' "${FAKE_UNAME_S:?}" ;;
  -m) printf '%s\n' "${FAKE_UNAME_M:?}" ;;
  *) exit 2 ;;
esac
FAKE_UNAME

cat >"$FAKEBIN/curl" <<'FAKE_CURL'
#!/usr/bin/env bash
set -eu
output=""
write_format=""
url=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    -o) output="$2"; shift 2 ;;
    -w) write_format="$2"; shift 2 ;;
    --proto|--retry|--retry-delay|--connect-timeout) shift 2 ;;
    --tlsv1.2|-f|-s|-S|-L|-I|-fsSL|-fsSIL) shift ;;
    *) url="$1"; shift ;;
  esac
done
printf '%s\n' "$url" >>"${FAKE_CURL_LOG:?}"
if [ -n "${FAKE_CURL_FAIL_MATCH:-}" ] && [[ "$url" == *"$FAKE_CURL_FAIL_MATCH"* ]]; then
  exit 22
fi
if [ -n "$write_format" ]; then
  printf '%s' "${FAKE_LATEST_URL:?}"
  exit 0
fi
name="${url##*/}"
[ -n "$output" ]
/bin/cp "${FAKE_RELEASE_DIR:?}/$name" "$output"
FAKE_CURL

chmod 0755 "$FAKEBIN/uname" "$FAKEBIN/curl"
SYSTEM_PATH="/usr/bin:/bin"

run_installer() {
  local os_name="$1" machine="$2" install_dir="$3" log="$4" release_dir="$5"
  shift 5
  PATH="$FAKEBIN:$SYSTEM_PATH" \
    HOME="$HOME_DIR" \
    TMPDIR="$STAGE_ROOT" \
    FAKE_UNAME_S="$os_name" \
    FAKE_UNAME_M="$machine" \
    FAKE_CURL_LOG="$log" \
    FAKE_RELEASE_DIR="$release_dir" \
    FAKE_LATEST_URL="https://github.com/onembyte/kolkrabbi/releases/tag/v$VERSION" \
    KOLK_INSTALL_DIR="$install_dir" \
    "$@" bash "$INSTALLER"
}

for spec in 'Darwin x86_64 darwin_amd64' 'Darwin arm64 darwin_arm64' 'Linux x86_64 linux_amd64' 'Linux aarch64 linux_arm64'; do
  read -r os_name machine target <<<"$spec"
  install_dir="$WORK/install-$target"
  log="$WORK/curl-$target.log"
  if run_installer "$os_name" "$machine" "$install_dir" "$log" "$RELEASES" env KOLK_VERSION="v$VERSION" >"$WORK/out-$target" 2>"$WORK/err-$target"; then
    pass
  else
    fail "$target install failed: $(<"$WORK/err-$target")"
  fi
  if [ -x "$install_dir/kolk" ]; then pass; else fail "$target did not install an executable"; fi
  if grep -Fq "kolk_${VERSION}_${target}.tar.gz" "$log"; then pass; else fail "$target requested the wrong archive"; fi
  if grep -Fq '/checksums.txt' "$log"; then pass; else fail "$target did not request checksums.txt"; fi
done

latest_dir="$WORK/install-latest"
latest_log="$WORK/curl-latest.log"
if run_installer Darwin arm64 "$latest_dir" "$latest_log" "$RELEASES" env -u KOLK_VERSION >"$WORK/out-latest" 2>"$WORK/err-latest"; then
  pass
else
  fail "latest-version discovery failed: $(<"$WORK/err-latest")"
fi
if grep -Fq '/releases/latest' "$latest_log"; then pass; else fail "latest release endpoint was not consulted"; fi

default_bin="$HOME_DIR/.local/bin"
mkdir -p "$default_bin"
default_log="$WORK/curl-default-path.log"
if PATH="$FAKEBIN:$default_bin:$SYSTEM_PATH" \
  HOME="$HOME_DIR" \
  TMPDIR="$STAGE_ROOT" \
  FAKE_UNAME_S=Darwin \
  FAKE_UNAME_M=arm64 \
  FAKE_CURL_LOG="$default_log" \
  FAKE_RELEASE_DIR="$RELEASES" \
  FAKE_LATEST_URL="https://github.com/onembyte/kolkrabbi/releases/tag/v$VERSION" \
  KOLK_VERSION="v$VERSION" \
  env -u KOLK_INSTALL_DIR bash "$INSTALLER" >"$WORK/out-default-path" 2>"$WORK/err-default-path"; then
  pass
else
  fail "default PATH install failed: $(<"$WORK/err-default-path")"
fi
if [ -x "$default_bin/kolk" ]; then pass; else fail "installer did not choose the writable user PATH directory"; fi

write_versioned_kolk() {
  local path="$1" version="$2"
  mkdir -p "${path%/*}"
  printf '#!/usr/bin/env bash\nprintf "kolk %s test existing\\n"\n' "$version" >"$path"
  chmod 0755 "$path"
}

older_dir="$WORK/install-older"
older_version=1.1.2
write_versioned_kolk "$older_dir/kolk" "$older_version"
older_log="$WORK/curl-older.log"
if run_installer Darwin arm64 "$older_dir" "$older_log" "$RELEASES" env -u KOLK_VERSION >"$WORK/out-older" 2>"$WORK/err-older"; then
  pass
else
  fail "older-version upgrade failed: $(<"$WORK/err-older")"
fi
for text in "Current version: $older_version" "Updating kolk $older_version → $VERSION"; do
  if grep -Fq "$text" "$WORK/out-older"; then pass; else fail "older-version output omitted: $text"; fi
done
if grep -Fq '/checksums.txt' "$older_log" && grep -Fq "kolk_${VERSION}_darwin_arm64.tar.gz" "$older_log"; then
  pass
else
  fail "older-version upgrade did not download both verified assets"
fi
if "$older_dir/kolk" | grep -Fq "kolk $VERSION test darwin_arm64"; then pass; else fail "older binary was not upgraded"; fi

current_dir="$WORK/install-current"
write_versioned_kolk "$current_dir/kolk" "$VERSION"
current_before="$(shasum -a 256 "$current_dir/kolk" | awk '{print $1}')"
current_log="$WORK/curl-current.log"
if run_installer Darwin arm64 "$current_dir" "$current_log" "$RELEASES" env -u KOLK_VERSION >"$WORK/out-current" 2>"$WORK/err-current"; then
  pass
else
  fail "current-version check failed: $(<"$WORK/err-current")"
fi
current_after="$(shasum -a 256 "$current_dir/kolk" | awk '{print $1}')"
if grep -Fq "Kolk is up to date ($VERSION)" "$WORK/out-current"; then pass; else fail "current version was not reported up to date"; fi
if [ "$(wc -l <"$current_log" | tr -d ' ')" = 1 ] && grep -Fq '/releases/latest' "$current_log"; then
  pass
else
  fail "current version requested an artifact: $(<"$current_log")"
fi
if [ "$current_before" = "$current_after" ]; then pass; else fail "current binary was unnecessarily replaced"; fi

newer_dir="$WORK/install-newer"
newer_version=2.0.0
write_versioned_kolk "$newer_dir/kolk" "$newer_version"
newer_before="$(shasum -a 256 "$newer_dir/kolk" | awk '{print $1}')"
newer_log="$WORK/curl-newer.log"
if run_installer Darwin arm64 "$newer_dir" "$newer_log" "$RELEASES" env -u KOLK_VERSION >"$WORK/out-newer" 2>"$WORK/err-newer"; then
  pass
else
  fail "newer-version check failed: $(<"$WORK/err-newer")"
fi
newer_after="$(shasum -a 256 "$newer_dir/kolk" | awk '{print $1}')"
if grep -Fq "Installed kolk $newer_version is newer than latest release $VERSION" "$WORK/out-newer"; then
  pass
else
  fail "newer local version did not name both versions"
fi
if [ "$(wc -l <"$newer_log" | tr -d ' ')" = 1 ] && [ "$newer_before" = "$newer_after" ]; then
  pass
else
  fail "newer local version was downloaded over or mutated"
fi

wide_dir="$WORK/install-wide-version"
write_versioned_kolk "$wide_dir/kolk" 0.9.10
wide_log="$WORK/curl-wide-version.log"
if run_installer Darwin arm64 "$wide_dir" "$wide_log" "$RELEASES" env \
  FAKE_LATEST_URL='https://github.com/onembyte/kolkrabbi/releases/tag/v0.10.0' >"$WORK/out-wide" 2>"$WORK/err-wide"; then
  fail "wide-component fixture unexpectedly installed absent 0.10.0 assets"
else
  pass
fi
if grep -Fq 'Updating kolk 0.9.10 → 0.10.0' "$WORK/out-wide"; then pass; else fail "version comparison treated 0.9.10 as newer than 0.10.0"; fi

pinned_dir="$WORK/install-pinned-existing"
write_versioned_kolk "$pinned_dir/kolk" 9.0.0
pinned_log="$WORK/curl-pinned-existing.log"
if run_installer Darwin arm64 "$pinned_dir" "$pinned_log" "$RELEASES" env KOLK_VERSION="v$VERSION" >"$WORK/out-pinned-existing" 2>"$WORK/err-pinned-existing"; then
  pass
else
  fail "explicit pinned reinstall failed: $(<"$WORK/err-pinned-existing")"
fi
if grep -Fq '/checksums.txt' "$pinned_log" && "$pinned_dir/kolk" | grep -Fq "kolk $VERSION test darwin_arm64"; then
  pass
else
  fail "explicit pinned version did not override a newer existing binary"
fi

replace_dir="$WORK/install-replace"
mkdir -p "$replace_dir"
printf 'old binary\n' >"$replace_dir/kolk"
chmod 0755 "$replace_dir/kolk"
if run_installer Darwin arm64 "$replace_dir" "$WORK/curl-replace.log" "$RELEASES" env KOLK_VERSION="v$VERSION" >/dev/null 2>"$WORK/err-replace"; then
  pass
else
  fail "replacement install failed: $(<"$WORK/err-replace")"
fi
if "$replace_dir/kolk" | grep -Fq "kolk $VERSION test darwin_arm64"; then pass; else fail "successful install did not replace the old binary"; fi
if installed_mode="$(stat -f '%Lp' "$replace_dir/kolk" 2>/dev/null)"; then
  :
else
  installed_mode="$(stat -c '%a' "$replace_dir/kolk")"
fi
if [ "$installed_mode" = 755 ]; then
  pass
else
  fail "installed binary mode is not 0755"
fi

unsupported_log="$WORK/curl-unsupported.log"
if run_installer FreeBSD amd64 "$WORK/install-unsupported" "$unsupported_log" "$RELEASES" env KOLK_VERSION="v$VERSION" >/dev/null 2>"$WORK/err-unsupported"; then
  fail "unsupported OS was accepted"
else
  pass
fi
if [ ! -s "$unsupported_log" ]; then pass; else fail "unsupported OS contacted the network"; fi

invalid_log="$WORK/curl-invalid-version.log"
if run_installer Darwin arm64 "$WORK/install-invalid" "$invalid_log" "$RELEASES" env KOLK_VERSION='../../evil' >/dev/null 2>"$WORK/err-invalid"; then
  fail "unsafe version was accepted"
else
  pass
fi
if [ ! -s "$invalid_log" ]; then pass; else fail "unsafe version contacted the network"; fi

relative_log="$WORK/curl-relative-dir.log"
if run_installer Darwin arm64 relative/bin "$relative_log" "$RELEASES" env KOLK_VERSION="v$VERSION" >/dev/null 2>"$WORK/err-relative-dir"; then
  fail "relative KOLK_INSTALL_DIR was accepted"
else
  pass
fi
if [ ! -s "$relative_log" ]; then pass; else fail "relative install directory contacted the network"; fi

bad_release="$WORK/releases-bad-checksum"
mkdir -p "$bad_release"
/bin/cp "$RELEASES/checksums.txt" "$bad_release/checksums.txt"
/bin/cp "$RELEASES/kolk_${VERSION}_darwin_arm64.tar.gz" "$bad_release/kolk_${VERSION}_darwin_arm64.tar.gz"
printf 'tampered\n' >>"$bad_release/kolk_${VERSION}_darwin_arm64.tar.gz"
preserve_dir="$WORK/install-preserve"
mkdir -p "$preserve_dir"
printf 'existing binary\n' >"$preserve_dir/kolk"
before="$(shasum -a 256 "$preserve_dir/kolk" | awk '{print $1}')"
if run_installer Darwin arm64 "$preserve_dir" "$WORK/curl-bad.log" "$bad_release" env KOLK_VERSION="v$VERSION" >/dev/null 2>"$WORK/err-bad"; then
  fail "checksum mismatch was accepted"
else
  pass
fi
after="$(shasum -a 256 "$preserve_dir/kolk" | awk '{print $1}')"
if [ "$before" = "$after" ]; then pass; else fail "checksum failure damaged the existing binary"; fi

missing_hash_release="$WORK/releases-missing-hash"
mkdir -p "$missing_hash_release"
: >"$missing_hash_release/checksums.txt"
/bin/cp "$RELEASES/kolk_${VERSION}_darwin_arm64.tar.gz" "$missing_hash_release/kolk_${VERSION}_darwin_arm64.tar.gz"
if run_installer Darwin arm64 "$WORK/install-missing-hash" "$WORK/curl-missing-hash.log" "$missing_hash_release" env KOLK_VERSION="v$VERSION" >/dev/null 2>"$WORK/err-missing-hash"; then
  fail "archive without a checksum entry was accepted"
else
  pass
fi
if [ ! -e "$WORK/install-missing-hash/kolk" ]; then pass; else fail "missing checksum entry wrote an install target"; fi

extra_release="$WORK/releases-extra-member"
extra_payload="$WORK/extra-payload"
mkdir -p "$extra_release" "$extra_payload"
/bin/cp "$WORK/payload-darwin_arm64/kolk" "$extra_payload/kolk"
/bin/cp "$WORK/payload-darwin_arm64/README.md" "$extra_payload/README.md"
/bin/cp "$WORK/payload-darwin_arm64/LICENSE" "$extra_payload/LICENSE"
printf 'unexpected\n' >"$extra_payload/surprise.txt"
tar -czf "$extra_release/kolk_${VERSION}_darwin_arm64.tar.gz" -C "$extra_payload" kolk README.md LICENSE surprise.txt
shasum -a 256 "$extra_release/kolk_${VERSION}_darwin_arm64.tar.gz" | \
  awk '{print $1 "  kolk_'"$VERSION"'_darwin_arm64.tar.gz"}' >"$extra_release/checksums.txt"
if run_installer Darwin arm64 "$WORK/install-extra" "$WORK/curl-extra.log" "$extra_release" env KOLK_VERSION="v$VERSION" >/dev/null 2>"$WORK/err-extra"; then
  fail "archive with an unexpected member was accepted"
else
  pass
fi
if [ ! -e "$WORK/install-extra/kolk" ]; then pass; else fail "unexpected archive member wrote an install target"; fi

symlink_release="$WORK/releases-symlink"
mkdir -p "$symlink_release" "$WORK/symlink-payload"
ln -s /bin/sh "$WORK/symlink-payload/kolk"
/bin/cp "$WORK/payload-darwin_arm64/README.md" "$WORK/symlink-payload/README.md"
/bin/cp "$WORK/payload-darwin_arm64/LICENSE" "$WORK/symlink-payload/LICENSE"
tar -czf "$symlink_release/kolk_${VERSION}_darwin_arm64.tar.gz" -C "$WORK/symlink-payload" kolk README.md LICENSE
shasum -a 256 "$symlink_release/kolk_${VERSION}_darwin_arm64.tar.gz" | \
  awk '{print $1 "  kolk_'"$VERSION"'_darwin_arm64.tar.gz"}' >"$symlink_release/checksums.txt"
if run_installer Darwin arm64 "$WORK/install-symlink" "$WORK/curl-symlink.log" "$symlink_release" env KOLK_VERSION="v$VERSION" >/dev/null 2>"$WORK/err-symlink"; then
  fail "symlink binary archive was accepted"
else
  pass
fi
if [ ! -e "$WORK/install-symlink/kolk" ]; then pass; else fail "symlink archive wrote an install target"; fi

truncated="$WORK/install-truncated.sh"
sed '$d' "$INSTALLER" >"$truncated"
truncated_log="$WORK/curl-truncated.log"
if PATH="$FAKEBIN:$SYSTEM_PATH" \
  HOME="$HOME_DIR" \
  TMPDIR="$STAGE_ROOT" \
  FAKE_UNAME_S=Darwin \
  FAKE_UNAME_M=arm64 \
  FAKE_CURL_LOG="$truncated_log" \
  FAKE_RELEASE_DIR="$RELEASES" \
  FAKE_LATEST_URL="https://github.com/onembyte/kolkrabbi/releases/tag/v$VERSION" \
  KOLK_INSTALL_DIR="$WORK/install-truncated" \
  KOLK_VERSION="v$VERSION" \
  bash <"$truncated" >/dev/null 2>"$WORK/err-truncated"; then
  pass
else
  fail "truncated definition-only script returned an error"
fi
if [ ! -s "$truncated_log" ] && [ ! -e "$WORK/install-truncated/kolk" ]; then
  pass
else
  fail "truncated installer performed a side effect"
fi

if ! compgen -G "$STAGE_ROOT/kolk-install.*" >/dev/null; then
  pass
else
  fail "installer left a private staging directory behind"
fi

if [ "$failures" -ne 0 ]; then
  printf 'installer: %d/%d checks failed\n' "$failures" "$checks" >&2
  exit 1
fi
printf 'installer: %d checks passed\n' "$checks"
