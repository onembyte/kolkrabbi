#!/usr/bin/env bash
# A local end-to-end smoke test: build kolk, then drive the real binary through
# the things a person actually does with it, and say what worked.
#
#   ./scripts/smoke.sh                     # scripted mock: no key, no network, no cost
#   ./scripts/smoke.sh --real              # your real key, one cheap turn
#   ./scripts/smoke.sh --real --key-file ~/keys/openrouter
#   ./scripts/smoke.sh --real --key-dir  ~/keys       # scan a folder for the key
#   ./scripts/smoke.sh --real --model google/gemini-2.5-flash
#
# It never touches your real ~/.config/kolk: HOME is redirected to a scratch
# directory for the whole run, so the smoke test cannot overwrite your settings
# or leave sessions behind. The key is never printed, only masked.
set -uo pipefail
cd "$(dirname "$0")/.."
REPO="$PWD"

# Captured before HOME is redirected below, so --real can still find the key
# the caller already had.
REAL_HOME="$HOME"
SMOKE_ENV_KEY="${OPENROUTER_API_KEY:-}"

REAL=0
KEY_FILE=""
KEY_DIR=""
MODEL=""
KEEP=0

while [ $# -gt 0 ]; do
  case "$1" in
    --real)     REAL=1 ;;
    --key-file) KEY_FILE="${2:-}"; shift ;;
    --key-dir)  KEY_DIR="${2:-}"; shift ;;
    --model)    MODEL="${2:-}"; shift ;;
    --keep)     KEEP=1 ;;
    -h|--help)  sed -n '2,16p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "unknown option: $1 (try --help)"; exit 2 ;;
  esac
  shift
done

# ── output ────────────────────────────────────────────────────────────────
bold=$(printf '\033[1m'); dim=$(printf '\033[2m'); red=$(printf '\033[31m')
grn=$(printf '\033[32m'); yel=$(printf '\033[33m'); rst=$(printf '\033[0m')
pass=0; fail=0
ok()   { pass=$((pass+1)); printf '  %s✓%s %s\n' "$grn" "$rst" "$1"; }
no()   { fail=$((fail+1)); printf '  %s✗%s %s\n' "$red" "$rst" "$1"; [ -n "${2:-}" ] && printf '      %s%s%s\n' "$dim" "$2" "$rst"; }
note() { printf '  %s·%s %s\n' "$dim" "$rst" "$1"; }
head1(){ printf '\n%s%s%s\n' "$bold" "$1" "$rst"; }
mask() { # never print a key
  local k="$1"
  if [ "${#k}" -le 10 ]; then echo '****'; else echo "${k:0:6}…${k: -4}"; fi
}

# check <name> <expected-exit> <command...>
check() {
  local name="$1" want="$2"; shift 2
  local out rc
  out="$("$@" 2>&1)"; rc=$?
  if [ "$rc" = "$want" ]; then ok "$name"; else no "$name" "exit $rc, wanted $want: $(echo "$out" | head -2 | tr '\n' ' ')"; fi
  LAST_OUT="$out"
}

# contains <name> <needle>  — asserts against the previous check's output
contains() {
  case "$LAST_OUT" in
    *"$2"*) ok "$1" ;;
    *)      no "$1" "output did not contain: $2" ;;
  esac
}

# ── scratch: an isolated HOME, so your real config is untouchable ──────────
SCRATCH="$(mktemp -d)"
export HOME="$SCRATCH/home"
export XDG_CONFIG_HOME="$HOME/.config"
export XDG_DATA_HOME="$HOME/.local/share"
export XDG_CACHE_HOME="$HOME/.cache"
mkdir -p "$HOME" "$SCRATCH/work"
cleanup() {
  if [ -n "${MOCK_PID:-}" ]; then kill "$MOCK_PID" 2>/dev/null; wait "$MOCK_PID" 2>/dev/null; fi
  if [ "$KEEP" = 1 ]; then echo "kept: $SCRATCH"; else rm -rf "$SCRATCH"; fi
}
trap cleanup EXIT

# ── build ─────────────────────────────────────────────────────────────────
head1 "build"
if CGO_ENABLED=0 go build -trimpath -o "$SCRATCH/kolk" ./cmd/kolk 2>"$SCRATCH/build.err"; then
  ok "kolk builds (CGO_ENABLED=0)"
else
  no "kolk builds" "$(head -3 "$SCRATCH/build.err")"; printf '\n%sbuild failed — nothing else can run%s\n' "$red" "$rst"; exit 1
