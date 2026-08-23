#!/usr/bin/env bash
set -euo pipefail

readonly KOLK_RELEASES="https://github.com/onembyte/kolkrabbi/releases"

die() {
  printf 'kolk installer: %s\n' "$1" >&2
  exit 1
}

need() {
  command -v "$1" >/dev/null 2>&1 || die "required command not found: $1"
}

path_contains() {
  case ":${PATH:-}:" in
    *":$1:"*) return 0 ;;
    *) return 1 ;;
  esac
}

detect_target() {
  local os arch
  case "$(uname -s)" in
    Darwin) os=darwin ;;
    Linux) os=linux ;;
    *) die "unsupported operating system: $(uname -s) (supported: macOS and Linux)" ;;
  esac

  case "$(uname -m)" in
    x86_64|amd64) arch=amd64 ;;
    arm64|aarch64) arch=arm64 ;;
    *) die "unsupported architecture: $(uname -m) (supported: amd64 and arm64)" ;;
  esac
  printf '%s_%s\n' "$os" "$arch"
}

valid_version() {
  [[ "$1" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?(\+[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?$ ]]
}

resolve_version() {
  local requested latest
  requested="${KOLK_VERSION:-}"
  if [ -n "$requested" ]; then
    requested="${requested#v}"
    valid_version "$requested" || die "invalid KOLK_VERSION: use a semantic version such as v0.1.0"
    printf '%s\n' "$requested"
    return
  fi

  latest="$(curl -fsSIL --proto '=https' --tlsv1.2 --retry 3 --retry-delay 1 \
    --connect-timeout 10 -o /dev/null -w '%{url_effective}' "$KOLK_RELEASES/latest")" || \
    die "could not discover the latest release"
  case "$latest" in
    "$KOLK_RELEASES/tag/v"*) requested="${latest##*/v}" ;;
    *) die "latest release redirected to an unexpected URL" ;;
  esac
  valid_version "$requested" || die "latest release is not a semantic version"
  printf '%s\n' "$requested"
}

choose_install_dir() {
  local candidate existing dir
  if [ -n "${KOLK_INSTALL_DIR:-}" ]; then
    case "$KOLK_INSTALL_DIR" in
      /*) printf '%s\n' "$KOLK_INSTALL_DIR" ;;
      *) die "KOLK_INSTALL_DIR must be an absolute path" ;;
    esac
    return
  fi

  if [ -n "${HOME:-}" ]; then
    for candidate in "$HOME/.local/bin" "$HOME/bin"; do
      if path_contains "$candidate" && { [ -d "$candidate" ] || [ -w "$HOME" ]; }; then
        printf '%s\n' "$candidate"
        return
      fi
    done
  fi

  existing="$(command -v kolk 2>/dev/null || true)"
  if [ -n "$existing" ]; then
    dir="${existing%/*}"
    if [ -n "$dir" ] && [ -d "$dir" ] && [ -w "$dir" ]; then
      printf '%s\n' "$dir"
      return
    fi
  fi

  for candidate in /usr/local/bin /opt/homebrew/bin; do
    if path_contains "$candidate" && [ -d "$candidate" ] && [ -w "$candidate" ]; then
      printf '%s\n' "$candidate"
      return
    fi
  done

  while IFS= read -r candidate; do
    case "$candidate" in
      /*)
        if [ -d "$candidate" ] && [ -w "$candidate" ]; then
          printf '%s\n' "$candidate"
          return
        fi
        ;;
    esac
  done < <(printf '%s' "${PATH:-}" | tr ':' '\n')

  die "no writable directory is on PATH; add ~/.local/bin to PATH or set KOLK_INSTALL_DIR"
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

verify_checksum() {
  local manifest="$1" archive="$2" name expected actual
  name="${archive##*/}"
  expected="$(awk -v name="$name" '$2 == name {print $1}' "$manifest")"
  if [ "${#expected}" -ne 64 ] || [[ ! "$expected" =~ ^[0-9a-f]+$ ]]; then
    die "checksums.txt has no unique SHA-256 entry for $name"
  fi
  actual="$(sha256_file "$archive")"
  [ "$actual" = "$expected" ] || die "SHA-256 mismatch for $name"
}

