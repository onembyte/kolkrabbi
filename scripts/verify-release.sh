#!/usr/bin/env bash
# Authenticate and inspect every public artifact before declaring a release usable.
set -euo pipefail

readonly RELEASES_URL="https://github.com/onembyte/kolkrabbi/releases"
readonly OIDC_ISSUER="https://token.actions.githubusercontent.com"
readonly WORKFLOW_IDENTITY="https://github.com/onembyte/kolkrabbi/.github/workflows/release.yml@refs/tags/"

die() {
  printf 'release verifier: %s\n' "$1" >&2
  exit 1
}

need() {
  command -v "$1" >/dev/null 2>&1 || die "required command not found: $1"
}

download() {
  local url="$1" output="$2"
  curl -fsSL --proto '=https' --tlsv1.2 --retry 3 --retry-delay 1 \
    --connect-timeout 10 -o "$output" "$url"
}

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
  else
    die "SHA-256 tool not found (need sha256sum or shasum)"
  fi
}

expected_checksum() {
  local manifest="$1" name="$2" expected
  expected="$(awk -v name="$name" '$2 == name {print $1}' "$manifest")"
  if [ "${#expected}" -ne 64 ] || [[ ! "$expected" =~ ^[0-9a-f]+$ ]]; then
    die "checksums.txt has no unique lowercase SHA-256 entry for $name"
  fi
  printf '%s\n' "$expected"
}

validate_manifest() {
  local manifest="$1"
  shift
  local name

  [ "$(awk 'NF {count++} END {print count + 0}' "$manifest")" -eq 4 ] || \
    die "checksums.txt must contain exactly four non-empty rows"
  awk 'NF && NF != 2 {exit 1}' "$manifest" || \
    die "checksums.txt contains a malformed row"

  for name in "$@"; do
    expected_checksum "$manifest" "$name" >/dev/null
  done

  awk -v one="$1" -v two="$2" -v three="$3" -v four="$4" \
    'NF && $2 != one && $2 != two && $2 != three && $2 != four {exit 1}' "$manifest" || \
    die "checksums.txt contains an unexpected asset"
}

verify_checksum() {
  local manifest="$1" archive="$2" name expected actual
  name="${archive##*/}"
  expected="$(expected_checksum "$manifest" "$name")"
  actual="$(sha256_file "$archive")"
  [ "$actual" = "$expected" ] || die "SHA-256 mismatch for $name"
}

validate_archive() {
  local archive="$1" listing verbose entry mode
  local kolk_count=0 readme_count=0 license_count=0

  listing="$(tar -tzf "$archive")" || die "could not list ${archive##*/}"
  while IFS= read -r entry; do
    case "$entry" in
      kolk) kolk_count=$((kolk_count + 1)) ;;
      README.md) readme_count=$((readme_count + 1)) ;;
      LICENSE) license_count=$((license_count + 1)) ;;
      *) die "${archive##*/} contains an unexpected path: $entry" ;;
    esac
  done <<<"$listing"
  if [ "$kolk_count" -ne 1 ] || [ "$readme_count" -ne 1 ] || [ "$license_count" -ne 1 ]; then
    die "${archive##*/} must contain one kolk, README.md, and LICENSE"
  fi

  verbose="$(tar -tvzf "$archive")" || die "could not inspect ${archive##*/}"
  while IFS=' ' read -r mode _; do
    case "$mode" in
      -*) ;;
      *) die "${archive##*/} contains a link or non-regular member" ;;
    esac
  done <<<"$verbose"
}

detect_host() {
  local os arch
  case "$(uname -s)" in
    Darwin) os=darwin ;;
    Linux) os=linux ;;
    *) die "unsupported verification host: $(uname -s)" ;;
  esac
  case "$(uname -m)" in
    x86_64|amd64) arch=amd64 ;;
    arm64|aarch64) arch=arm64 ;;
    *) die "unsupported verification architecture: $(uname -m)" ;;
  esac
  printf '%s_%s\n' "$os" "$arch"
}