fi
go build -o "$SCRATCH/kolk-mock" ./cmd/kolk-mock 2>/dev/null && ok "kolk-mock builds" || no "kolk-mock builds"
K="$SCRATCH/kolk"

# ── the surface: everything that needs no model at all ────────────────────
head1 "surface (no key, no network)"
check "kolk version"          0 "$K" version
contains "  names the build"     "kolk "
check "kolk help"             0 "$K" help
contains "  lists commands"      "Commands:"
check "kolk help config"      0 "$K" help config
contains "  shows the grammar"   "usage: kolk config"
check "kolk sessions (empty)"  0 "$K" sessions
contains "  says so plainly"     "no sessions yet"
check "kolk stats (empty)"     0 "$K" stats
contains "  keeps the local promise" "nothing ever leaves this machine"
check "bad flag exits 2"       2 "$K" --nope
check "missing flag value exits 2" 2 "$K" --model
check "unknown help topic exits 2" 2 "$K" help nope

head1 "first run with no key"
OPENROUTER_API_KEY="" check "exits 1, not a panic" 1 "$K" -p "hello"
contains "  names the command that fixes it" "kolk config set-key"
contains "  names the env var"                "OPENROUTER_API_KEY"

head1 "config round-trips"
check "config set-key"   0 "$K" config set-key "sk-or-v1-smoketestsmoketest0000"
check "config set-tier"  0 "$K" config set-tier quick "google/gemini-2.5-flash"
check "config show"      0 "$K" config show
contains "  masks the key"        "sk-or-…0000"
contains "  keeps the tier"       "google/gemini-2.5-flash"
case "$LAST_OUT" in
  *"smoketestsmoketest"*) no "  never prints the whole key" "config show leaked it" ;;
  *) ok "  never prints the whole key" ;;
esac
check "config rejects a bad tier" 2 "$K" config set-tier medium some/model
rm -f "$HOME/.config/kolk/config.json"

# ── a real turn, against the scripted mock ────────────────────────────────
head1 "a real turn (scripted mock — no network, no cost)"
"$SCRATCH/kolk-mock" >"$SCRATCH/mock.log" 2>&1 &
MOCK_PID=$!
URL=""
for _ in $(seq 200); do
  URL="$(grep -o 'http://127\.0\.0\.1:[0-9]*' "$SCRATCH/mock.log" 2>/dev/null | head -1)"
  [ -n "$URL" ] && break
  sleep 0.05
done
if [ -z "$URL" ]; then
  no "mock server starts" "$(head -3 "$SCRATCH/mock.log")"
else
  ok "mock server starts"
  cd "$SCRATCH/work"
  OPENROUTER_API_KEY=mock-key check "one code-mode turn" 0 "$K" --base-url "$URL" -y -p "create the hello file"
  contains "  streamed an answer" "All done"
  [ -f "$SCRATCH/work/hello-from-mock.txt" ] && ok "  the tool actually wrote the file" || no "  the tool actually wrote the file"
  cd "$REPO"
  check "the session was saved" 0 "$K" sessions
  contains "  and is listed"      "create the hello file"
  check "stats recorded the cost" 0 "$K" stats
  contains "  with a model row"   "mock/model"
fi
kill "$MOCK_PID" 2>/dev/null; wait "$MOCK_PID" 2>/dev/null; MOCK_PID=""