validate_archive() {
  local archive="$1" listing verbose entry mode
  local kolk_count=0 readme_count=0 license_count=0
  listing="$(tar -tzf "$archive")" || die "could not list release archive"
  while IFS= read -r entry; do
    case "$entry" in
      kolk) kolk_count=$((kolk_count + 1)) ;;
      README.md) readme_count=$((readme_count + 1)) ;;
      LICENSE) license_count=$((license_count + 1)) ;;
      *) die "release archive contains an unexpected path: $entry" ;;
    esac
  done <<<"$listing"
  if [ "$kolk_count" -ne 1 ] || [ "$readme_count" -ne 1 ] || [ "$license_count" -ne 1 ]; then
    die "release archive must contain one kolk, README.md, and LICENSE"
  fi

  verbose="$(tar -tvzf "$archive")" || die "could not inspect release archive"
  while IFS=' ' read -r mode _; do
    case "$mode" in
      -*) ;;
      *) die "release archive contains a link or non-regular member" ;;
    esac
  done <<<"$verbose"
}

cleanup() {
  if [ -n "${install_temp:-}" ] && [ -e "$install_temp" ]; then
    rm -f -- "$install_temp"
  fi
  if [ -n "${stage_dir:-}" ] && [ -d "$stage_dir" ]; then
    rm -rf -- "$stage_dir"
  fi
}

main() {
  local target version install_dir archive_name download_base archive manifest extract_dir
  stage_dir=""
  install_temp=""
  trap cleanup EXIT
  trap 'exit 129' HUP
  trap 'exit 130' INT
  trap 'exit 143' TERM
  umask 077

  for command_name in uname curl tar awk mktemp mkdir chmod cp mv rm tr; do
    need "$command_name"
  done

  target="$(detect_target)"
  version="$(resolve_version)"
  install_dir="$(choose_install_dir)"
  mkdir -p "$install_dir" || die "could not create install directory: $install_dir"
  [ -d "$install_dir" ] && [ -w "$install_dir" ] || die "install directory is not writable: $install_dir"

  archive_name="kolk_${version}_${target}.tar.gz"
  download_base="$KOLK_RELEASES/download/v$version"
  stage_dir="$(mktemp -d "${TMPDIR:-/tmp}/kolk-install.XXXXXX")" || die "could not create staging directory"
  archive="$stage_dir/$archive_name"
  manifest="$stage_dir/checksums.txt"
  extract_dir="$stage_dir/extract"

  printf 'Downloading kolk v%s for %s...\n' "$version" "$target"
  download "$download_base/checksums.txt" "$manifest" || die "could not download checksums.txt"
  download "$download_base/$archive_name" "$archive" || die "could not download $archive_name"
  verify_checksum "$manifest" "$archive"
  validate_archive "$archive"

  mkdir "$extract_dir"
  tar -xzf "$archive" -C "$extract_dir" kolk || die "could not extract kolk"
  [ -f "$extract_dir/kolk" ] && [ ! -L "$extract_dir/kolk" ] || die "extracted kolk is not a regular file"

  install_temp="$(mktemp "$install_dir/.kolk.XXXXXX")" || die "could not stage binary in $install_dir"
  cp "$extract_dir/kolk" "$install_temp"
  chmod 0755 "$install_temp"
  mv -f "$install_temp" "$install_dir/kolk"
  install_temp=""

  printf 'Installed kolk v%s to %s/kolk\n' "$version" "$install_dir"
  if ! path_contains "$install_dir"; then
    printf 'Add %s to PATH before running kolk.\n' "$install_dir"
  else
    printf 'Run: kolk\n'
  fi
}

main "$@"