cleanup() {
  if [ -n "${stage_dir:-}" ] && [ -d "$stage_dir" ]; then
    rm -rf -- "$stage_dir"
  fi
}

main() {
  local tag="${1:-}" version base manifest bundle identity host_target host_os host_arch
  local archive_name archive extract_dir version_line command_name
  local -a archive_names

  [ "$#" -eq 1 ] || die "usage: scripts/verify-release.sh v<semver>"
  "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/check-release-tag.sh" "$tag" >/dev/null

  for command_name in uname curl cosign tar awk mktemp mkdir rm chmod; do
    need "$command_name"
  done
  if ! command -v sha256sum >/dev/null 2>&1 && ! command -v shasum >/dev/null 2>&1; then
    die "SHA-256 tool not found (need sha256sum or shasum)"
  fi

  version="${tag#v}"
  host_target="$(detect_host)"
  host_os="${host_target%_*}"
  host_arch="${host_target#*_}"
  base="$RELEASES_URL/download/$tag"
  identity="$WORKFLOW_IDENTITY$tag"
  archive_names=(
    "kolk_${version}_darwin_amd64.tar.gz"
    "kolk_${version}_darwin_arm64.tar.gz"
    "kolk_${version}_linux_amd64.tar.gz"
    "kolk_${version}_linux_arm64.tar.gz"
  )

  stage_dir=""
  trap cleanup EXIT
  trap 'exit 129' HUP
  trap 'exit 130' INT
  trap 'exit 143' TERM
  umask 077
  stage_dir="$(mktemp -d "${TMPDIR:-/tmp}/kolk-release-verify.XXXXXX")" || \
    die "could not create verification directory"
  manifest="$stage_dir/checksums.txt"
  bundle="$stage_dir/checksums.txt.sigstore.json"

  download "$base/checksums.txt" "$manifest" || die "could not download checksums.txt"
  download "$base/checksums.txt.sigstore.json" "$bundle" || \
    die "could not download checksums.txt.sigstore.json"

  cosign verify-blob \
    --bundle "$bundle" \
    --certificate-identity "$identity" \
    --certificate-oidc-issuer "$OIDC_ISSUER" \
    "$manifest" >/dev/null || die "checksum manifest signature verification failed"

  validate_manifest "$manifest" "${archive_names[@]}"

  for archive_name in "${archive_names[@]}"; do
    archive="$stage_dir/$archive_name"
    download "$base/$archive_name" "$archive" || die "could not download $archive_name"
    verify_checksum "$manifest" "$archive"
    validate_archive "$archive"
  done

  archive_name="kolk_${version}_${host_target}.tar.gz"
  archive="$stage_dir/$archive_name"
  extract_dir="$stage_dir/host"
  mkdir "$extract_dir"
  tar -xzf "$archive" -C "$extract_dir" kolk || die "could not extract host kolk"
  [ -f "$extract_dir/kolk" ] && [ ! -L "$extract_dir/kolk" ] || \
    die "extracted host kolk is not a regular file"
  chmod 0755 "$extract_dir/kolk"
  # `kolk version` is a session command since v1.2.33; the identity line is in `kolk help`.
  help_text="$("$extract_dir/kolk" help 2>/dev/null)" || die "host kolk help command failed"
  version_line="$(printf '%s\n' "$help_text" | awk '$1 == "kolk" && $2 ~ /^v?[0-9]/ { sub(/^[ \t]+/, ""); print; exit }')"
  [ -n "$version_line" ] || die "host kolk help did not print a build identity line"

  case "$version_line" in
    "kolk $version "*) ;;
    *) die "host kolk does not report release version $version" ;;
  esac
  case "$version_line" in
    *" $host_os/$host_arch") ;;
    *) die "host kolk does not report $host_os/$host_arch" ;;
  esac
  case "$version_line" in
    "kolk dev "*) die "host kolk is an unstamped development build" ;;
  esac

  printf 'release %s verified: four signed/checksummed archives and host build\n' "$tag"
}

main "$@"
