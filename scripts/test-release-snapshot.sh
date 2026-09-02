#!/usr/bin/env bash
# Build and inspect real snapshot archives without publishing or signing.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
RELEASE_TOOL="${KOLK_GORELEASER_BIN:-goreleaser}"

if [ ! -x "$RELEASE_TOOL" ] && ! command -v "$RELEASE_TOOL" >/dev/null 2>&1; then
  printf 'snapshot: GoReleaser not found: %s\n' "$RELEASE_TOOL" >&2
  exit 1
fi

cd "$ROOT"
"$RELEASE_TOOL" check
"$RELEASE_TOOL" release --snapshot --clean --skip=sign

failures=0
checks=0
pass() { checks=$((checks + 1)); }
fail() { checks=$((checks + 1)); failures=$((failures + 1)); printf 'snapshot: %s\n' "$1" >&2; }

archives=()
while IFS= read -r archive; do
  archives+=("$archive")
done < <(find dist -maxdepth 1 -type f -name 'kolk_*.tar.gz' -print | sort)

if [ "${#archives[@]}" -eq 4 ]; then pass; else fail "archive count = ${#archives[@]}, want 4"; fi

for os_name in darwin linux; do
  for arch_name in amd64 arm64; do
    matches=()
    while IFS= read -r archive; do
      matches+=("$archive")
    done < <(find dist -maxdepth 1 -type f -name "kolk_*_${os_name}_${arch_name}.tar.gz" -print)
    if [ "${#matches[@]}" -ne 1 ]; then
      fail "$os_name/$arch_name archive count = ${#matches[@]}, want 1"
      continue
    fi
    members="$(tar -tzf "${matches[0]}")"
    for member in kolk README.md LICENSE; do
      if printf '%s\n' "$members" | grep -Fxq -- "$member"; then
        pass
      else
        fail "${matches[0]} is missing $member"
      fi
    done
  done
done

if find dist -maxdepth 1 -type f | grep -Eiq 'windows|\.zip$'; then
  fail "snapshot contains a Windows artifact"
else
  pass
fi

if [ -f dist/checksums.txt ]; then
  pass
else
  fail "snapshot has no checksums.txt"
fi

for archive in "${archives[@]}"; do
  name="$(basename "$archive")"
  expected="$(awk -v name="$name" '$2 == name {print $1}' dist/checksums.txt)"
  actual="$(shasum -a 256 "$archive" | awk '{print $1}')"
  if [ -n "$expected" ] && [ "$actual" = "$expected" ]; then
    pass
  else
    fail "$name does not match checksums.txt"
  fi
done

host_os="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$(uname -m)" in
  x86_64) host_arch=amd64 ;;
  arm64|aarch64) host_arch=arm64 ;;
  *) host_arch=unsupported ;;
esac

if { [ "$host_os" = darwin ] || [ "$host_os" = linux ]; } && [ "$host_arch" != unsupported ]; then
  host_archive="$(find dist -maxdepth 1 -type f -name "kolk_*_${host_os}_${host_arch}.tar.gz" -print)"
  stage="$(mktemp -d "${TMPDIR:-/tmp}/kolk-snapshot.XXXXXX")"
  trap 'rm -rf "$stage"' EXIT
  tar -xzf "$host_archive" -C "$stage" kolk
  # `kolk version` is a session command since v1.2.33; the identity line is in `kolk help`.
  version="$($stage/kolk help | awk '$1 == "kolk" && $2 ~ /^(v?[0-9]|dev)/ { print; exit }')"
  if printf '%s\n' "$version" | grep -Fq " $host_os/$host_arch"; then pass; else fail "host build identity: $version"; fi
  if printf '%s\n' "$version" | grep -Fq 'kolk dev '; then fail "snapshot version was not stamped"; else pass; fi
fi

if [ "$failures" -ne 0 ]; then
  printf 'snapshot: %d/%d checks failed\n' "$failures" "$checks" >&2
  exit 1
fi
printf 'snapshot: %d checks passed\n' "$checks"
