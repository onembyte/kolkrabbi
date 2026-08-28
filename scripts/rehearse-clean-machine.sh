#!/usr/bin/env bash
# T0.5 clean-machine rehearsal: install, first run, key, first model response,
# on a machine with no Go toolchain and no prior Kolkrabbi files.
#
# CI proves the release artifacts verify. It cannot prove a stranger gets from
# `curl` to an answer, because CI has a toolchain, a checkout, and no first-run
# experience at all. This script is that proof, written down so what was
# rehearsed is legible afterwards rather than remembered.
#
# It NEVER deletes anything. A machine that is not clean is a refusal with the
# paths named, because a rehearsal that tidies away the state it was supposed to
# find is not a rehearsal — and the state it would tidy is somebody's session
# history and API keys.
#
# Usage:
#   KOLK_REHEARSAL_KEY=sk-or-... ./scripts/rehearse-clean-machine.sh
#
# Set KOLK_REHEARSAL_DRY_RUN=1 to check the preconditions and stop before
# installing anything. Worth doing first: the preconditions are the part you
# have to fix by hand, and finding that out after an install is worse.
#
# The key is read from the environment and never written to the log. Use a
# throwaway; a rehearsal is exactly when you find out what a first run does
# with a credential.
set -uo pipefail

failures=0
checks=0
step=0

pass() { checks=$((checks + 1)); printf '  ok    %s\n' "$1"; }
fail() { checks=$((checks + 1)); failures=$((failures + 1)); printf '  FAIL  %s\n' "$1" >&2; }
head_step() { step=$((step + 1)); printf '\n%d. %s\n' "$step" "$1"; }

# ---------------------------------------------------------------------------
head_step "the machine is actually clean"

if command -v go >/dev/null 2>&1; then
  printf '  note  a Go toolchain is present (%s).\n' "$(go version 2>/dev/null | awk '{print $3}')"
  printf '        that is not fatal, but it means this run does not prove the\n'
  printf '        no-toolchain case, which is the one T0.5 exists for.\n'
else
  pass "no Go toolchain, which is the case being rehearsed"
fi

existing=""
for dir in "${KOLK_CONFIG_DIR:-$HOME/.config/kolkrabbi}" \
           "${KOLK_DATA_DIR:-$HOME/.local/share/kolkrabbi}" \
           "${KOLK_CACHE_DIR:-$HOME/.cache/kolkrabbi}"; do
  [ -e "$dir" ] && existing="$existing$dir"$'\n'
done
if [ -n "$existing" ]; then
  fail "prior Kolkrabbi state exists, so this is not a first run:"
  printf '%s' "$existing" | sed 's/^/        /' >&2
  printf '        move these aside yourself and re-run. this script will not\n' >&2
  printf '        delete them: they hold sessions and credentials.\n' >&2
  printf '\nrehearsal: refused before installing anything (%d checks, %d failed)\n' "$checks" "$failures"
  exit 1
fi
pass "no prior Kolkrabbi config, data or cache"

if command -v kolk >/dev/null 2>&1; then
  fail "kolk is already on PATH at $(command -v kolk); uninstall it first"
else
  pass "kolk is not installed"
fi

if [ -n "${KOLK_REHEARSAL_DRY_RUN:-}" ]; then
  printf '\nrehearsal: preconditions only, nothing installed (%d checks, %d failed)\n' "$checks" "$failures"
  [ "$failures" -eq 0 ] || exit 1
  exit 0
fi

# ---------------------------------------------------------------------------
head_step "install from the public installer, the way a stranger would"

if ! command -v curl >/dev/null 2>&1; then
  fail "curl is missing, so the documented install cannot be rehearsed"
else
  if curl -fsSL https://kolkrabbi.dev/install.sh | sh >/tmp/kolk-rehearsal-install.log 2>&1; then
    pass "the installer ran"
  else
    fail "the installer failed; see /tmp/kolk-rehearsal-install.log"
  fi
fi

# The installer may place kolk somewhere not yet on this shell's PATH, which is
# itself part of what the rehearsal checks: a user who cannot type `kolk` after
# installing has not had a successful install.
if command -v kolk >/dev/null 2>&1; then
  pass "kolk is on PATH without opening a new shell"
  KOLK=kolk
elif [ -x "$HOME/.local/bin/kolk" ]; then
  fail "kolk installed to ~/.local/bin but is not on PATH; the installer must say so plainly"
  KOLK="$HOME/.local/bin/kolk"
else
  fail "kolk is not runnable after installing"
  printf '\nrehearsal: stopped (%d checks, %d failed)\n' "$checks" "$failures"
  exit 1
fi

# ---------------------------------------------------------------------------
head_step "the binary reports a real version"

version="$("$KOLK" version 2>&1 | head -1)"
if printf '%s' "$version" | grep -Eq '[0-9]+\.[0-9]+\.[0-9]+'; then
  pass "version reads $version"
else
  fail "version reads '$version', which is not a release build"
fi

# ---------------------------------------------------------------------------
head_step "first run with no key explains itself"

first="$("$KOLK" doctor 2>&1)"
if printf '%s' "$first" | grep -qi 'key'; then
  pass "the first run names the missing credential"
else
  fail "the first run does not mention a key; a stranger is stuck here"
  printf '%s\n' "$first" | sed 's/^/        /' >&2
fi

# ---------------------------------------------------------------------------
head_step "adding a key"

if [ -z "${KOLK_REHEARSAL_KEY:-}" ]; then
  fail "KOLK_REHEARSAL_KEY is unset, so the two steps that matter most cannot run"
  printf '        re-run with: KOLK_REHEARSAL_KEY=sk-or-... %s\n' "$0" >&2
  printf '\nrehearsal: incomplete (%d checks, %d failed)\n' "$checks" "$failures"
  exit 1
fi

if "$KOLK" key "$KOLK_REHEARSAL_KEY" >/dev/null 2>&1; then
  pass "the key was accepted"
else
  fail "kolk key rejected the key"
fi

# ---------------------------------------------------------------------------
head_step "a first model response"

# -p is the non-interactive form: one prompt, one answer, no TUI. A rehearsal
# that needs a human to type into a terminal cannot be re-run identically.
answer="$("$KOLK" -p 'Reply with the single word: ready' 2>&1)"
if printf '%s' "$answer" | grep -qi 'ready'; then
  pass "a model answered on a machine that had nothing installed ten minutes ago"
else
  fail "no usable first response"
  printf '%s\n' "$answer" | sed 's/^/        /' >&2
fi

# ---------------------------------------------------------------------------
head_step "the free-first promise held"

model="$("$KOLK" config get model 2>&1)"
printf '  note  session model: %s\n' "$model"
printf '        B12.13 says a first run stays free. If this names a billed\n'
printf '        model, that is a finding, not a preference.\n'

printf '\nrehearsal: %d checks, %d failed\n' "$checks" "$failures"
[ "$failures" -eq 0 ] || exit 1