# ── the real provider, only if asked ──────────────────────────────────────
if [ "$REAL" = 1 ]; then
  head1 "your real key"

  find_key() {
    # 1. an explicit file: a raw key, or a .env line
    if [ -n "$KEY_FILE" ]; then
      [ -f "$KEY_FILE" ] || { echo "KEYERR:no such file: $KEY_FILE"; return; }
      grep -ohE 'sk-or-v1-[A-Za-z0-9_-]+' "$KEY_FILE" | head -1 && return
      echo "KEYERR:no sk-or-v1-… key found in $KEY_FILE"; return
    fi
    # 2. a folder of keys: first file that contains one
    if [ -n "$KEY_DIR" ]; then
      [ -d "$KEY_DIR" ] || { echo "KEYERR:no such directory: $KEY_DIR"; return; }
      grep -rohE 'sk-or-v1-[A-Za-z0-9_-]+' "$KEY_DIR" 2>/dev/null | head -1 && return
      echo "KEYERR:no sk-or-v1-… key found under $KEY_DIR"; return
    fi
    # 3. the environment of whoever ran this script
    [ -n "${SMOKE_ENV_KEY:-}" ] && { echo "$SMOKE_ENV_KEY"; return; }
    # 4. the real config file, read-only
    if [ -f "$REAL_HOME/.config/kolk/config.json" ]; then
      grep -ohE 'sk-or-v1-[A-Za-z0-9_-]+' "$REAL_HOME/.config/kolk/config.json" | head -1 && return
    fi
    echo "KEYERR:no key given. Use --key-file PATH, --key-dir DIR, or export OPENROUTER_API_KEY"
  }

  KEY="$(find_key)"
  case "$KEY" in
    KEYERR:*) no "found a key" "${KEY#KEYERR:}" ;;
    "")       no "found a key" "the search came back empty" ;;
    *)
      ok "found a key ($(mask "$KEY"))"
      export OPENROUTER_API_KEY="$KEY"
      M="${MODEL:-openrouter/auto}"
      note "model: $M"

      check "kolk models reaches OpenRouter" 0 "$K" models
      lines="$(printf '%s\n' "$LAST_OUT" | wc -l | tr -d ' ')"
      [ "$lines" -gt 10 ] && ok "  listed $lines models" || no "  listed only $lines lines"

      cd "$SCRATCH/work"
      check "one real chat turn" 0 "$K" --mode chat -m "$M" -p "Reply with exactly the word: kolkrabbi"
      case "$LAST_OUT" in
        *kolkrabbi*|*Kolkrabbi*) ok "  the model answered" ;;
        *) no "  the model answered" "got: $(printf '%s' "$LAST_OUT" | head -2 | tr '\n' ' ')" ;;
      esac
      cd "$REPO"

      # Code mode against a real model: the whole agentic loop, not just
      # streaming. A chat turn proves the transport; only this proves that a
      # real model can drive real tools to a real change on disk.
      head1 "a real code-mode turn"
      CODE_DIR="$SCRATCH/work/code"
      mkdir -p "$CODE_DIR"; cd "$CODE_DIR"

      check "creates a file with tools" 0 "$K" -m "$M" -y -p \
        'Create a file named kolk-smoke.txt whose entire contents are the single line: it works. Use your tools. Do not ask for confirmation.'
      if [ -f "$CODE_DIR/kolk-smoke.txt" ]; then
        ok "  the file exists"
        if grep -qi 'it works' "$CODE_DIR/kolk-smoke.txt"; then
          ok "  with the right contents"
        else
          no "  with the right contents" "got: $(head -1 "$CODE_DIR/kolk-smoke.txt")"
        fi
      else
        no "  the file exists" "no kolk-smoke.txt in $CODE_DIR — the model may not support tool calling; try --model with one that does"
      fi

      if [ -f "$CODE_DIR/kolk-smoke.txt" ]; then
        check "edits the file it just made" 0 "$K" -m "$M" -y -p \
          'In kolk-smoke.txt, change the words "it works" to "it really works". Use your tools. Do not ask for confirmation.'
        if grep -qi 'it really works' "$CODE_DIR/kolk-smoke.txt"; then
          ok "  the edit landed"
        else
          no "  the edit landed" "contents now: $(head -1 "$CODE_DIR/kolk-smoke.txt")"
        fi
      fi

      check "checkpoints recorded the changes" 0 "$K" sessions
      cd "$REPO"

      check "stats recorded the real call" 0 "$K" stats
      case "$LAST_OUT" in
        *'$'*) ok "  with a cost" ;;
        *)     no "  with a cost" "no cost column in the stats output" ;;
      esac
      printf '\n%s%s%s\n' "$dim" "$(printf '%s' "$LAST_OUT" | grep -E '^(MODEL|TOTAL|[a-z].*/)' | head -6)" "$rst"
      unset OPENROUTER_API_KEY
      ;;
  esac
else
  head1 "your real key"
  note "skipped — pass --real to run one cheap turn against OpenRouter"
fi

# ── verdict ───────────────────────────────────────────────────────────────
printf '\n%s%d passed%s' "$grn" "$pass" "$rst"
[ "$fail" -gt 0 ] && printf ', %s%d failed%s' "$red" "$fail" "$rst"
printf '\n'
[ "$fail" -gt 0 ] && exit 1
printf '%skolk works.%s\n' "$grn" "$rst"
exit 0
