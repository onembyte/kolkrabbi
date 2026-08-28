# Build log

What has actually been built, step by step, against the migration checklist in
`docs/plan/02-architecture.md` §12. The plan says what to do; this file says
what is done, how it was verified, and what changed along the way.

One line per step. Verification is a command someone else can re-run.

---

## Step 3 — split `cmd/kolk/main.go` into `internal/cli/*`

**Status:** done, 2026-08-22 · **Tests:** 22 → 44 · **Binary:** 5.82 MB · `go vet` clean

`main.go` went from 606 lines to 21. Everything moved per the §4 table:

| From | To |
|---|---|
| `main()` flag loop, session/model resolution, engine construction | `internal/cli/run.go` |
| `main()` dispatch, `printUsage`, `printJSON`, `orDefault`, `configDir`, `sessionsDir` | `internal/cli/cli.go` |
| `runREPL`, `yoloTag` | `internal/cli/repl.go` |
| `handleSlash` | `internal/cli/slash.go` |
| `runConfigCmd`, `saveCfg`, `maskKey` | `internal/cli/cmd_config.go` |
| `runStatsCmd` | `internal/cli/cmd_stats.go` |
| `runSessionsCmd` | `internal/cli/cmd_sessions.go` |
| `runModelsCmd`, `formatPricing` | `internal/cli/cmd_models.go` |
| `fatal` | `internal/cli/exit.go`, as an exit-code table |
| `resolveBaseURL` | `internal/config/resolve.go` |

### What the step added beyond the move

- **`cli.Main(ctx, args) int`.** Nothing below `cmd/` calls `os.Exit` any more, which
  is what makes the surface testable in-process instead of by subprocess.
- **The command table** (`cli.go`) is the single source for dispatch, `kolk help`,
  and the generated "usage:" line each command prints when misused. `kolk help
  <command>` renders a verb's grammar from the same table. A command cannot exist
  undocumented, and a usage string cannot drift from what dispatch accepts —
  both are asserted by tests rather than promised.
- **The flag table** (`flags.go`) is the single source for parsing and for the
  Flags section of help, with the same guarantee.
- **Exit codes** (`exit.go`): 0 ok · 1 error · 2 usage · 3 budget (reserved for
  saga) · 130 interrupt. `UsageError`, `BudgetError` and `GuidedError` map onto
  them; `exitCode` unwraps, so a wrapped `context.Canceled` still exits 130.
- **`GuidedError`** exists to keep the North star honest: a first-run failure
  must end in a line the user can paste. The type carries the hint lines, so the
  obligation is structural rather than remembered.
- **Streams are injected** (`app{stdout, stderr, in}`). One shared `bufio.Reader`
  for stdin, because the REPL and tool confirmations must not each buffer it.

### Deliberate behaviour changes

Three, all in the direction of catching mistakes earlier:

1. **An unknown flag is now exit 2, not prompt text.** `kolk --mdoel gpt-4` used
   to append `--mdoel gpt-4` to the prompt and bill the default model.
2. **A flag missing its value is now exit 2.** `kolk -m` used to be ignored.
3. **`--` ends flag parsing** and `--long=value` is accepted, which together are
   the only way to write a prompt or a value that starts with a dash.

`kolk fix the failing test` still reaches the prompt: dispatch only diverts on an
exact command-table hit.

### Verification

```sh
go build ./... && go vet ./... && go test ./...        # 44 tests, all green
go run ./cmd/kolk-mock                                 # prints a URL
kolk --base-url <url> -y -p "create the hello file"    # in a scratch dir
```

The mock run was done end-to-end against the built binary: the turn streamed, the
`write_file` tool executed, `hello-from-mock.txt` appeared, the session was saved
and `kolk stats` reported 2 calls / 230 tokens / $0.0019. `kolk help`, `kolk help
config`, `kolk sessions`, `kolk stats`, `kolk config set-key`/`show` and the
bad-flag and no-key paths were each run and their exit codes checked by hand.

### Not done here, on purpose

`configDir()`/`sessionsDir()` are still the prototype's hardcoded `~/.config/kolk`,
sitting in `cli.go`. They become `internal/paths` at step 5 with the XDG split.
`maskKey` likewise becomes `secret.Redact` at step 5. `internal/engine` was not
touched at all: mode dispatch is frozen until `docs/plan/06-modes.md` lands.

---

## Step 5 / L0.5 — terminal facts

**Status:** done, 2026-08-23 · **Tests:** 197 → 198 · **Binary:** 6.14 MB ·
**Cold start p50:** 5.0 ms · `go vet` clean

`internal/term` now owns three facts needed by every terminal surface: whether a file is an actual
terminal, whether color is wanted, and the safe output width. It does not render ANSI, manage a
TUI, or change CLI output.

### TDD record

**Red:** `TestIsTerminalRejectsTheNullDevice` opened `os.DevNull` and failed because the initial
implementation equated `os.ModeCharDevice` with a terminal:

```text
--- FAIL: TestIsTerminalRejectsTheNullDevice
    term_test.go:104: /dev/null is a character device, not a terminal
```

**Green:** `IsTerminal` now delegates to native console probes isolated by build target:

- Darwin: `unix.IoctlGetTermios(..., unix.TIOCGETA)`
- Linux: `unix.IoctlGetTermios(..., unix.TCGETS)`
- Windows: `windows.GetConsoleMode`
- other targets: an explicit false-returning stub until that OS has a real probe

This uses `golang.org/x/sys v0.47.0`, the dependency already reserved exclusively for
`internal/term` by the architecture allow-list. The root dependency count is now 1.

**Refactor:** the portable color and width policy remains in `term.go`; only the OS question moved
behind `isTerminal`. The generic package has no platform constants or build constraints.

### Verification

```sh
go test ./internal/term -count=1
go test -race ./internal/term -count=1
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go test -c ./internal/term
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go test -c ./internal/term
make fmt-check && make vet && make arch && make purity && make buildtags && make budgets
./scripts/test.sh
```

All gates passed. The full suite reports 198 tests. The architecture, purity, build-tag, 20 MB
binary, 30 ms cold-start, test-floor, and dependency-count gates are green.

### Not done here, on purpose

Live terminal resize, ANSI/VT enablement, rendering, and TUI behavior belong to migration step 14
and PLAN item 11. File locking is the separate L0.7 checkpoint. No mode, config, engine, or CLI
behavior changed.

---

## Step 5 / L0.6 — sortable identifiers

**Status:** done, 2026-08-23 · **Tests:** 198 → 211 · **Binary:** 6.14 MB ·
**Cold start p50:** 5.1 ms · `go vet` clean

`internal/xid` now generates self-describing, lexically sortable ULIDs for sessions, turns, events,
tool calls, and delegated tasks. It owns no persistence and has no caller yet; this checkpoint
stabilizes the primitive before the protocol and session-format cuts depend on it.

### TDD record

Three boundaries were demonstrated red independently:

1. `Valid`, `Time`, and `KindOf` accepted a bare ULID, an empty prefix, and arbitrary prefixes such
   as `unknown_…`, despite the package contract requiring typed IDs.
2. `New(Kind("unknown"))` emitted an ID that the corrected validator rejected.
3. An all-`FF` 80-bit monotonic counter wrapped to zero and moved the next ID backward.

**Green:** one `split` parser now owns kind and body validation for all three query functions.
`New` rejects unknown programmer constants at its boundary. Counter overflow advances the logical
millisecond and draws fresh entropy instead of wrapping backward; ordinary generation retains all
80 entropy bits.

**Interoperability:** `TestOfficialULIDVector` checks both directions against
`01ARYZ6S41TSV4RRFFQ69G5FAV` / `1469918176385`, published by the canonical ULID implementation,
and verifies the specification's case-insensitive input rule. This prevents an encoder and decoder
with the same private mistake from passing only by agreeing with each other.

**Refactor:** validation of known kinds, length, Crockford alphabet, and the two overflow padding
bits is centralized. A fuzz target asserts that `Time`, `Valid`, and `KindOf` agree for arbitrary
bytes.

### Verification

```sh
go test ./internal/xid -count=1
go test -race ./internal/xid -count=1
go test ./internal/xid -run '^$' -fuzz '^FuzzParserConsistency$' -fuzztime=2s
make fmt-check && make vet && make arch && make purity && make buildtags && make budgets
./scripts/test.sh
```

All gates passed. The fuzz run executed 256,046 inputs without a panic or parser disagreement. The
full suite reports 211 tests; architecture, purity, build-tag, binary, startup, test-floor, and
dependency-count budgets remain green.

### Not done here, on purpose

No existing session filename or stored row was migrated. The session format adopts these IDs at
migration step 10; protocol envelopes adopt them at steps 6–7. Cross-process total ordering is not
claimed: the later event bus owns sequence numbers for replay and merging.

---

## Step 5 / L0.7 — cross-process file locking

**Status:** done, 2026-08-23 · **Tests:** 211 → 217 · **Binary:** 6.14 MB ·
**Cold start p50:** 6.4 ms · `go vet` clean

`internal/lock` now serializes file-backed read-modify-write operations across independent
Kolkrabbi processes. Immediate `Try` and context-bounded `Acquire` share the same exclusive OS lock;
`BusyError` carries the holder PID so credential and login commands can give a useful next action.

### TDD record

**Red:** the first contract test did not compile because `Try`, `Acquire`, `File`, `ErrBusy`, and
`BusyError` did not exist. It specified the complete smallest behavior before implementation: mode
`0600`, one owner, typed contention with a PID, idempotent close, and reacquisition.

**Green:** Darwin and Linux use non-blocking `flock(2)`. `Acquire` retries with bounded backoff until
its context is cancelled or expires. The owner truncates and syncs its PID only after taking the OS
lock; stale PID text is therefore diagnostic only and can never create ownership. Every failure
path closes its descriptor.

**Cross-process proof:** a child test process acquires the lock and waits on stdin. The parent sees
`ErrBusy` with the child's real PID, then closes the pipe; process exit releases the kernel lock and
the parent immediately reacquires it. The lock path deliberately remains. Removing it would allow
two processes to lock different inodes.

**Refactor:** platform operations are two private functions behind build tags. Windows compiles to
an explicit `ErrUnsupported` boundary until migration step 13 adds and exercises `LockFileEx`; it
does not pretend an in-memory mutex protects separate Windows processes.

### Verification

```sh
go test ./internal/lock -count=1
go test -race ./internal/lock -count=1
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go test -c ./internal/lock
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go test -c ./internal/lock
make fmt-check && make vet && make arch && make purity && make buildtags && make budgets
./scripts/test.sh
```

All gates passed. The full suite reports 217 tests. Architecture, purity, build tags, binary size,
startup time, test floor, and dependency count remain within budget.

### Not done here, on purpose

The credential manifest consumes this package in owner-trial checkpoint T0.1. No existing writer
was changed here, and there is no distributed/network-filesystem locking claim. Windows runtime
support stays a named step-13 task rather than an untested promise.

---

## Step 5 / L0.8 — platform-boundary closure

**Status:** done, 2026-08-23 · **Tests:** 217 · **Binary:** 6.14 MB ·
**Cold start p50:** 6.1 ms · **Root dependencies:** 1 · `go vet` and lint clean

Architecture migration step 5 is closed. The L0 boundary now consists of `paths`, `shell`,
`atomicfile`, `lock`, `term`, `secret`, `xid`, and `buildinfo`; every package is named in the
architecture table and the complete root module compiles for every CLI release target.

### TDD record

**Red:** `make platforms` was added to the Makefile and CI first. It failed with:

```text
make: ./scripts/check-platforms.sh: No such file or directory
make: *** [platforms] Error 1
```

**Green:** `scripts/check-platforms.sh` compiles every root package and test with `CGO_ENABLED=0`
for Darwin amd64/arm64, Linux amd64/arm64, and advisory Windows amd64. Foreign test binaries are
passed through `-exec=true`: compilation is real, but the gate never pretends foreign tests ran.
The ordinary host suite remains the separate `scripts/test.sh` gate.

**Refactor:** `make check` and CI's named guard-rail job both call the same script. The architecture
document's stale prototype measurements and impossible “zero dependencies below the surface” claim
were replaced with measured closure values and the mechanically true allow-list claim.

### Verification

```sh
make platforms
make check
```

The combined gate ran formatting, vet, all 217 host tests, architecture, purity, build-tag checks,
the five-target compile matrix, lint, and budgets. Everything passed. The binary is 6.14 MB, cold
start p50 is 6.1 ms, and the root graph has one dependency (`golang.org/x/sys`).

### Next checkpoint

T0.1 builds the locked, atomic `0600` credential manifest. It is the first direct implementation
step toward the owner's public install → `kolk` → API-key onboarding trial flow.

---

## Owner trial / T0.1 — credential manifest

**Status:** done, 2026-08-23 · **Tests:** 217 → 236 · **Binary:** 6.14 MB ·
**Cold start p50:** 4.9 ms · **Root dependencies:** 1 · `go vet` and lint clean

`internal/keystore` now owns the version-1 provider/profile routing manifest. The default file
backend stores one tagged base64 value per slot, while list and probe return only metadata. Every
mutation takes the caller-bounded cross-process lock and commits through the durable atomic writer.

### TDD record

**Red:** the first concurrency contract did not compile because `keystore.Ref`, `NewFileStore`, and
the provider/profile store API did not exist. That test defined the smallest public boundary before
implementation and required 40 independent store instances to retain every update.

**Green:** the manifest now normalizes slots such as `OpenRouter` to `openrouter/default`, rejects
unknown versions and backends with typed errors, caps raw values at 2,560 bytes, and encodes them as
`kolk-b64:` plus standard base64. New files land at `0600` inside a `0700` directory; existing loose
modes are tightened, and symlinks or non-regular targets are refused. Empty and cancelled writes
do not create credential state.

**Cross-process proof:** eight child test processes wrote distinct credentials into the same file.
All eight entries survived in valid, sorted JSON. The focused race run also exercised 40 concurrent
in-process writers, and the lock-timeout test proved a cancelled contender cannot mutate the file.

**Refactor:** the superseded raw-string `secret.FileStore` was deleted. Credential persistence and
its `Store` interface now live only in `internal/keystore`; an architecture test permanently
forbids `internal/secret` from importing OS or filesystem packages. Errors derived from corrupt
manifest values no longer quote those values.

### Verification

```sh
go test ./internal/keystore -count=1
go test -race ./internal/keystore -count=1
./scripts/test.sh
make fmt-check vet arch purity buildtags platforms lint budgets
```

All gates passed. The host suite reports 236 tests. The complete module compiles for Darwin and
Linux on amd64/arm64 and for advisory Windows amd64. Lint reports zero issues; the binary, startup,
test-floor, and dependency-count budgets remain green.

### Not done here, on purpose

Base64 is transport-safe encoding, not encryption; the manifest is protected by owner-only file
permissions. No CLI, inference, provider verification, legacy-config migration, keychain, DPAPI,
helper, OAuth, or first-run behavior changed. T0.2 owns the provider-agnostic `kolk key` command.

---

## Owner trial / T0.2a — credential shape classifier and mask

**Status:** done, 2026-08-23 · **Tests:** 236 → 272 · **Binary:** 6.14 MB ·
**Cold start p50:** 4.8 ms · **Root dependencies:** 1 · `go vet` and lint clean

`internal/redact` now owns a versioned embedded `keyshapes.json` table. Classification returns only
safe provider/denial facts, evaluates every deny rule first, applies length and alphabet constraints,
and selects only the longest valid inference prefix. It never retains its input.

### TDD record

**Red:** the table-driven tests failed to compile because `Classify`, `Classification`, the five
denial kinds, the injectable rule evaluator, and `Mask` did not exist. The red matrix named all 15
supported inference forms and all 15 forbidden forms before implementation.

**Green:** the embedded table covers OpenRouter, Anthropic API keys, three OpenAI forms plus the
legacy catch-all, Google, Groq, xAI, Perplexity, Fireworks, Cerebras, Nvidia, Replicate, and
Hugging Face. Claude subscription tokens, GitHub tokens, AWS access IDs, Slack tokens, and private
keys are classified as denials before inference. Synthetic overlapping rules prove longest-prefix
selection and equal-length ambiguity.

**Refactor:** `redact.Mask` now reveals a known shape prefix and last four only when at least eight
bytes remain hidden. Unknown long values reveal at most four bytes at each end; shorter values show
only an ellipsis. `secret.Redact` delegates to this one implementation, fixing the old overlapping
slice behavior without changing the secret type's safe printing contract.

### Verification

```sh
go test -race ./internal/redact -count=1
./scripts/test.sh
make fmt-check vet arch purity buildtags platforms lint budgets
```

All gates passed: 272 host tests, all five compile targets, zero lint issues, and every budget green.
No CLI or network behavior changed in this slice. T0.2b owns the OpenRouter verification request.

---

## Owner trial / T0.2b — OpenRouter key verifier

**Status:** done, 2026-08-23 · **Tests:** 272 → 281 · **Binary:** 6.14 MB ·
**Cold start p50:** 4.9 ms · **Root dependencies:** 1 · `go vet` and lint clean

`provider.OpenRouterVerifier` now makes one authenticated `GET /api/v1/key` under a hard two-second
deadline and parses `limit`, `limit_remaining`, `usage`, and `is_free_tier` into safe metadata. The
response shape was rechecked against OpenRouter's current official API reference before the tests
were frozen.

### TDD record

**Red:** the offline verifier matrix failed to compile because the verifier, result, and typed
rejection/verification errors did not exist. It specified method, URL, bearer delivery, deadline,
current JSON fields, cancellation, empty input, failure classification, and canary scrubbing.

**Green:** authentication is attached only inside `secret.AuthTransport`; response bodies are
bounded to 1 MiB and closed on every path. HTTP 401 is `ErrKeyRejected`; transport, server, redirect,
oversized, and invalid-response failures are `ErrKeyVerification`. No error includes response body
contents or the key.

**Refactor/security audit:** the copied HTTP client refuses every redirect. This is load-bearing:
an authentication transport would otherwise attach the credential again to a redirected request.
An offline redirect test proves a second host is never contacted. Static analysis then caught and
closed one missing error wrap and made response ownership explicit in the fixtures.

### Verification

```sh
go test -race ./internal/provider -count=1
./scripts/test.sh
make fmt-check vet arch purity buildtags platforms lint budgets
```

All gates passed: 281 host tests, all five compile targets, zero lint issues, and every budget green.
The verifier has no CLI caller yet; T0.2c owns that wiring.

---

## Owner trial / T0.2c — `kolk key` CLI path

**Status:** done, 2026-08-23 · **Tests:** 281 → 294 · **Binary:** 6.19 MB ·
**Cold start p50:** 4.5 ms · **Root dependencies:** 1 · `go vet` and lint clean

`kolk key <API_KEY>` now classifies a positional credential before any side effect, infers its
provider, verifies OpenRouter through the bounded T0.2b verifier, and atomically writes
`openrouter/default` to the T0.1 manifest. Explicit `-` reads stdin; `<provider> <key|->` is the
working escape for a provider whose shape is not known yet.

### TDD record

**Red:** the command contract first failed to compile because the app had no verifier/store seams,
the keystore had no safe write-metadata input, and `runKey` did not exist. The initial matrix fixed
the success, offline, denied, unknown, CI, explicit-provider, write-failure, and argument-count
behavior before production wiring. Two audit tests then failed independently: CI dropped an
explicit provider from its suggested stdin command, and an unknown-format key echoed by a store
error escaped the global shape scrubber.

**Green:** the command accepts only one value or one provider/value pair. Denials and ambiguous
positional shapes exit 2 without resolving directories, contacting a verifier, or creating a
manifest. Positional values are refused when `CI` is set, with an executable stdin alternative.
OpenRouter success records the verification instant and safe account facts; an offline verifier is
a redacted warning after the credential is stored. Store failures exit 1 with a key-free re-paste
command.

**Refactor/security audit:** provider names pass through `keystore.NormalizeRef` before they appear
in guidance. The store error path applies both the global shape scrubber and an exact-value mask, so
the explicit-provider escape cannot introduce a new leak. `SetWithMetadata` commits source and
verification facts in the same lock-protected atomic manifest update, and its direct UTC round-trip
test keeps those facts metadata-only.

### Verification

```sh
go test -race ./internal/cli ./internal/keystore -count=1
./scripts/test.sh
make fmt-check vet arch purity buildtags platforms lint budgets
```

All gates passed: 294 host tests, all five compile targets, and zero lint issues. The binary is
6.19 MB, cold-start p50 is 4.5 ms, and the root dependency graph still contains one module.

### Not done here, on purpose

The prototype `config.json.api_key` still exists in the old config schema, and `kolk config
set-key` still follows that legacy path. T0.2d owns evacuating it without overwrite or loss. T0.3
will then make a bare `kolk` read the manifest and print the exact first-run guidance when no key is
available.

---

## Owner trial / T0.2d — legacy credential evacuation

**Status:** done, 2026-08-23 · **Tests:** 294 → 303 · **Binary:** 6.21 MB ·
**Cold start p50:** 4.7 ms · **Root dependencies:** 1 · `go vet` and lint clean

The prototype `config.json.api_key` now moves into `openrouter/default` under the credential
manifest lock before config is atomically rewritten without the field. `internal/config.Config`
has no credential-shaped field, cannot import `internal/secret` or `internal/keystore`, and every
new config serialization is credential-free by construction.

### TDD record

**Red:** the migration fixture did not compile because `MigrateLegacyConfig` and
`ErrMigrationConflict` did not exist. Independent tests also showed `Config` still exposed and
wrote `api_key`, while `kolk config set-key` returned success and created config state. The final
side-effect audit caught a malformed `config set-tier` invocation migrating a credential even
though the settings command itself was invalid.

**Green:** migration preserves every JSON setting it does not own, copies the legacy key before
removing the source field, records `source: legacy-config`, and becomes a no-op on the second run.
An identical manifest key lets cleanup finish without rewriting credential metadata; a different
key returns the typed conflict and leaves both values untouched. The old CLI spelling is an exit-2,
key-free redirect to `kolk key <API_KEY>` before directory resolution.

**Refactor/security audit:** valid state-writing commands are the only migration triggers. Config
symlinks remain symlinks while their targets are cleaned. A forced config rewrite failure proves
the manifest copy already exists and the source file still retains its copy. Runtime help, provider
errors, README setup, and the smoke contract now name the one supported command.

### Verification

```sh
go test -race ./internal/keystore ./internal/config ./internal/cli -count=1
./scripts/test.sh
make fmt-check vet arch purity buildtags platforms lint budgets
```

All gates passed: 303 host tests, all five compile targets, and zero lint issues. The binary is
6.21 MB, cold-start p50 is 4.7 ms, and the root graph still has one dependency.

### Next checkpoint

At the owner's request, W0.1 adds the static purple retro-octopus landing page and records the exact
Cloudflare Pages setup before T0.3 resumes the local first-run path. This is an additive static
surface; the installer itself remains T0.4.

---

## W0.1 — static landing page and Cloudflare handoff

**Status:** deployed, 2026-08-23 · **Site contract:** 44/44 · **App tests:** 303 ·
**Binary:** 6.21 MB · **Cold start p50:** 4.6 ms · `go vet` and lint clean

`site/` is now a framework-free Cloudflare Pages output directory. Its original eight-arm pixel
octopus, black/violet grid, oversized type, and terse terminal composition take the requested cue
from Omarchy's visual restraint without reusing Omarchy assets. The page explains the exact
install → first run → key → run flow and labels the installer honestly as a v0.1 deliverable.

### TDD and visual record

**Red:** the independent site contract began at 36/36 failures because no deploy directory existed.
The test fixed the required semantic HTML, exact commands, no-external-resource rule, responsive
and focus hooks, logo accessibility, purple identity, and Cloudflare header policy before the page
was written.

**Green:** static HTML, CSS, SVG, favicon, 404, robots, and `_headers` files satisfy those checks
with no JavaScript, package manager, framework, analytics, cookie, or external font/image request.
The deployment document checks the production branch, build command, output directory, hostname,
wildcard preservation, TrueNAS-origin ownership, and safe custom-domain ordering, bringing the
contract to 44.

**Visual refactor:** the first rendered mascot had only six distinct arms and clipped its right curl.
The accepted SVG has eight separate arms, a bounded shadow, crisp pixel edges, and a legible
silhouette. A 1600-pixel Quick Look render verified the full desktop hero with local assets. Quick
Look does not expose a true mobile viewport; mobile behavior remains covered structurally by the
640/900-pixel CSS breakpoints and content contract until a browser-based visual suite is justified.

### Live-domain finding

Read-only DNS and HTTPS checks found that `kolkrabbi.francomichetti.com` already resolves through
Cloudflare. Both `/` and `/install.sh` return `302 /login` followed by a cached Next.js HTML page.
The owner confirmed that response is the intentional `*.francomichetti.com` wildcard multitenant
fallback backed by the owner's TrueNAS server, not an exact Kolkrabbi binding. No DNS, Worker,
Pages, TrueNAS, or application binding was changed. The deployment contract now keeps the wildcard
and TrueNAS route untouched and adds only the exact `kolkrabbi` Pages hostname; it also records the
conditional no-script exception needed if a wildcard Worker route, rather than wildcard DNS alone,
fronts the multitenant application.

### Verification

```sh
make site
bash -n scripts/test-site.sh
make check
```

The full gate passed: 303 app tests, architecture/purity/build-tag checks, five target builds, zero
lint issues, every budget, and 44 site checks. [`docs/cloudflare-pages.md`](cloudflare-pages.md)
records the exact Pages values (`main`, `exit 0`, `site`) and the reversible dashboard cutover.

The owner connected the Pages project to `main`, added the exact proxied `kolkrabbi` CNAME while
leaving the wildcard TrueNAS route unchanged, and reported the custom domain **Active**. A direct
public HTTPS check then returned HTTP 200 with the deployed octopus page, CSP, permissions policy,
no-referrer policy, MIME-sniffing protection, and frame denial. The owner visually accepted the
live page, closing the domain-root item in the owner-trial gate.

The post-deploy release audit caught one content error: the footer said MIT while the repository's
`LICENSE` is Apache-2.0. The site contract was changed first and failed 1/44, then the footer label
was corrected without changing its source link; all 44 checks returned green.

### Next checkpoint

T0.3 resumes the executable owner flow: a bare `kolk` must read the credential manifest, print the
exact three-line first-run guidance when absent, and start a computed-default session when present.

---

## Owner trial / T0.3 — first-run path

**Status:** done, 2026-08-23 · **Tests:** 303 → 308 · **Binary:** 6.21 MB ·
**Cold start p50:** 4.9 ms · **Root dependencies:** 1 · `go vet` and lint clean

A bare `kolk` now checks `OPENROUTER_API_KEY` and then the locked
`openrouter/default` file-manifest slot. No credential prints the owner's exact three-line next
action and exits 2; a stored credential builds the ordinary provider client and enters the existing
computed-default session without another setup step.

### TDD record

**Red A — first-run surface:** the exact-output test found all three prototype failures at once:
exit 1 instead of action-required exit 2, old environment/config-oriented wording instead of the
three promised lines, and creation of the data directory merely to discover that no key existed.

**Green A:** path location is now a read-only operation separated from state preparation. The
missing-key branch reads no config value beyond the optional settings file, writes no directory or
file, and uses an action-required guided error whose renderer adds neither `error:` nor the generic
help suffix.

**Red B — manifest boundary:** the stored-key matrix did not compile because `newAgent` had no
invocation context. A credential backend therefore could not honor cancellation. The matrix also
specified environment precedence over a deliberately corrupt manifest, a named hard error for
store corruption, computed defaults, and an offline streamed model turn.

**Green B:** one resolver checks the non-empty environment override before constructing a store,
then reads exactly `openrouter/default` under the caller's context. Only typed `ErrNotFound` becomes
first run; every other error remains wrapped, scrubbed, and actionable. The resolved `secret.Secret`
is revealed only at the existing provider-construction boundary.

**Refactor and leak audit:** directory location and preparation have distinct names and effects;
the default credential reference is local rather than mutable package state. The offline SSE
fixture observed the stored bearer credential and `openrouter/auto`, returned a successful answer,
and then scanned stdout, stderr, the session transcript, and every checkpoint file for the key.
None contained it. Existing engine defaults produced `code` and `standard` without config.

### Verification

```sh
go test ./internal/cli -run '^TestFirstRunWithoutAKeyIsExactAndReadOnly$' -count=1
go test ./internal/cli -run '^(TestStoredCredentialBuildsComputedDefaultAgent|TestEnvironmentCredentialWinsWithoutReadingCorruptManifest|TestCorruptManifestIsNotReportedAsMissingCredential|TestCanceledCredentialReadStopsTheRun|TestStoredCredentialCompletesOfflineDefaultTurn)$' -count=1
go test -race ./internal/cli -count=1
make check
```

All gates passed: 308 host tests, architecture/purity/build-tag checks, all five compile targets,
zero lint issues, a 6.21 MB binary, 4.9 ms cold-start p50, one root dependency, and 44 site checks.

### Next checkpoint

T0.4 owns the release boundary: versioned macOS/Linux archives, checksums, a reviewed installer,
and the exact public `/install.sh` route. Nothing in T0.3 claims that URL is ready.

---

## Owner trial / T0.4a — release artifact contract

**Status:** done, 2026-08-23 · **Host tests:** 308 · **Release contract:** 24 ·
**Snapshot contract:** 21 · **GoReleaser:** v2.17.1 · **Targets:** 4

The CLI release now has one deterministic archive for Darwin/Linux on amd64/arm64 and no Windows
asset. Every build is cgo-free and trimpath-stamped with version, full commit, and release date.
The archives contain `kolk`, README, and Apache-2.0 LICENSE; `checksums.txt` is explicitly SHA-256
and the tag workflow will produce a Cosign v3 `.sigstore.json` bundle over that manifest.

### TDD record

**Red — static contract:** the first release test failed 8/21 checks. The prior skeleton included
Windows and zip output, left archive names to defaults, relied on an implicit checksum algorithm,
and contained no signing command, signature bundle, or checksum-artifact binding.

**Green:** `.goreleaser.yaml` now names `kolk_{{ .Version }}_{{ .Os }}_{{ .Arch }}.tar.gz`, lists
only the four supported targets, states SHA-256, and follows GoReleaser's current Cosign v3 bundle
configuration for signing the checksum manifest once. A second red step required the fast static
contract to be called by both `make check` and CI; it failed 2/24 before those entries were added.

**Executable snapshot red:** GoReleaser's own validator accepted the YAML, but the first snapshot
aborted before building. The historical `proto-0` tag is intentionally not SemVer, while the old
snapshot template called `incpatch .Version`. Snapshot identity is now the explicit prerelease
`0.1.0-dev.<short-commit>`; real tag releases still derive their version from the semantic tag.

**Snapshot green:** the official GoReleaser v2.17.1 Darwin/arm64 validator was downloaded to a
temporary directory and matched its official SHA-256. The repeatable snapshot script then built
and inspected all four archives, rejected Windows/zip output, required the three archive members,
matched all four SHA-256 values, and executed the host artifact. It reported
`0.1.0-dev.3773c79` and `darwin/arm64` rather than `dev`.

### Verification

```sh
./scripts/test-release.sh
KOLK_GORELEASER_BIN=/path/to/goreleaser-v2.17.1 ./scripts/test-release-snapshot.sh
make check
```

The complete gate remains green: 308 host tests, five compile targets, zero lint issues, 6.21 MB,
4.3 ms cold-start p50, one dependency, 44 site checks, and 24 release checks. Primary configuration
references: https://goreleaser.com/customization/sign/sign/ and
https://goreleaser.com/customization/package/checksum/.

### Next slice

T0.4b builds and attacks the installer entirely offline. No tag, GitHub Release, public visibility,
or `/install.sh` production claim is made by T0.4a.

---

## Owner trial / R0.1 — v0.1 chat and code surface

**Status:** done, 2026-08-23 · **Host tests:** 308 → 313 · **Site contract:** 46 ·
**Surface contract:** 7 · **Binary:** 6.21 MB · **Cold start p50:** 4.4 ms

The first working deploy now exposes exactly `chat` and `code`. Plain `kolk` computes `code` as its
default, `--mode chat` selects tool-free conversation, and both the command line and REPL reject
the experimental `agent` value as a usage error. Help, README, and landing-page claims describe the
same two-mode release.

### TDD record

**Red:** the engine registry test reported `[chat code agent]`; the REPL changed its active mode to
`agent`; slash help advertised `<chat|code|agent>`; the site failed both its required two-mode copy
and unreleased-mode exclusion; and the new public-surface contract failed 6/7 checks across the
README and CLI help sources. The focused CLI rerun used its localhost fixture and independently
confirmed both REPL failures.

**Green:** the release registry now contains only `chat` and `code`, and the flag parser validates
against it before runtime setup. The REPL delegates to the same registry. Mode and effort help copy,
the README, metadata, hero, and capability card now make the code-default two-mode boundary
explicit. One small static contract is enforced by both `make check` and CI so those surfaces cannot
silently re-advertise a future mode.

**Preserved future work:** `ModeAgent`, `runOrchestrated`, and their scripted end-to-end tests remain
inside `internal/engine`. Direct internal fixtures can still exercise them, but no v0.1 flag, slash
command, help entry, README example, or website claim can select them. The code-mode tool-loop test
and both dormant orchestration tests passed independently after the boundary changed.

### Verification

```sh
go test ./internal/cli -run '^TestStoredCredentialBuildsComputedDefaultAgent$' -count=1
go test ./internal/engine -run '^TestE2E_ToolLoopWithPersistenceAndRewind$' -count=1
go test ./internal/engine -run '^TestE2E_(OrchestratedAgentMode|OrchestratorFallsBackOnSingleTask)$' -count=1
./scripts/test.sh
make fmt-check
make vet
make arch
make purity
make buildtags
make platforms
make lint
make budgets
make site
make surface
make release-check
```

All gates passed independently: 313 offline tests, five compile targets, zero lint issues, a 6.21
MB binary, 4.4 ms cold-start p50, one root dependency, 46 site checks, seven v0.1 surface checks,
and 24 release-artifact checks.

### Next checkpoint

T0.4b resumes the paused installer harness. It must remain fully offline until platform mapping,
version selection, checksum failure safety, archive validation, and atomic installation are green.

---

## Owner trial / T0.4b — offline installer

**Status:** done, 2026-08-23 · **Installer contract:** 56 · **Host tests:** 313 ·
**Site contract:** 46 · **Release contract:** 24 · **Bash:** 3.2 · ShellCheck clean

`site/install.sh` is now a runtime-free installer for the four CLI release targets. It discovers or
pins a semantic version, downloads the matching GoReleaser archive and SHA-256 manifest, validates
the archive before extracting, and atomically places a mode-`0755` `kolk` in an explicit or writable
PATH directory. It never invokes sudo and needs only standard macOS/Linux tools.

### TDD record

**Harness audit before Red:** the paused truncation test redirected a shortened script into stdin
while still passing the full installer filename to Bash, so it would have tested the wrong program.
That harness was corrected first. Default PATH placement, successful replacement, installed mode,
and Makefile/CI enforcement were also added before production code.

**Red:** with no `site/install.sh`, the focused target failed 13/15 checks: missing script, shell
contract, release origin, version and destination controls, checksum tools, private staging, and
final execution sentinel. It stopped before the black-box matrix, so no fake download could hide
the missing implementation.

**Green:** a definition-only Bash 3.2 script now waits until its final `main "$@"`. Platform and
pinned-version validation happen before downloads. Latest discovery accepts only GitHub's expected
`releases/tag/v<semver>` redirect. Downloads require HTTPS/TLS, bounded connection/retry behavior,
and the selected archive's single lowercase SHA-256 entry. The install file is staged in the target
directory and renamed only after every check.

**Adversarial refactor:** the matrix covers Darwin/Linux on both architectures, latest and pinned
versions, a conventional user PATH directory, an existing-binary upgrade, unsupported OS, unsafe
version, relative destination, tampered and missing hashes, an unexpected member, a symlink member,
and a genuinely truncated stdin stream. Every failure leaves the destination untouched; every run
removes its private staging directory. The local fixture PATH excludes Homebrew coreutils, proving
stock macOS uses `shasum`; Linux CI exercises `sha256sum`. Signal handling converges on the same EXIT
cleanup path. ShellCheck reports no issue.

### Verification

```sh
bash -n site/install.sh
bash -n scripts/test-installer.sh
shellcheck --shell=bash --severity=style site/install.sh scripts/test-installer.sh
make installer
make site
make surface
make release-check
make check
```

The complete gate passed: 313 offline Go tests, five compile targets, zero lint issues, a 6.21 MB
binary, 4.6 ms cold-start p50, one root dependency, 46 site checks, seven two-mode checks, 56
installer checks, and 24 release checks.

### Not ready for owner testing yet

The script contract is green, but the exact three-command flow is not. No tag-only release workflow
or public `v0.1.0` assets exist, and the GitHub repository is still private. T0.4c builds the release
workflow; T0.4d requires explicit owner approval before any visibility change or public tag, and
T0.5 performs the clean-machine rehearsal.

---

## Owner trial / T0.4c — tag-only signed release workflow

**Status:** done, 2026-08-23 · **Workflow contract:** 41 · **Snapshot contract:** 21 ·
**actionlint:** v1.7.12 · **GoReleaser:** v2.17.1 · **Cosign:** v3.0.6

`.github/workflows/release.yml` now reacts only to pushed `v*` tags. Its read-only `verify` job
accepts only strict v-prefixed Semantic Versions, runs `make check`, validates GoReleaser, and
rebuilds/inspects the four release archives. A dependent `publish` job alone receives
`contents: write` and `id-token: write`, then creates the GitHub Release and keyless Cosign bundle
with the repository-scoped token.

### TDD record

**Red:** with no workflow or tag guard, the first contract failed 24/26 checks. It identified the
missing tag-only trigger, default-deny permission boundary, read/write job separation, repository
identity guard, verification commands, immutable action pins, fixed tool versions, clean publish
command, and ordinary-CI enforcement before any YAML was written.

**Green:** `scripts/check-release-tag.sh` implements SemVer 2.0 core, prerelease, and build syntax,
including the no-leading-zero rule for numeric core/prerelease identifiers. The workflow starts
with `permissions: {}`; `verify` grants only `contents: read`, while `publish` depends on it and
adds only the release-write and OIDC permissions. Checkout does not persist credentials.

**Supply-chain refactor:** current official documentation was rechecked on 2026-08-23. Checkout v6,
Setup Go v6, GoReleaser Action v7, and Cosign Installer v4.1.2 were resolved from their official Git
tags to full 40-character commits. Cosign is separately fixed to v3.0.6 and GoReleaser to the
already snapshot-tested v2.17.1. The current official actionlint v1.7.12 Darwin/arm64 archive was
downloaded temporarily and matched its published SHA-256
`aba9ced2dee8d27fecca3dc7feb1a7f9a52caefa1eb46f3271ea66b6e0e6953f`; it accepted both workflows.

### Verification

```sh
bash -n scripts/check-release-tag.sh
shellcheck --shell=bash --severity=style scripts/check-release-tag.sh scripts/test-release-workflow.sh
make release-workflow-check
actionlint .github/workflows/ci.yml .github/workflows/release.yml
goreleaser check
KOLK_GORELEASER_BIN=/path/to/goreleaser-v2.17.1 ./scripts/test-release-snapshot.sh
make check
```

All 41 workflow checks passed. The executable snapshot produced exactly Darwin/Linux × amd64/arm64
at `0.1.0-dev.c76ebfc` and passed 21 archive/checksum checks. The complete gate remains green: 313
Go tests, five compile targets, zero lint issues, 6.21 MB, 4.5 ms p50, 46 site checks, seven
two-mode checks, 56 installer checks, and 24 release checks.

### Live repository settings and remaining owner decision

Read-only GitHub API checks report Actions enabled, all actions allowed, default workflow permission
`read`, pull-request approval disabled, default branch `main`, and repository visibility `private`.
No setting, tag, or release was changed. The official references used were
https://github.com/goreleaser/goreleaser-action,
https://github.com/sigstore/cosign-installer/releases, and
https://github.com/rhysd/actionlint/releases.

T0.4d is now the only remaining cutover slice. An unauthenticated installer cannot download release
assets from the private repository. Changing visibility and pushing `v0.1.0` are intentionally
blocked on explicit owner approval; keeping the source private instead requires a different public
artifact origin and an installer contract change.

---

## Owner trial / T0.4d1 — signed public-release verifier

**Status:** done, 2026-08-23 · **Verifier contract:** 30 · **Host tests:** 313 ·
**ShellCheck:** clean · **actionlint:** v1.7.12

`scripts/verify-release.sh` now provides one fail-closed verdict for a published tag. It validates
strict v-prefixed SemVer before any network operation, downloads the checksum manifest and Sigstore
bundle first, and asks Cosign to authenticate the exact tag-bound release workflow identity and
GitHub Actions OIDC issuer. Only then does it accept and download the four expected Darwin/Linux ×
amd64/arm64 archives.

### TDD record

**Red:** before the production script existed, the focused contract failed 12/14 checks. It named
the absent Bash/fail-closed boundary, fixed release origin and bundle, exact OIDC issuer and workflow
identity, Cosign verification, final execution sentinel, and post-publish workflow call. The
black-box matrix correctly refused to run without an implementation.

**Green:** the verifier now requires exactly one lowercase SHA-256 for every expected archive and
rejects unknown, missing, duplicate, or malformed manifest rows. Each downloaded digest is checked
against the authenticated manifest. Tar inspection then permits exactly one regular `kolk`,
`README.md`, and `LICENSE`, with no link or extra path. Finally, the host archive is extracted into
the private staging directory and its `kolk version` output must match the requested release and
host OS/architecture rather than `dev`.

**Adversarial refactor:** 30 offline checks prove invalid tags make no request, failed signatures
stop before archive downloads, exactly four archives are fetched, tampered bytes fail, a fifth
asset fails, duplicate/missing and malformed manifest rows fail, a missing download fails, an
unexpected archive member fails, and an unstamped host build fails. The release workflow invokes
this same script only after GoReleaser reports a successful publish. Ordinary CI and `make check`
enforce the contract before any tag can reach that workflow.

### Verification

```sh
bash -n scripts/verify-release.sh scripts/test-release-verifier.sh
shellcheck -x scripts/verify-release.sh scripts/test-release-verifier.sh
make release-check release-workflow-check release-verifier-check
actionlint .github/workflows/ci.yml .github/workflows/release.yml
make check
```

The complete gate passed with 313 Go tests, five compile targets, zero lint issues, a 6.21 MB
binary, 4.9 ms cold-start p50, one root dependency, 46 site checks, seven two-mode checks, 56
installer checks, 24 release checks, 41 workflow checks, and 30 release-verifier checks.

### Remaining cutover boundary

T0.4d2 remains intentionally separate. No repository visibility, tag, release, or public artifact
setting changed in this checkpoint. The exact three-command owner trial is still unavailable until
the owner authorizes either a public repository or a separate public release-artifact origin, after
which `v0.1.0` can be published and verified live.

---

## Architecture migration / A6.1 — protocol envelope foundation

**Status:** done, 2026-08-23 · **Protocol checks:** 26 · **Host tests:** 339 ·
**Dependencies:** standard library only · **User-visible changes:** none

Publishing remains postponed at the owner's request, so the implementation sequence resumed with
the first purely additive protocol slice. `spec/VERSION` now holds protocol version `0`;
`spec/schemas/envelope.json` defines the six-field language-neutral wrapper; and one compact golden
`message.delta` frame fixes field names, ordering, canonical IDs, timestamp shape, and object data.
`protocol` is the public Go binding and is registered as the dependency-free L1 contract package.

### TDD record

**Red:** after the spec, golden frame, and conformance test were written, `go test ./protocol`
failed to compile only on the intended absent surface: `Version`, `Envelope`, `Decode`, and
`Encode` were undefined. No existing package was touched to manufacture the failure.

**Green:** `Envelope` now validates a positive sequence, canonical uppercase typed session/turn
ULIDs, RFC 3339 timestamps through `time.Time`, lowercase dot-separated event names, and
object-valued valid JSON data. `Encode` emits one compact frame with no transport delimiter;
`Decode` consumes exactly one JSON value, ignores unknown top-level fields, retains unknown data,
and accepts syntactically valid future event names. This is the byte seam future NDJSON and SSE
wrappers will share.

**Refactor:** the conformance test parses the actual JSON Schema and checks its dialect, field
order, positive minimum, date-time format, ID/event patterns, data shape, and explicit forward
compatibility. Its invalid matrix independently covers every absent required field, zero sequence,
malformed time, wrong/lowercase/overflow/forbidden IDs, malformed event segments, null/array data,
and a trailing JSON value. The pre-existing vendor fixtures now state clearly that they are adapter
inputs rather than Kolkrabbi envelope frames.

### Verification

```sh
go test -race ./protocol
go test ./internal/arch
go vet ./protocol
go list -deps -f '<non-standard dependency filter>' ./protocol
make check
```

The dependency filter printed nothing. The complete gate passed with 339 tests, five compile
targets, zero lint issues, a 6.21 MB binary, 4.7 ms cold-start p50, one root dependency, 46 site
checks, seven v0.1 surface checks, 56 installer checks, and all release contracts unchanged.

### Next checkpoint

A6.2 adds the closed event-name vocabulary and typed payload/golden pairs without connecting the
engine or changing CLI output. Publishing, the public tag, and the clean-machine rehearsal remain
blocked by the owner's explicit sequencing decision.

---

## Architecture migration / A6.2a — streamed delta vocabulary

**Status:** done, 2026-08-23 · **Protocol checks:** 38 · **Host tests:** 351 ·
**Dependencies:** standard library only · **User-visible changes:** none

The first event-vocabulary slice is limited to the two payloads already fixed by the hardened
architecture and provider plans. `message.delta` and `reasoning.delta` now each have an event
constant, a typed Go payload, a versioned JSON Schema, and a compact golden envelope. Both require
one non-empty `text` string and allow unknown payload fields so version-0 producers can extend a
frame without making older decoders discard it.

Lifecycle events and `message.completed` were deliberately excluded: their complete payload fields
are not fixed in the authoritative plans yet. Tool, permission, orchestration, accounting, and
diagnostic events remain separate A6.2 slices as well.

### TDD record

**Red:** after the two schemas, the `reasoning.delta` golden, and the conformance matrix were
written, `go test ./protocol` failed to compile only on the intended missing public surface:
`EventType`, `EventMessageDelta`, `EventReasoningDelta`, `MessageDeltaData`, and
`ReasoningDeltaData` were undefined.

**Green:** `Envelope.Type` now uses the string-backed `EventType`, preserving the exact JSON wire
shape. Known delta events validate their schema-backed payload during both encode and decode;
missing, empty, null, and non-string `text` fail closed. Syntactically valid unknown event names
remain accepted, with their raw data retained exactly as before.

**Refactor:** one internal validator owns the shared known-event dispatch, while the public payload
types remain distinct so later evolution cannot accidentally couple message and reasoning fields.
The table-driven conformance test derives each schema and fixture path from its event constant and
checks schema identity, required fields, forward compatibility, typed decoding, and byte-exact
round trips.

### Verification

```sh
go test -count=1 ./protocol
go vet ./protocol
go list -deps -f '{{if and (not .Standard) (ne .ImportPath "github.com/onembyte/kolkrabbi/protocol")}}{{.ImportPath}}{{end}}' ./protocol
make check
```

The dependency filter printed nothing. The complete gate passed with 351 tests, five compile
targets, zero lint issues, a 6.21 MB binary, 4.8 ms cold-start p50, one root dependency, 46 site
checks, seven v0.1 surface checks, 56 installer checks, and all release contracts unchanged.

### Next checkpoint

A6.2b fixes the handshake, session, turn, and completed-message payloads before adding their
constants, schemas, bindings, and goldens. Publishing, the public tag, and the clean-machine
rehearsal remain postponed by the owner's explicit sequencing decision.

---

## Architecture migration / A6.2b1 — hello handshake

**Status:** done, 2026-08-23 · **Protocol checks:** 50 · **Host tests:** 363 ·
**Dependencies:** standard library only · **User-visible changes:** none

The first lifecycle slice freezes only the handshake fields already explicit in the architecture
and mobile constraint: protocol version, server identity, and capability names. `hello` now has a
public event constant, typed `HelloData`, versioned JSON Schema, and compact golden envelope. The
same payload type can later back `GET /v1/hello` without making the HTTP endpoint a second contract.

The capability list must be present but may be empty, allowing a minimal server to describe itself
honestly. Capability names are non-empty and unique; unknown payload fields remain retained for
version-0 forward compatibility. Platform-specific capability selection stays outside the protocol
package and outside this checkpoint.

### TDD record

**Red:** with the schema, golden, and invalid-field matrix in place, `go test ./protocol` failed to
compile only because `EventHello` and `HelloData` did not exist.

**Green:** the string-backed event constant is `hello`; `HelloData` exposes `protocol`, `server`,
and `capabilities` with the exact snake-case wire fields. Known-event validation requires protocol
`0`, a non-empty server, and a non-null array of unique non-empty capability names. An empty array
and unknown payload fields are accepted, while the complete golden frame round-trips byte-for-byte.

**Refactor:** handshake validation remains inside the existing known-event dispatcher, so both
`Encode` and `Decode` enforce the same contract. No existing package imports `protocol`, and the
architecture dependency guard continues to prove that public conformance tests use only the
standard library.

### Verification

```sh
go test -count=1 ./protocol
go vet ./protocol
go list -deps -f '{{if and (not .Standard) (ne .ImportPath "github.com/onembyte/kolkrabbi/protocol")}}{{.ImportPath}}{{end}}' ./protocol
make check
```

The dependency filter printed nothing. The complete gate passed with 363 tests, five compile
targets, zero lint issues, a 6.21 MB binary, 5.1 ms cold-start p50, one root dependency, 46 site
checks, seven v0.1 surface checks, 56 installer checks, and all release contracts unchanged.

### Next checkpoint

At the owner's direction, the preserved experimental agent implementation is restored through the
public mode surfaces as its own TDD checkpoint before A6.2b2 resumes the session-lifecycle contract.
Publishing, the public tag, and the clean-machine rehearsal remain postponed.

---

## Owner trial / R0.2 — agentic surface restoration

**Status:** done, 2026-08-23 · **Protocol checks:** 50 · **Host tests:** 365 ·
**User-visible changes:** agent mode restored · **Default mode:** code

At the owner's direction, the preserved orchestrator is reachable again as the third public mode.
The engine registry, `--mode`, `/mode`, top-level and slash help, README, landing page, and static
guard rails now expose exactly `chat`, `code`, and `agent`. Plain `kolk` still computes `code` as
its default.

The restored copy describes the implementation that actually exists: agent mode asks one planner
for an ordered task list, runs each tool-capable subagent sequentially in an isolated context, and
uses a tool-free synthesis call for the final response. Effort continues to select a configured
model tier and, in agent mode, caps task width at 2, 3, 4, or 6. This checkpoint did not redesign
or parallelize orchestration and did not change providers, tools, permissions, or protocol code.

### TDD record

**Red:** the new exact-three-mode registry test found only `[chat code]`; `--mode agent` returned the
old `(chat|code)` usage error; `/mode agent` stayed in code mode; slash help listed only two modes;
the public-surface contract failed seven of nine checks; and the landing-page contract failed its
three new agent assertions.

**Green:** `ModeAgent` joined the accepted registry and both invalid-mode errors now enumerate all
three choices. Flags, live mode switching, help, README, and site copy expose agent mode while
retaining code as the default. The pre-existing multi-task orchestration and single-task fallback
tests stayed green without any production change to the orchestrator.

**Refactor:** a CLI-level offline end-to-end test now drives the real `--mode agent -p` path using a
stored credential and scripted provider. It proves four distinct calls with the correct tool
boundaries: planner without tools, two ordered tool-capable subagents, then synthesis without tools.
Static contracts reject inaccurate parallel-execution claims.

### Verification

```sh
go test -count=1 ./internal/engine ./internal/cli -run '<focused R0.2 matrix>'
./scripts/test-v01-surface.sh
./scripts/test-site.sh
go run ./cmd/kolk help
make check
```

The complete gate passed with 365 tests, five compile targets, zero lint issues, a 6.21 MB binary,
4.9 ms cold-start p50, one root dependency, 48 site checks, nine mode-surface checks, 56 installer
checks, and all release contracts unchanged.

### Next checkpoint

A6.2b2 resumes the session-lifecycle protocol contract as an independent schema, binding, golden,
and validation slice. Publishing, the public tag, and the clean-machine rehearsal remain postponed.

---

## Architecture migration / A6.2b2 — session lifecycle

**Status:** done, 2026-08-23 · **Protocol checks:** 91 · **Host tests:** 406 ·
**Dependencies:** standard library only · **User-visible changes:** none

This slice freezes the three session lifecycle names from the architecture and gives each one a
minimal payload that does not duplicate envelope state. `session.started` projects the non-empty
model, mode, effort, and working directory needed when attaching to a live session.
`session.updated` is a non-empty patch whose known optional fields are model, mode, effort, and
title. `session.ended` carries a non-empty reason.

Session and turn IDs plus the event timestamp remain solely in the envelope. Mode, effort, and end
reason are open strings rather than enums, so new product values do not require a protocol bump.
An update containing only unknown future fields is valid and retained; present known fields still
fail closed when empty, null, or the wrong JSON type.

### TDD record

**Red:** after adding the three schemas, compact golden frames, changelog entry, and invalid-field
matrix, `go test ./protocol` failed to compile only because `EventSessionStarted`,
`EventSessionUpdated`, `EventSessionEnded`, and their three payload types were undefined.

**Green:** the constants now match `session.started`, `session.updated`, and `session.ended` exactly.
Known-event validation enforces every required started field, non-empty update patches with valid
known values, and a non-empty ended reason. All three typed goldens round-trip byte-for-byte, while
unknown update fields remain in the raw envelope data.

**Refactor:** optional fields on `SessionUpdatedData` use `omitempty`, and an explicit binding test
proves a one-field Go value marshals to a schema-valid one-field patch. The update validator checks
raw field presence separately from typed values, distinguishing an omitted field from a present
empty or null value without rejecting additive unknown-only patches.

### Verification

```sh
go test -count=1 ./protocol
go vet ./protocol
go list -deps -f '{{if and (not .Standard) (ne .ImportPath "github.com/onembyte/kolkrabbi/protocol")}}{{.ImportPath}}{{end}}' ./protocol
make check
```

The dependency filter printed nothing. The complete gate passed with 406 tests, five compile
targets, zero lint issues, a 6.21 MB binary, 4.5 ms cold-start p50, one root dependency, 48 site
checks, nine mode-surface checks, 56 installer checks, and all release contracts unchanged.

### Next checkpoint

A6.2b3 fixes turn.started, turn.finished, and turn.cancelled as a separate lifecycle slice before
completed content. Publishing, the public tag, and the clean-machine rehearsal remain postponed.

---

## Architecture migration / A6.2b3 — turn lifecycle

**Status:** done, 2026-08-23 · **Protocol checks:** 126 · **Host tests:** 441 ·
**Dependencies:** standard library only · **User-visible changes:** none

This slice freezes the three turn lifecycle names from the architecture and gives each one a
minimal payload that does not duplicate envelope state. `turn.started` preserves the non-empty
input, requested model, mode, and effort needed for replay and newly attached clients.
`turn.finished` records a normalized open-ended reason and can preserve one optional provider
`raw_reason`. `turn.cancelled` records its own open-ended reason.

Session and turn IDs plus the event timestamp remain solely in the envelope. Completed or partial
content, usage, errors, duration, response metadata, persistence, transport, and engine wiring stay
outside this checkpoint.

### TDD record

**Red:** after adding the three schemas, compact golden frames, changelog entry, and invalid-field
matrix, `go test -count=1 ./protocol` failed to compile only because `EventTurnStarted`,
`EventTurnFinished`, `EventTurnCancelled`, and their three payload types were undefined.

**Green:** the constants now match `turn.started`, `turn.finished`, and `turn.cancelled` exactly.
Known-event validation requires every started projection field and both lifecycle reasons while
allowing future reason vocabulary. Typed payloads decode every golden, and each complete envelope
round-trips byte-for-byte.

**Refactor:** `TurnFinishedData.RawReason` uses `omitempty`, while validation separately checks raw
field presence. This distinguishes an omitted provider reason from a present empty, null, or
wrongly typed value without rejecting additive unknown fields.

### Verification

```sh
go test -count=1 ./protocol
go vet ./protocol
go list -deps -f '{{if and (not .Standard) (ne .ImportPath "github.com/onembyte/kolkrabbi/protocol")}}{{.ImportPath}}{{end}}' ./protocol
make check
```

The dependency filter printed nothing. The complete gate passed with 441 tests, five compile
targets, zero lint issues, a 6.21 MB binary, 4.5 ms cold-start p50, one root dependency, 48 site
checks, nine mode-surface checks, 56 installer checks, and all release contracts unchanged.

### Next checkpoint

A separate website checkpoint adds a capabilities navbar page with honest available/planned
status, subscription and API-key options, cap-aware continuity policy, theme plans, and accessible
English/Spanish explainer-video slots. Publishing, the public tag, and the clean-machine rehearsal
remain postponed.

---

## Website / W0.2 — capabilities catalog

**Status:** done, 2026-08-23 · **Site checks:** 93 · **Host tests:** 441 ·
**Runtime dependencies:** none · **User-visible changes:** static catalog and navbar link

The landing-page navigation now includes a prominent `Capabilities` button. Its dedicated page is
a comprehensive catalog divided into working, access, continuity, workflow, safety, and interface
groups. A three-state legend and a status badge on every card distinguish `Available now` from
`Designed` and `Planned`, so the site can describe the product direction without advertising an
unfinished capability as usable.

The catalog records the requested subscription and continuation direction: Claude Agent and Codex
sign-in paths, preserving one Kolkrabbi session across backends, provider-agnostic key onboarding,
limit classification, best-rated eligible-model selection, ask-before-free fallback by default,
and opt-in automatic switching. It also covers themes and the remaining roadmap groups. The last
main-content section reserves accessible English and Spanish explainer slots; both are honest
placeholders with no broken media element or third-party embed.

### TDD record

**Red:** after adding W0.2's checklist and extending the independent contract first,
`bash scripts/test-site.sh` failed 42 of 90 checks. Every failure named an absent W0.2 surface: the
page, nav button, statuses, capability groups, requested continuity text, video slots, or styles.

**Green:** `site/capabilities.html`, the landing-page link, and responsive shared CSS satisfied all
90 checks without JavaScript, external runtime assets, inline styles, a framework, or a build step.

**Refactor:** the site contract now also proves the videos section is the final main-content
section, both language placeholders disclose their pending state, and no iframe, video, or source
element ships before real media sources exist. The final independent count is 93.

### Verification

```sh
bash -n scripts/test-site.sh
bash scripts/test-site.sh
git diff --check
make check
```

The complete gate passed with 441 tests, five compile targets, zero lint issues, a 6.21 MB binary,
5.0 ms cold-start p50, one root dependency, 93 site checks, nine mode-surface checks, 56 installer
checks, and all release contracts unchanged.

### Next checkpoint

A6.2b4 resumes the protocol vocabulary with the authoritative `message.completed` payload as an
independent schema, golden, binding, and validation slice. Publishing, the public tag, and the
clean-machine rehearsal remain postponed.

---

## Architecture migration / A6.2b4 — completed message snapshot

**Status:** done, 2026-08-23 · **Protocol checks:** 138 · **Host tests:** 453 ·
**Dependencies:** standard library only · **User-visible changes:** none

This slice freezes `message.completed` as the authoritative final display-text snapshot for an
assistant message. Its one required `text` string contains the complete assembled text rather than
the last streamed delta, so replay and newly attached clients do not depend on retaining every
coalescible `message.delta` frame.

An explicit empty string is valid because a tool-only or interrupted assistant message can reach a
real completion boundary without display text. Missing, null, and non-string values fail closed.
Unknown future fields remain in the raw envelope, while message identity, status, finish reason,
tools, reasoning, provider state, usage, and annotations remain owned by the envelope or dedicated
events.

### TDD record

**Red:** after adding the schema, compact golden frame, changelog entry, and invalid-value matrix,
`go test -count=1 ./protocol` failed to compile only because `EventMessageCompleted` and
`MessageCompletedData` were undefined.

**Green:** the new constant and typed payload now match the language-neutral artifacts. Known-event
validation requires a present string without imposing a non-empty constraint. The golden decodes
through the typed binding and its complete envelope round-trips byte-for-byte.

**Refactor:** validation uses pointer presence only at the wire boundary, distinguishing missing or
null text from an explicit empty snapshot without adding optionality to the public payload. Focused
tests also prove Unicode acceptance and retention of an additive unknown field.

### Verification

```sh
go test -count=1 ./protocol
go vet ./protocol
go list -deps -f '{{if and (not .Standard) (ne .ImportPath "github.com/onembyte/kolkrabbi/protocol")}}{{.ImportPath}}{{end}}' ./protocol
make check
```

The dependency filter printed nothing. The complete gate passed with 453 tests, five compile
targets, zero lint issues, a 6.21 MB binary, 4.7 ms cold-start p50, one root dependency, 93 site
checks, nine mode-surface checks, 56 installer checks, and all release contracts unchanged.

### Next checkpoint

A6.2c begins with the smallest tool/decision event slice after auditing the architecture and current
engine vocabulary. Publishing, the public tag, and the clean-machine rehearsal remain postponed.

---

## Architecture migration / A6.2c1 — requested tool invocation

**Status:** done, 2026-08-23 · **Protocol checks:** 162 · **Host tests:** 477 ·
**Dependencies:** standard library only · **User-visible changes:** none

This slice freezes `tool.requested` independently from execution progress, outcomes, and permission
decisions. Every request carries a non-empty stable call ID, non-empty tool name, complete valid JSON
argument text, and an explicit executor. Call IDs remain provider-compatible correlation strings;
the protocol does not rewrite them into a second identity system.

The executor vocabulary follows the later hardened subscription decision: `kolk` means the call is
eligible for Kolkrabbi's tool and permission boundary, while `provider` means the backend already
executed it. The latter distinction prevents clients from presenting a false approval control after
an external agent has already changed the working tree. `vendor` remains presentation terminology,
not a third wire value.

### TDD record

**Red:** after adding the schema, compact golden frame, changelog entry, and invalid-value matrix,
`go test -count=1 ./protocol` failed to compile only because `EventToolRequested`, `ToolExecutor`,
its two values, and `ToolRequestedData` were undefined.

**Green:** the constant, typed payload, executor values, and known-event validation now match the
language-neutral artifacts. Validation rejects absent or malformed required fields, malformed JSON
argument text, and unknown ownership while accepting both defined executors.

**Refactor:** arguments remain a string and are checked with `json.Valid` without unmarshalling, so
spacing and byte representation survive in the raw envelope. Tests also prove schema field order,
byte-stable golden round-trip, Unicode arguments, and retention of an additive unknown field.

### Verification

```sh
go test -count=1 ./protocol
go vet ./protocol
go list -deps -f '{{if and (not .Standard) (ne .ImportPath "github.com/onembyte/kolkrabbi/protocol")}}{{.ImportPath}}{{end}}' ./protocol
make check
```

The dependency filter printed nothing. The complete gate passed with 477 tests, five compile
targets, zero lint issues, a 6.21 MB binary, 4.5 ms cold-start p50, one root dependency, 93 site
checks, nine mode-surface checks, 56 installer checks, and all release contracts unchanged.

### Next checkpoint

A6.2c2 audits and freezes `tool.started`, `tool.output`, and `tool.finished` as an independent
execution-lifecycle slice. Permission events remain separate. Publishing, the public tag, and the
clean-machine rehearsal remain postponed.

---

## Architecture migration / A6.2c2a — tool execution started

**Status:** done, 2026-08-23 · **Protocol checks:** 177 · **Host tests:** 492 ·
**Dependencies:** standard library only · **User-visible changes:** none

This slice freezes `tool.started` independently from output and outcome. Its payload contains only
the non-empty tool-call correlation ID and the executor inherited from `tool.requested`. Repeating
ownership on every lifecycle line preserves the safety-critical distinction between a Kolkrabbi-run
tool and work already delegated to a provider backend.

The request event remains authoritative for name and arguments, while the envelope owns event time.
Output, terminal status, duration, permission state, process identity, progress, cross-event
consistency, and engine instrumentation remain outside this checkpoint.

### TDD record

**Red:** after adding the schema, compact golden frame, changelog entry, and invalid-value matrix,
`go test -count=1 ./protocol` failed to compile only because `EventToolStarted` and
`ToolStartedData` were undefined.

**Green:** the new constant and typed payload now match the language-neutral artifacts. Known-event
validation rejects missing or malformed correlation IDs and rejects missing, wrongly typed, or
unknown executors while accepting both `kolk` and `provider`.

**Refactor:** one `validToolExecutor` helper now enforces the same closed ownership vocabulary for
both requested and started events. Tests also prove schema field order, byte-stable golden
round-trip, Unicode unknown-field retention, and continued forward compatibility.

### Verification

```sh
go test -count=1 ./protocol
go vet ./protocol
go list -deps -f '{{if and (not .Standard) (ne .ImportPath "github.com/onembyte/kolkrabbi/protocol")}}{{.ImportPath}}{{end}}' ./protocol
make check
```

The dependency filter printed nothing. The complete gate passed with 492 tests, five compile
targets, zero lint issues, a 6.21 MB binary, 4.4 ms cold-start p50, one root dependency, 93 site
checks, nine mode-surface checks, 56 installer checks, and all release contracts unchanged.

### Next checkpoint

A6.2c2b audits and freezes `tool.output` content semantics without deciding terminal success or
failure. Publishing, the public tag, and the clean-machine rehearsal remain postponed.

---

## Architecture migration / A6.2c2b — tool execution output

**Status:** done, 2026-08-23 · **Protocol checks:** 195 · **Host tests:** 510 ·
**Dependencies:** standard library only · **User-visible changes:** none

This slice freezes `tool.output` independently from terminal outcome. Its payload contains the
non-empty tool-call correlation ID, one complete display-ready output string, and the executor
inherited from the preceding tool lifecycle events. Output is required but may be empty: provider
tools can legitimately finish without producing display text, and dropping that value would make
their lifecycle unreplayable.

The request event remains authoritative for name and arguments, while `tool.finished` will own
success or failure. Streaming markers, stdout/stderr structure, truncation, MIME metadata, duration,
cross-event consistency, provider translation, and engine instrumentation remain outside this
checkpoint.

### TDD record

**Red:** after freezing the acceptance boundary and adding the conformance matrix,
`go test -count=1 ./protocol` failed to compile only because `EventToolOutput` and
`ToolOutputData` were undefined.

**Green:** the new constant, typed payload, schema, golden frame, changelog entry, and known-event
validator now agree. Validation rejects missing or malformed correlation IDs, missing/null/non-string
output, and missing, wrongly typed, or unknown executors while accepting empty output and both
defined ownership values.

**Refactor:** the public payload keeps `Output` as an ordinary string, while the validator uses a
private pointer-valued wire view to distinguish a valid empty string from a missing or null field.
Tests also prove schema field order, byte-stable golden round-trip, Unicode output, unknown-field
retention, and continued forward compatibility.

### Verification

```sh
go test -count=1 ./protocol
go vet ./protocol
go list -deps -f '{{if and (not .Standard) (ne .ImportPath "github.com/onembyte/kolkrabbi/protocol")}}{{.ImportPath}}{{end}}' ./protocol
make check
```

The dependency filter printed nothing. The complete gate passed with 510 tests, five compile
targets, zero lint issues, a 6.21 MB binary, 4.6 ms cold-start p50, one root dependency, 93 site
checks, nine mode-surface checks, 56 installer checks, and all release contracts unchanged.

### Next checkpoint

A6.2c2c audits and freezes `tool.finished` terminal outcome semantics without adding permission or
engine wiring. Publishing, the public tag, and the clean-machine rehearsal remain postponed.

---

## Architecture migration / A6.2c2c — tool execution finished

**Status:** done, 2026-08-23 · **Protocol checks:** 215 · **Host tests:** 530 ·
**Dependencies:** standard library only · **User-visible changes:** none

This slice freezes `tool.finished` independently from permission and engine wiring. Its payload
contains the non-empty tool-call correlation ID, one boolean `ok` outcome, and the executor inherited
from the preceding tool lifecycle events. The wire name follows the existing `tool_ok` analytics
vocabulary rather than exposing a provider-specific `is_error` field; provider outcomes map as
`ok = !IsError`.

`ok` reports whether the invocation produced a valid tool result. It does not reinterpret facts
inside that result: for example, the existing shell runner deliberately returns a subprocess's
non-zero exit as model-visible output when the tool machinery itself worked. Error prose remains in
`tool.output`; duration, exit metadata, cancellation, cross-event consistency, translation, and
instrumentation remain outside this checkpoint.

### TDD record

**Red:** after freezing the acceptance boundary and adding the conformance matrix,
`go test -count=1 ./protocol` failed to compile only because `EventToolFinished` and
`ToolFinishedData` were undefined.

**Green:** the new constant, typed payload, schema, golden frame, changelog entry, and known-event
validator now agree. Validation rejects missing or malformed correlation IDs, missing/null/non-boolean
outcomes, and missing, wrongly typed, or unknown executors while accepting both boolean outcomes and
both ownership values.

**Refactor:** the public payload keeps `OK` as an ordinary boolean, while the validator uses a
private pointer-valued wire view to distinguish a valid `false` from a missing or null field. Tests
also prove schema field order, byte-stable golden round-trip, Unicode unknown-field retention, and
continued forward compatibility.

### Verification

```sh
go test -count=1 ./protocol
go vet ./protocol
go list -deps -f '{{if and (not .Standard) (ne .ImportPath "github.com/onembyte/kolkrabbi/protocol")}}{{.ImportPath}}{{end}}' ./protocol
make check
```

The dependency filter printed nothing. The complete gate passed with 530 tests, five compile
targets, zero lint issues, a 6.21 MB binary, 4.8 ms cold-start p50, one root dependency, 93 site
checks, nine mode-surface checks, 56 installer checks, and all release contracts unchanged.

### Next checkpoint

A6.2c3 audits the permission request/resolution boundary after the tool execution vocabulary is
complete. Publishing, the public tag, and the clean-machine rehearsal remain postponed.

---

## Architecture migration / A6.2c3a — permission requested

**Status:** done, 2026-08-23 · **Protocol checks:** 236 · **Host tests:** 551 ·
**Dependencies:** standard library only · **User-visible changes:** none

This slice freezes the user-facing half of the permission round-trip independently from its later
decision. `permission.requested` carries a non-empty opaque permission request ID, non-empty tool
name, non-empty human-readable detail, and an optional string diff. Diff omission means there is no
separate preview; an explicitly empty string remains valid wire data.

The event deliberately has no executor because only Kolkrabbi-run tool calls can reach this approval
boundary; provider-executed tools have already run. Decision choices, timeout policy, expiration,
risk and rule metadata, structured arguments/diffs, cross-event correlation, the serialized queue,
engine integration, and transport endpoints remain outside this checkpoint.

### TDD record

**Red:** after splitting request from resolution, freezing the acceptance boundary, and adding the
conformance matrix, `go test -count=1 ./protocol` failed to compile only because
`EventPermissionRequested` and `PermissionRequestedData` were undefined.

**Green:** the new constant, typed payload, schema, golden frame, changelog entry, and known-event
validator now agree. Validation rejects missing or malformed request identity, tool, and detail;
accepts omitted, empty, non-empty, and Unicode diff text; and rejects null or wrongly typed diffs.

**Refactor:** validation uses the raw field map plus a private pointer-valued diff view so omission
stays distinct from an explicitly invalid null without changing the public payload. Tests also prove
schema field order, byte-stable golden round-trip, unknown-field retention, the absence of an
executor field, and continued forward compatibility.

### Verification

```sh
go test -count=1 ./protocol
go vet ./protocol
go list -deps -f '{{if and (not .Standard) (ne .ImportPath "github.com/onembyte/kolkrabbi/protocol")}}{{.ImportPath}}{{end}}' ./protocol
make check
```

The dependency filter printed nothing. The complete gate passed with 551 tests, five compile
targets, zero lint issues, a 6.21 MB binary, 5.2 ms cold-start p50, one root dependency, 93 site
checks, nine mode-surface checks, 56 installer checks, and all release contracts unchanged.

### Next checkpoint

A6.2c3b audits and freezes `permission.resolved` correlation and decision vocabulary independently.
Publishing, the public tag, and the clean-machine rehearsal remain postponed.

---

## Release cutover / R0.3 — public v0.1.0 and copyable install command

**Status:** done, 2026-08-23 · **Release:** `v0.1.0` · **Release commit:** `e0b81a4` ·
**Site checks:** 110 · **Public installer:** verified on Darwin/arm64

The first owner install exposed an operational gap rather than an installer parsing bug: with no
public GitHub Release, GitHub sent `/releases/latest` to the releases index. The installer's strict
tag allowlist correctly rejected that destination instead of downloading mutable or unsigned
content. The exact green `e0b81a4` commit is now tagged `v0.1.0`; release run `32666486535` built all
four archives, authenticated the Sigstore bundle, checked every archive against the signed manifest,
and executed the host binary before completing successfully.

The public install pipeline then resolved `/releases/latest` to `/releases/tag/v0.1.0`, downloaded
the Darwin/arm64 archive through the live Pages installer, verified its checksum, installed it into
an isolated temporary directory, and reported the stamped version and commit. A separate temporary
home proved that bare `kolk` still prints the exact three-line API-key next action without touching
the owner's configuration.

The landing page now places a keyboard-focusable Copy button beside the install command. One local,
deferred script uses the secure Clipboard API with a selection-based compatibility fallback, changes
the visible label after success, and announces success or failure through a polite status region.
The CSP remains closed to inline and third-party scripts; only same-origin JavaScript is permitted.

### TDD record

**Red:** the expanded site contract failed 12 checks for the absent controller, copy target, button,
accessible status, responsive layout, styles, fallback, and same-origin CSP allowance.

**Green:** the minimal HTML, CSS, local controller, and header policy passed all 105 initial site
checks and `node --check site/app.js`. The owner-reviewed sequence then placed the API-key command in
the Run row and reduced Use it to the single final `kolk`; three structural checks keep that ordering
fixed. A decorative overlapping-squares icon was then added beside the stable text label with two
accessibility/style checks, bringing the focused contract to 110 checks.

**Refactor:** the exclusion helper now distinguishes a real match from an invalid regular expression;
this removed a false-positive path discovered while checking the new script. The complete repository
gate passed with 570 tests, five compile targets, zero lint issues, a 6.21 MB binary, 4.7 ms cold-start
p50, and all installer and release contracts green.

### Remaining release gate

The fully clean-machine trial still needs a machine with no prior Kolkrabbi state or Go toolchain,
followed by the owner's real key and first model response. This local rehearsal does not claim that
final box.

---

## Architecture migration / A6.2c3b — permission resolved

**Status:** done, 2026-08-23 · **Protocol checks:** 255 · **Host tests:** 570 ·
**Dependencies:** standard library only · **User-visible changes:** none

This slice closes the protocol's permission vocabulary without wiring a permission queue or changing
the CLI. `permission.resolved` correlates to one earlier request by its non-empty opaque `id` and
records exactly one of `allow`, `allow_session`, or `deny`. An optional non-empty `reason` can explain
facts such as an unattended timeout; those cases remain ordinary denies instead of expanding the
decision enum.

The resolution deliberately does not repeat the tool, detail, diff, or executor. The original
request remains authoritative for presentation facts, the envelope owns the event timestamp, and
cross-event correlation remains a later transport/runtime concern. Permanent approval rules also
remain configuration rather than a fourth wire decision.

### TDD record

**Red:** after freezing the scope and adding the complete conformance matrix,
`go test -count=1 ./protocol` failed to compile only because `EventPermissionResolved`,
`PermissionResolvedData`, and the three decision constants were undefined.

**Green:** the event constant, closed decision type, typed payload, schema, golden envelope,
changelog entry, and known-event validator now agree. The validator rejects absent or malformed
identity and decision fields, rejects unknown decisions, and distinguishes an omitted reason from
empty, null, or wrongly typed reason data.

**Refactor:** the validator reuses the public typed payload while a raw field map preserves the
presence distinction needed by the optional reason. Tests also prove all three decisions, Unicode
reason text, schema field order, omission of an absent reason, byte-stable golden round-trip,
unknown-field retention, and the deliberate absence of tool and executor fields.

### Verification

```sh
gofmt -d protocol/events.go protocol/permission_resolved_test.go
go test -count=1 ./protocol
go vet ./protocol
go list -deps -f '{{if and (not .Standard) (ne .ImportPath "github.com/onembyte/kolkrabbi/protocol")}}{{.ImportPath}}{{end}}' ./protocol
make check
```

The dependency filter printed nothing. The complete gate passed with 570 tests, five compile
targets, zero lint issues, a 6.21 MB binary, 4.9 ms cold-start p50, one root dependency, 110 site
checks, nine mode-surface checks, 56 installer checks, and all release contracts green.

### Next checkpoint

The owner's explicit auto-approve command is next, followed independently by a TTY-safe loading
octopus. A6.2d orchestration and operational events remains queued after those product checkpoints.

---

## Owner UX / U0.1 — explicit auto-approve command

**Status:** done, 2026-08-23 · **Host tests:** 572 · **Mode-surface checks:** 11 ·
**Dependencies:** unchanged · **User-visible change:** `/auto-approve [on|off]`

The interactive session now has an explicit approval control. `/auto-approve` and
`/auto-approve on` idempotently enable the same live state as `-y`; `/auto-approve off` disables it.
The enabled response says that tool actions will run without confirmation, while the disabled
response says they will ask first. Invalid values print the exact usage and leave the current state
unchanged. `/yolo` remains the short compatibility toggle.

The command is deliberately process-local. It writes no config, survives no restart, grants no
permanent rule, and changes neither the tool set nor the engine's established confirmation logic.
README and in-session help expose the descriptive command, while the existing launch flag remains
available for single-shot use.

### TDD record

**Red:** `go test -count=1 ./internal/cli` failed only because help omitted the command,
`/auto-approve` left `Agent.Yolo` disabled, and an invalid value fell through to the generic unknown
command response. The public-surface check then failed only because README still named `/yolo`
without the explicit form.

**Green:** the slash-command branch implements no-argument/on/off behavior, reports both states,
rejects unknown values without mutation, and is documented in both command surfaces.

**Refactor:** one `setAutoApprove` helper owns state mutation and its paired safety message, removing
duplicated output while leaving `/yolo` unchanged.

### Verification

```sh
gofmt -d internal/cli/slash.go internal/cli/repl_test.go
go test -count=1 ./internal/cli
go vet ./internal/cli
./scripts/test-v01-surface.sh
make check
```

The complete gate passed with 572 tests, five compile targets, zero lint issues, a 6.21 MB binary,
5.2 ms cold-start p50, one root dependency, 110 site checks, 11 mode-surface checks, 56 installer
checks, and all release contracts green.

### Next checkpoint

U0.2 freezes and implements the verified `kolk update` and `/update` paths independently from the
loading octopus in U0.3.

---

## Owner UX / U0.1b — mode-prefixed interactive prompt

**Status:** done, 2026-08-23 · **Host tests:** 577 · **Dependencies:** unchanged ·
**User-visible change:** `kolk-<mode>` prompt

Every interactive input prompt now identifies the program and its current mode: `kolk-code>`,
`kolk-chat>`, or `kolk-agent>`. A successful `/mode` change is reflected on the very next prompt.
The startup banner, mode state, non-interactive output, color sequence, and input behavior are
otherwise unchanged.

### TDD record

**Red:** the focused CLI test observed the legacy `code>`, `chat>`, and `agent>` strings in all
three table cases and again across a live code-to-chat switch.

**Green:** one renderer format change added the `kolk-` prefix while continuing to derive the mode
from the live agent on every loop iteration.

**Refactor:** the table test uses the engine's public three-mode registry, so any future supported
mode must prove its prompt identity automatically; a separate transition test protects dynamic
mode changes.

### Verification

```sh
gofmt -d internal/cli/repl.go internal/cli/repl_test.go
go test -count=1 ./internal/cli
go vet ./internal/cli
make check
```

The complete gate passed with 577 tests, five compile targets, zero lint issues, a 6.21 MB binary,
5.0 ms cold-start p50, one root dependency, 110 site checks, 11 mode-surface checks, 56 installer
checks, and all release contracts green.

### Next checkpoint

U0.1c makes bare `/model` list the provider catalog before the frozen U0.2 self-update work resumes.

---

## Owner UX / U0.1c — in-session model catalog

**Status:** done, 2026-08-23 · **Host tests:** 580 · **Dependencies:** unchanged ·
**User-visible change:** bare `/model` lists available models

Bare `/model` now prints the current model and the active provider's available catalog, sorted and
rendered with the same context-length and pricing format as top-level `kolk models`. Supplying an ID
keeps the existing fast path: `/model <id>` updates the live agent and saved session without making
a catalog request. Catalog failures are reported without changing the model or ending the REPL.

### TDD record

**Red:** the focused CLI suite failed to compile because slash handling did not accept the REPL
context required by a cancellable catalog request. The new behavior tests also specified sorted
free/paid rendering, failure recovery, and the no-request direct-switch path.

**Green:** the REPL now passes its context into slash commands, and bare `/model` calls the active
agent client while `/model <id>` retains its direct state update.

**Refactor:** top-level and in-session listing share one fetch path and one renderer, removing the
previous sorting/filtering/formatting ownership from `runModels`. Slash help describes the two forms.

### Verification

```sh
gofmt -d internal/cli/cmd_models.go internal/cli/slash.go internal/cli/repl.go internal/cli/repl_test.go
go test -count=1 ./internal/cli
make check
```

The complete gate passed with 580 tests, five compile targets, zero lint issues, a 6.21 MB binary,
4.9 ms cold-start p50, one root dependency, 110 site checks, 11 mode-surface checks, 56 installer
checks, and all release contracts green.

### Next checkpoint

U0.1d addresses the owner's observed empty-response stop and clarifies the process-local scope of
auto-approval. U0.2 self-update, U0.3 loading state, and U0.4 persistent terminal UI remain isolated
follow-on checkpoints.

---

## Owner UX / U0.1d — resilient agent completion

**Status:** done, 2026-08-23 · **Host tests:** 584 · **Dependencies:** unchanged ·
**User-visible changes:** bounded empty-response recovery and explicit auto-approve scope

The owner's live session `20260823-183354-039e` established the failure precisely: after successful
inspection tool calls, `stealth/ox-alpha` twice returned an assistant message with no text and no
tool calls. The engine treated that wire-valid but semantically empty response as a final answer,
printed only its footer, and returned to the input prompt. No tool-loop limit, cancellation, or
permission denial stopped the task.

Kolk now rejects that empty completion, makes one retry carrying a concise continuation instruction,
and proceeds normally if the model returns text or tool calls. A second consecutive empty completion
returns an actionable error suggesting `/model`, bounding latency and spend. Empty replies and the
synthetic instruction never enter saved history, and valid tool calls are never repeated by the
recovery mechanism. The code/agent system prompt also tells a project-building turn to move from
relevant inspection through one concrete verified checkpoint or report evidence for a blocker.

The same review confirmed that auto-approval had worked in the first process but the transcript then
started a new plain `kolk` process. Persisting this dangerous setting remains intentionally out of
scope. `/yolo` and `/auto-approve` now state “this process only” and point to `kolk --yolo` for a
future launch; flag help describes the setting as applying to one run.

### TDD record

**Red:** the engine test failed because the empty-completion recovery marker did not exist, while
the CLI test showed enabled auto-approve output without either process scope or `kolk --yolo`.

**Green:** the loop now sends one copied, recovery-augmented request without mutating canonical
history, accepts the next ordinary tool/text response, and fails after a second empty result. The
prompt and both approval commands expose the new contracts.

**Refactor:** one copy-on-append helper makes it structurally clear that recovery context is
ephemeral. Tests prove continuation through a real file tool and final response, request count,
clean persisted history, bounded repeated failure, prompt content, and both approval spellings.

### Verification

```sh
gofmt -d internal/engine/agent.go internal/engine/agent_test.go internal/cli/slash.go internal/cli/flags.go internal/cli/repl_test.go
go test -count=1 ./internal/engine ./internal/cli
make check
```

The complete gate passed with 584 tests, five compile targets, zero lint issues, a 6.21 MB binary,
5.1 ms cold-start p50, one root dependency, 110 site checks, 11 mode-surface checks, 56 installer
checks, and all release contracts green.

### Next checkpoint

U0.2 implements the already-frozen verified self-update contract. U0.3 then supplies a TTY-safe
loading octopus, and U0.4 builds the persistent terminal UI on that lifecycle seam.

---

## Owner UX / U0.2a — update identity and discovery

**Status:** done, 2026-08-23 · **Host tests:** 620 · **Dependencies:** standard library only ·
**User-visible changes:** none

The first self-update leaf establishes identity without downloading an artifact or touching the
filesystem. Stable build versions accept only numeric `major.minor.patch` (plus an optional input
`v`), reject prerelease/build/dev/ambiguous/overflowing forms, normalize without the prefix, and
compare each numeric component. Darwin and Linux on amd64 and arm64 are the only update targets;
their archive names exactly match GoReleaser.

Latest discovery makes a cancellable `HEAD` request to the compiled official releases origin. It
requires a 2xx final response and accepts only the same scheme/host and exact
`/onembyte/kolkrabbi/releases/tag/v<stable>` destination, with no query, fragment, escaped path,
suffix, leading-zero component, prerelease, or alternate host. Local URLs exist only as the private
test seam; no alternate update source is exposed to users or configuration.

### TDD record

**Red:** the focused package test failed to compile only because stable parsing/comparison, target
resolution/archive naming, and exact redirect discovery did not exist.

**Green:** two small standard-library files implement those pure contracts, and the architecture
registry places `internal/selfupdate` at L0 alongside build identity, paths, and atomic files.

**Refactor:** static analysis identified and removed a redundant `HasPrefix` branch. The final tests
also pin the official origin, oversized-component rejection, cancellation, HTTP status, request
method, all four targets, and unexpected redirect variants.

### Verification

```sh
gofmt -d internal/selfupdate/*.go internal/arch/layers.go
go test -count=1 ./internal/selfupdate ./internal/arch
make check
```

The first full gate exposed only staticcheck S1017; after the refactor, the complete gate passed with
620 tests, five compile targets, zero lint issues, a 6.21 MB binary, 5.2 ms cold-start p50, one root
dependency, 110 site checks, 11 mode-surface checks, 56 installer checks, and all release contracts
green.

### Next checkpoint

U0.2b downloads the exact manifest/archive with hard size and status bounds, then validates checksum
and archive structure entirely in memory. It still cannot locate or replace an executable.

---

## Owner UX / U0.2b — bounded artifact verification

**Status:** done, 2026-08-23 · **Host tests:** 648 · **Race tests:** green ·
**Dependencies:** standard library only · **User-visible changes:** none

The second self-update leaf is a memory-only verification pipeline. It requests the exact versioned
`checksums.txt` and target archive paths in that order, requires HTTP 200, preserves HTTPS across
redirects, closes every response body, rejects declared and streamed over-limit responses, and
honors context cancellation. The limits are 64 KiB for the manifest and 64 MiB compressed for the
archive.

The manifest must contain one unique exact archive-name row with a lowercase 64-character SHA-256.
Kolk compares that digest before constructing a gzip reader. A matching archive may contain exactly
one regular `kolk`, `README.md`, and `LICENSE`, with no path prefix, duplicate, link, extended
metadata, extra member, oversized payload, truncation, trailing data, or concatenated gzip stream.
Expanded data is bounded per member and in total; an empty executable fails. Only the verified
executable bytes leave the function, and no filesystem API is imported.

This matches the public installer's client-side checksum boundary. It does not claim that a checksum
sidecar authenticates itself: the release workflow and verifier continue to authenticate the
Sigstore-signed manifest before GitHub publishes it.

### TDD record

**Red:** the focused package test failed to compile only because bounded download, exact checksum
selection, combined artifact verification, and strict archive extraction did not exist.

**Green:** one standard-library implementation added the status/size/cancellation guards, digest
gate, gzip/tar structural checks, and in-memory binary result. Tests use local HTTP and generated tar
fixtures; they create no update files.

**Refactor:** discovery and download now share one strict release-origin parser. HTTPS downgrade and
missing final URL fail explicitly. The safety matrix added response-body closure, oversize headers,
truncation, and concatenated streams. The first full gate then identified wrapped-EOF and deprecated
tar syntax; `errors.Is`, the modern PAX field, and literal legacy regular marker removed those lint
issues without relaxing the archive contract.

### Verification

```sh
gofmt -d internal/selfupdate/*.go
go test -count=1 ./internal/selfupdate ./internal/arch
go test -race -count=1 ./internal/selfupdate
make check
```

After the lint refactor, the complete gate passed with 648 tests, five compile targets, zero lint
issues, a 6.21 MB binary, 5.0 ms cold-start p50, one root dependency, 110 site checks, 11
mode-surface checks, 56 installer checks, and all release contracts green.

### Next checkpoint

U0.2c composes version/target/discovery/artifact verification around the running executable and one
atomic `0755` replacement. It must prove every preflight and verification failure preserves the old
binary before U0.2d exposes any command.

---

## Owner UX / U0.2c — atomic executable replacement

**Status:** done, 2026-08-23 · **Host tests:** 669 · **Race tests:** green ·
**Dependencies:** standard library only · **User-visible changes:** none (internal API only)

The updater now composes the verified leaves behind `selfupdate.Update(ctx)`. It rejects an unstable
running build and unsupported target before network or filesystem work, discovers latest, and skips
equal or older releases without executable lookup or artifact requests. For a newer release it
resolves the running executable through symlinks to a regular canonical target, verifies the entire
artifact in memory, checks cancellation once more at the mutation boundary, and performs one
same-directory atomic replacement at mode `0755`.

The success fixture begins with a relative `kolk` symlink and a `0600` target. The exact verified
binary replaces the canonical target at `0755`, while the launch symlink remains a symlink. Separate
fixtures prove discovery, executable resolution, archive download, checksum verification,
cancellation, and replacement errors preserve the old bytes and `0700` mode.

`atomicfile.Write` now distinguishes its only post-commit error: if rename succeeded but directory
sync failed, `DurabilityError` says the replacement is already visible. The updater returns
`Updated=true` with that warning rather than falsely claiming a preserved failure. All other errors
occur before rename and retain the previous file. Chmod failure is no longer ignored before writing,
which prevents a successful update from silently installing a non-executable file.

### TDD record

**Red:** the focused package test failed to compile only because the updater composition, public
result/API, executable resolver, and committed durability classification did not exist.

**Green:** the production updater injects only compiled build identity, runtime target, a bounded
HTTP client, the official release origin, `os.Executable`, and `atomicfile.Write`. Its ordered
preflight/discovery/compare/resolve/verify/cancel/replace path satisfies the mutation boundary.

**Refactor:** the first green test exposed macOS canonicalizing `/var` to `/private/var`; the test now
compares canonical targets. Preservation coverage was then expanded to explicit discovery and
archive-download failures, atomic permissions include `0755`, and pre-write chmod errors fail closed.

### Verification

```sh
gofmt -d internal/selfupdate/*.go internal/atomicfile/*.go
go test -count=1 ./internal/selfupdate ./internal/atomicfile ./internal/arch
go test -race -count=1 ./internal/selfupdate ./internal/atomicfile
make check
```

The complete gate passed with 669 tests, five compile targets, zero lint issues, a 6.21 MB binary,
5.2 ms cold-start p50, one root dependency, 110 site checks, 11 mode-surface checks, 56 installer
checks, 24 release checks, 41 release-workflow checks, and 30 release-verifier checks.

### Next checkpoint

U0.2d is the first leaf allowed to expose the updater: keyless top-level `kolk update` and non-fatal
in-session `/update`, with one injected function, exact help/argument behavior, and restart guidance.

---

## Owner UX / U0.2d — update command surfaces

**Status:** done, 2026-08-23 · **Host tests:** 681 · **Static surface:** 13 checks ·
**Dependencies:** unchanged · **User-visible changes:** `kolk update` and `/update`

The verified updater is now reachable through both requested surfaces. `kolk update` is an
argument-free generated command that dispatches before the default model session; tests point all
three state directories at absent temporary paths and prove a successful update creates none of
them and requests no key. Invalid arguments stop before the injected updater, failures exit 1, and
`Ctrl+C` uses the existing interrupt/exit-130 contract.

In the REPL, `/update` uses the same single app dependency and a per-command interrupt context. An
error is printed as `update failed` and the session continues. A replacement prints current→latest,
the canonical executable path, any durability warning on stderr, and the required restart message;
an unchanged build says it is current and does not ask for a restart. The same command context also
makes the earlier `/model` network listing cancellable without terminating the REPL.

### TDD record

**Red:** the focused CLI suite failed to compile because `app` had no updater seam; therefore neither
top-level nor slash behavior could be injected or dispatched.

**Green:** `newApp` owns one `selfupdate.Update` function, the generated command table owns
`kolk update`, slash handling owns `/update`, and one renderer owns current/updated/warning/restart
output. Tests cover keyless/stateless dispatch, exact call counts, arguments, errors, warnings,
restart truth, unchanged truth, active context, and REPL survival.

**Refactor:** signal ownership moved around each slash command and the top-level update so network
work can be interrupted safely. The static release-surface script now pins both spellings, growing
from 11 to 13 checks.

### Verification

```sh
gofmt -d internal/cli/cli.go internal/cli/cmd_update.go internal/cli/repl.go internal/cli/slash.go internal/cli/*_test.go
go test -count=1 ./internal/cli
./scripts/test-v01-surface.sh
make check
```

The complete gate passed with 681 tests, five compile targets, zero lint issues, a 6.32 MB binary,
4.9 ms cold-start p50, one root dependency, 110 site checks, 13 mode/update-surface checks, 56
installer checks, 24 release checks, 41 release-workflow checks, and 30 release-verifier checks.

### Next checkpoint

U0.3 adds the TTY-only loading octopus on the provider-wait boundary. U0.4 then replaces the
single-line REPL with the persistent terminal composer/status layout already frozen in the ledger.

## Owner UX / U0.1e — bounded rate-limit recovery

The owner's live agent-planning request reached OpenRouter successfully, but
`stealth/ox-alpha` returned HTTP 429 with `limit_source: upstream_provider_shared_pool`. Kolkrabbi
previously flattened the response into text and returned immediately to `kolk-agent>`, which looked
like the agent had chosen to stop. This checkpoint distinguishes that temporary upstream failure
from completion while keeping model-routing and cost policy out of the retry mechanism.

**Red:** provider and engine tests required a typed, scrubbed HTTP error plus one shared retry seam.
The focused compile failed only on the deliberately absent `provider.HTTPError` and
`Agent.RetryWait`. Fixtures cover the exact OpenRouter metadata, identical request replay, planner
coverage, bounded exhaustion, `Retry-After`, cancellation, non-429 responses, and an error received
after streaming has begun.

**Green:** pre-stream HTTP failures now retain status, safe response detail, provider name,
limit source, remedy hint, and parsed `Retry-After`. Every engine model call passes through one
boundary that retries only HTTP 429 at 1s, 2s, and 4s, honors a server delay within the four-second
cap, and returns an actionable `/model` error after four total attempts. The wait is context-owned;
only the successful call is persisted and accounted.

**Refactor:** direct-call inspection leaves exactly one `Client.StreamChat` invocation in the engine:
the retry boundary itself. A shared scripted provider fixture can now produce both pre-stream HTTP
and in-stream errors. Response text is scrubbed before it is stored on the typed error, so
`errors.As` does not weaken the existing credential boundary.

### Verification

```sh
go test ./internal/provider ./internal/engine
make check
```

The complete gate passed with 692 tests, five compile targets, zero lint issues, a 6.32 MB binary,
4.9 ms cold-start p50, one root dependency, 110 site checks, 13 mode/update-surface checks, 56
installer checks, 24 release checks, 41 release-workflow checks, and 30 release-verifier checks.

### Next checkpoint

U0.3 now adds the TTY-only loading octopus using this single provider-wait boundary. U0.4 remains
the separate persistent composer and status-layout checkpoint.

## Owner UX / U0.3a — provider-wait lifecycle

U0.3a isolates engine timing from terminal rendering. The engine now knows when one logical model
call is active and when that activity must be gone, but it still owns no animation, cursor sequence,
clock, goroutine, or terminal decision.

**Red:** six focused engine tests described content, tool-only, provider-error, cancellation,
orchestration-phase, and rate-limit-retry lifecycles. The package failed to compile only because the
planned `Activity` option did not exist.

**Green:** `ActivityIndicator.Start` receives the active context and deterministic `thinking`,
`planning`, `working`, or `synthesizing` phase. One `sync.Once`-guarded stop spans the entire logical
call, including U0.1e retries. It runs before the first visible token, or on return before tool
handling and error presentation. Nil remains the default and writes no bytes.

**Refactor:** all ordinary, planner, subagent, and synthesis paths use the existing shared
`streamChat` boundary. Direct-call inspection still leaves only the provider invocation inside that
boundary, so U0.3b needs only to supply a renderer. The indicator's stop contract explicitly permits
joining its own work; this prevents a stale frame racing the next token or prompt.

### Verification

```sh
go test ./internal/engine
go test -race ./internal/engine
make check
```

The complete gate passed with 698 tests, five compile targets, zero lint issues, a 6.32 MB binary,
4.3 ms cold-start p50, one root dependency, 110 site checks, 13 mode/update-surface checks, 56
installer checks, 24 release checks, 41 release-workflow checks, and 30 release-verifier checks.

### Next checkpoint

U0.3b supplies the fake-clock-tested, TTY-only purple octopus renderer and wires it only into
interactive sessions. Redirected and single-shot output remain outside that renderer by contract.

## Owner UX / U0.3b — TTY loading octopus

U0.3b turns the verified activity lifecycle into the owner's requested loading status without
changing scripted output. A 120 ms grace prevents fast model responses from flashing a frame; slow
calls show a compact purple Braille spinner, octopus, and the engine-owned phase label.

**Red:** deterministic CLI tests required the animation clock, renderer, activation dependencies,
and terminal capability predicate. The focused compile failed only on those deliberately absent
types, fields, and functions.

**Green:** the renderer uses one context-owned goroutine and one stoppable timer at a time. It saves
the cursor once, restores and erases to the line end for every frame, and restores once more while
joining on stop. This preserves the existing `assistant` prefix and guarantees cleanup before the
first token, tool line, error, or prompt. Frames advance every 120 ms; `NO_COLOR` and
`KOLK_NO_COLOR` remove magenta without removing status.

**Refactor:** cursor animation activates only for an interactive REPL when both stdin and stdout are
supported terminals and `TERM` is not `dumb`. Single-shot prompts, pipes, redirects, test buffers,
and unsupported targets never construct the renderer. The fake clock drives grace, frame order,
fast-stop, rendered-cancel, idempotent-stop, and no-colour tests without sleeps.

### Verification

```sh
go test ./internal/term ./internal/cli
go test -race ./internal/cli ./internal/engine
make check
```

The complete gate passed with 710 tests, five compile targets, zero lint issues, a 6.34 MB binary,
4.4 ms cold-start p50, one root dependency, 110 site checks, 13 mode/update-surface checks, 56
installer checks, 24 release checks, 41 release-workflow checks, and 30 release-verifier checks.

### Next checkpoint

U0.4 can now reuse the phase-labelled activity seam and octopus frames inside its persistent status
region. Its multiline composer, resize behavior, shortcuts, themes, and fallback remain a separate
terminal-architecture checkpoint.

---

## Architecture migration / A6.2d1 — subagent lifecycle

**Status:** done, 2026-08-23 · **Protocol checks:** 307 · **Host tests:** 568 ·
**Dependencies:** standard library only · **User-visible changes:** none

This slice gives parallel subagent work an explicit parent/child correlation contract before an
event bus or persistent TUI exists. Both lifecycle frames are emitted on the parent turn. A
canonical `k_` task ID identifies the delegated unit, while `child_turn` identifies the turn whose
deltas, tools, completed message, usage, and diagnostics belong to that unit.

`subagent.started` owns the display task, resolved mode, and 1-based index/total coordinates needed
for stable panes. `subagent.finished` repeats only the correlation and mode, then records an explicit
boolean outcome. It deliberately does not duplicate model output or error text: the child turn's
`message.completed` and the diagnostic `error` event remain authoritative for those facts.

### TDD record

**Red:** after freezing the parent/child boundary and adding both schemas, goldens, and the complete
conformance matrix, `go test ./protocol` failed to compile only because `EventSubagentStarted`,
`EventSubagentFinished`, `SubagentStartedData`, and `SubagentFinishedData` were undefined.

**Green:** the two constants, typed payloads, and known-event validators now match the schema and
golden names. Validation rejects missing and malformed task/turn correlation, empty modes and task
labels, zero, fractional, or inverted presentation coordinates, and absent, null, or non-boolean
outcomes. Both `ok: true` and `ok: false` remain valid terminal states.

**Refactor:** shared correlation validation keeps canonical ID and mode rules identical across the
two frames. Tests prove schema field exactness, typed field order, byte-stable golden round trips,
Unicode task text, additive unknown-field retention, and the deliberate absence of result and error
fields. No existing package imports `protocol`, and the public package still has no third-party
dependency.

### Verification

```sh
go test ./protocol
go test -race ./protocol
go test ./internal/arch
go vet ./protocol
go list -deps -f '{{if and (not .Standard) (ne .ImportPath "github.com/onembyte/kolkrabbi/protocol")}}{{.ImportPath}}{{end}}' ./protocol
make check
```

The dependency filter printed nothing. The complete gate passed with 568 tests, five compile
targets, zero lint issues, a 6.34 MB binary, 4.7 ms cold-start p50, one root dependency, 110 site
checks, 13 mode/update-surface checks, 56 installer checks, 24 release checks, 41 release-workflow
checks, and 30 release-verifier checks.

### Next checkpoint

A6.2d2 cannot freeze chapter identity before saga item 10 settles its state model. A6.2d4a proceeds
independently because the hardened provider plan already fixes its accounting fields and nullability.

---

## Architecture migration / A6.2d4a — usage reported

**Status:** done, 2026-08-23 · **Protocol checks:** 416 · **Host tests:** 677 ·
**Dependencies:** standard library only · **User-visible changes:** none

This slice freezes `usage.reported` as one accounting row for one model within one physical
provider attempt. The event envelope remains authoritative for session, turn, and report time;
the payload identifies the model/provider/request, attempt, call role, effort, measurement class,
and cost provenance needed by replay, stats, and the future dashboard.

Optional token, latency, and cost values use presence rather than sentinels. Omitted means unknown,
while a pointer to zero means measured zero. Cost adds a stronger invariant: `unknown` omits
`cost_usd`, `free` carries an explicit zero, and reported, header, follow-up, price-table, or vendor
estimate sources carry an explicit non-negative value. This prevents missing prices from being
ranked as free models.

### TDD record

**Red:** after freezing the schema, golden frame, provider mapping table, and validation matrix,
`go test ./protocol` failed to compile only because `EventUsageReported`, `UsageReportedData`,
`UsageCostSource`, `UsageMeasurement`, and their constants were undefined.

**Green:** the typed payload and validator now implement the frozen field set. Required identity and
attempt context fail closed; optional counters distinguish omission from zero; optional strings
validate only when present; cost-source and measurement vocabularies are closed; and the cost
presence/source relationship is enforced.

**Refactor:** pointer-valued numeric fields retain unknown-versus-zero semantics through JSON. A
shared optional-integer validator keeps all token and TTFT rules identical. The language-neutral
mapping names every source field and records which context remains in the envelope. Tests also prove
schema field exactness, typed field order, byte-stable golden round trip, all vocabulary members,
unknown-field retention, and the deliberate absence of unsafe derived totals and raw provider data.

### Verification

```sh
go test ./protocol
go test -race ./protocol
go test ./internal/arch
go vet ./protocol
go list -deps -f '{{if and (not .Standard) (ne .ImportPath "github.com/onembyte/kolkrabbi/protocol")}}{{.ImportPath}}{{end}}' ./protocol
make check
```

The dependency filter printed nothing. The complete gate passed with 677 tests, five compile
targets, zero lint issues, a 6.34 MB binary, 4.8 ms cold-start p50, one root dependency, 110 site
checks, 13 mode/update-surface checks, 56 installer checks, 24 release checks, 41 release-workflow
checks, and 30 release-verifier checks.

### Next checkpoint

A6.2d4b now freezes `score.recorded` independently. Saga chapter events remain deferred until item
10 fixes their state and identity semantics; the event bus and persistent TUI still remain behind
the complete A6 contract.

---

## Architecture migration / A6.2d4b — score recorded

**Status:** done, 2026-08-23 · **Protocol checks:** 476 · **Host tests:** 737 ·
**Dependencies:** standard library only · **User-visible changes:** none

This slice freezes `score.recorded` as one typed evaluation of a session, turn, or span. It supports
numeric, categorical, boolean, and text primitives without imposing one universal scale. The event
therefore represents today's 1–5 human `/rate`, future judge verdicts, and implicit signals such as
tool success without coercing them into misleading numbers.

Target and provenance rules are explicit. Session and turn targets use canonical IDs; span IDs stay
opaque until A6.3 freezes the span entity. Human, judge, and implicit sources are closed vocabulary.
A judge score must name its model, while non-judge scores may not carry a judge model. The envelope
owns creation time, and optional explanation text works for every source.

### TDD record

**Red:** after freezing the schema, golden frame, and full target/value/source matrix,
`go test ./protocol` failed to compile only because `EventScoreRecorded`, `ScoreRecordedData`, and
the target, data-type, and source vocabularies were undefined.

**Green:** the public types and known-event validator now enforce score identity, canonical
session/turn targets, the declared native JSON primitive, source provenance, judge-model ownership,
and optional explanation presence. All four value types and all three sources decode, while null,
object, array, mismatched, empty, and unknown values fail closed.

**Refactor:** `json.RawMessage` retains the decoded score primitive without an untyped `any` field or
lossy string conversion. Small closed-vocabulary helpers keep the switch exhaustive. Tests prove
schema property exactness and conditional clauses, typed field order, byte-stable golden round trip,
both boolean values, negative/fractional numeric scores, unknown-field retention, and the intentional
absence of scale, threshold, scorer prompt, and aggregation policy.

### Verification

```sh
go test ./protocol
go test -race ./protocol
go test ./internal/arch
go vet ./protocol
go list -deps -f '{{if and (not .Standard) (ne .ImportPath "github.com/onembyte/kolkrabbi/protocol")}}{{.ImportPath}}{{end}}' ./protocol
make check
```

The dependency filter printed nothing. The complete gate passed with 737 tests, five compile
targets, zero lint issues, a 6.34 MB binary, 5.0 ms cold-start p50, one root dependency, 110 site
checks, 13 mode/update-surface checks, 56 installer checks, 24 release checks, 41 release-workflow
checks, and 30 release-verifier checks.

### Next checkpoint

A6.2d3 can now freeze `checkpoint.created` from the already-shipped checkpoint subsystem. Saga
chapter events remain deferred until item 10 is hardened, and A6.2d5 diagnostics/closure follows
after checkpoints.

---

## Architecture migration / A6.2d3 — checkpoint created

**Status:** done, 2026-08-23 · **Protocol checks:** 503 · **Host tests:** 764 ·
**Dependencies:** standard library only · **User-visible changes:** none

This slice freezes `checkpoint.created` as one durable pre-write snapshot entry. The payload carries
an opaque checkpoint ID, open-ended creation reason, tool, path, and explicit prior-existence state.
The envelope owns session, turn, and creation time, so replaying clients can place the snapshot
without protocol data duplicating the checkpoint manifest.

Backup filenames, content, checksums, modes, refusal metadata, and store paths remain private. That
keeps secrets and internal storage layout off the client contract. Runtime integration will publish
only after the checkpoint store has durably recorded the entry and before the corresponding write;
this slice defines that boundary without changing the existing store or CLI.

### TDD record

**Red:** after freezing the scope and adding the schema, golden frame, and conformance matrix,
`go test ./protocol` failed to compile only because `EventCheckpointCreated` and
`CheckpointCreatedData` were undefined.

**Green:** the event constant, typed payload, and known-event validator now require all snapshot
context and distinguish the required boolean from its false zero value. Missing, null, empty, and
wrongly typed fields fail closed, while existing-file and new-file entries both decode.

**Refactor:** the public payload excludes every storage implementation detail. Tests prove future
reason/tool names, Unicode paths, typed field order, byte-stable golden round trip, schema field
exactness, unknown-field retention, and the deliberate absence of backup and envelope metadata.

### Verification

```sh
go test ./protocol
go test -race ./protocol
go test ./internal/arch
go vet ./protocol
go list -deps -f '{{if and (not .Standard) (ne .ImportPath "github.com/onembyte/kolkrabbi/protocol")}}{{.ImportPath}}{{end}}' ./protocol
make check
```

The dependency filter printed nothing. The complete gate passed with 764 tests, five compile
targets, zero lint issues, a 6.34 MB binary, 4.8 ms cold-start p50, one root dependency, 110 site
checks, 13 mode/update-surface checks, 56 installer checks, 24 release checks, 41 release-workflow
checks, and 30 release-verifier checks.

### Next checkpoint

A6.2d5 now freezes diagnostic `error` and `log` events, then proves the shipped vocabulary is closed.
Saga chapter lifecycle remains intentionally open until PLAN item 10 fixes its state machine.

---

## Architecture migration / A6.2d5a — log diagnostics

**Status:** done, 2026-08-23 · **Protocol checks:** 552 · **Host tests:** 813 ·
**Dependencies:** standard library only · **User-visible changes:** none

This slice freezes `log` as the structured non-error diagnostic channel. It carries a closed debug,
info, or warn level; one of the hardened provider-warning codes or the core `deltas_dropped` code;
and optional field, before/after, and message context. Field transitions must name their field, so a
client never guesses what was projected, dropped, or changed.

The bus backpressure design now uses the same payload: `deltas_dropped` names the condition,
`field` names the affected delta family, and `message` reports the count. This resolves the older
one-off `{dropped:N}` sketch without adding renderer-private data. Failures remain excluded because
A6.3 owns the stable error entity, status mapping, retryability, and remedy.

### TDD record

**Red:** after freezing the schema, golden frame, codes, levels, and conformance matrix,
`go test ./protocol` failed to compile only because `EventLog`, `LogData`, `LogLevel`, `LogCode`, and
their constants were undefined.

**Green:** the event constant, public vocabularies, typed payload, and known-event validator now
agree. All three levels and 16 codes decode. Missing, null, wrongly typed, empty, or unknown
vocabulary/context values fail closed, and before/after transitions require an owning field.

**Refactor:** exhaustive helpers keep the machine vocabulary visible in one place. Tests prove the
schema's exact six fields, dependent requirements, typed field order, byte-stable golden round trip,
minimal code-only diagnostics, backpressure rendering shape, and unknown-field retention. The
architecture and provider plans now describe that same wire payload.

### Verification

```sh
go test ./protocol
go test -race ./protocol
go test ./internal/arch
go vet ./protocol
go list -deps -f '{{if and (not .Standard) (ne .ImportPath "github.com/onembyte/kolkrabbi/protocol")}}{{.ImportPath}}{{end}}' ./protocol
make check
```

The dependency filter printed nothing. The complete gate passed with 813 tests, five compile
targets, zero lint issues, a 6.34 MB binary, 5.0 ms cold-start p50, one root dependency, 110 site
checks, 13 mode/update-surface checks, 56 installer checks, 24 release checks, 41 release-workflow
checks, and 30 release-verifier checks.

### Next checkpoint

A6.3 must now freeze the shared error entity and its error-code/status/exit/retryability mapping.
Only then can A6.2d5b add the `error` event and A6.2d5c prove the final vocabulary closure. Saga
chapter events remain separately gated on PLAN item 10.

---

## Architecture migration / A6.3a — error entity and mapping

**Status:** done, 2026-08-23 · **Protocol checks:** 574 · **Host tests:** 835 ·
**Dependencies:** standard library only · **User-visible changes:** none

This slice freezes the shared public error entity before any event or transport can invent a
different failure shape. The wire carries one closed code, safe display text, and optional positive
retry delay and remedy. HTTP status, shell exit, and default retryability are derived from the code
and are therefore absent from the entity itself.

The 28-code table covers current command/setup failures, every failure kind in the hardened
provider plan, and an expired replay cursor. It intentionally distinguishes a bad client argument
(`invalid_argument`, HTTP 400, exit 2) from a malformed upstream request generated by Kolkrabbi
(`invalid_request`, HTTP 500, exit 1). Temporary endpoint rate limiting remains retryable while an
account-wide exhausted quota does not invite an immediate retry or useless peer-model rotation.

### TDD record

**Red:** the schema, golden entity, Markdown mapping, and exhaustive conformance tests were added
first. `go test ./protocol` failed to compile only because `ErrorCode`, `Error`, their policy
methods, and the entity validator did not exist.

**Green:** `protocol/errors.go` now defines the closed constants, one policy lookup, the typed safe
error, and raw-JSON validation that distinguishes omitted optional values from explicit null.
Every code agrees across schema, documentation, constants, and lookup behavior. Unknown
programmatic codes map to HTTP 500 / exit 1 and disable retry, so construction failures fail closed.

**Refactor:** the architecture's cursor-expiry sketch now names the canonical `code` field. The
public HTTP column is explicitly Kolkrabbi's response rather than a copied provider status; this is
why an invalid upstream request caused by Kolkrabbi surfaces as HTTP 500 instead of blaming the
client with 400.

### Verification

```sh
go test ./protocol -count=1
go test -race ./protocol -count=1
go vet ./protocol
go test ./internal/arch -count=1
go list -deps -f '{{if and (not .Standard) (ne .ImportPath "github.com/onembyte/kolkrabbi/protocol")}}{{.ImportPath}}{{end}}' ./protocol
make check
```

The dependency filter printed nothing. The complete gate passed with 835 tests, five compile
targets, zero lint issues, a 6.34 MB binary, 4.8 ms cold-start p50, one root dependency, 110 site
checks, 13 mode/update-surface checks, 56 installer checks, 24 release checks, 41 release-workflow
checks, and 30 release-verifier checks. The Go module-cache stat write warning remains the known
sandbox-only diagnostic; the budget script and full gate exited zero.

### Next checkpoint

A6.2d5b can now reuse this entity verbatim as the terminal `error` event payload. A6.2d5c will then
prove vocabulary closure. Saga chapter events remain deferred until PLAN item 10 freezes their
state machine; A6.3b shared entities and A6.3c commands remain separate later slices.

---

## Architecture migration / A6.2d5b — error event

**Status:** done, 2026-08-23 · **Protocol checks:** 610 · **Host tests:** 871 ·
**Dependencies:** standard library only · **User-visible changes:** none

The terminal `error` event now wraps the A6.3a error entity without copying its fields or policy.
Its JSON Schema contains a relative reference to `schemas/entities/error.json`; the Go event
decoder calls the same entity validator. The event envelope remains the only source of session,
turn, sequence, and time context.

### TDD record

**Red:** the event schema, golden envelope, shared-entity equality proof, all-code matrix, malformed
entity matrix, and additive-field test were added first. `go test ./protocol` failed only because
`EventError` was undefined.

**Green:** the event constant and one validator branch now exist. Every one of the 28 shared codes
decodes through an event; malformed code, message, retry delay, or remedy fails through the shared
validator rather than an event-specific copy.

**Refactor:** the golden event's `data` bytes are asserted equal to the standalone golden entity,
and the event schema is asserted to have only dialect, id, title, and `$ref`. This makes duplication
a test failure at both the schema and fixture layers.

### Verification

```sh
go test ./protocol -count=1
go test -race ./protocol -count=1
go vet ./protocol
go test ./internal/arch -count=1
go list -deps -f '{{if and (not .Standard) (ne .ImportPath "github.com/onembyte/kolkrabbi/protocol")}}{{.ImportPath}}{{end}}' ./protocol
make check
```

The dependency filter printed nothing. The complete gate passed with 871 tests, five compile
targets, zero lint issues, a 6.34 MB binary, 4.9 ms cold-start p50, one root dependency, 110 site
checks, 13 mode/update-surface checks, 56 installer checks, 24 release checks, 41 release-workflow
checks, and 30 release-verifier checks. The known sandbox-only Go module-cache stat warning did not
affect the successful budget or full-gate exit.

### Next checkpoint

A6.2d5c now proves the shipped event vocabulary is closed: every public event constant must own
exactly one event schema and exactly one golden envelope, with no orphan contract files. Saga
chapter lifecycle stays outside that proof until its still-open subsystem freezes the events.

---

## Architecture migration / A6.2d5c — event vocabulary closure

**Status:** done, 2026-08-23 · **Protocol checks:** 635 · **Host tests:** 896 ·
**Dependencies:** standard library only · **User-visible changes:** none

Protocol version 0 now publishes an ordered catalog of its 23 shipped event types, and one
conformance test proves that catalog is closed across every representation. The test parses
`events.go` with Go's standard-library AST, discovers all exported `Event…` constants, and compares
their wire literals to the catalog, schema filenames, canonical schema IDs, golden filenames, and
the types decoded from those goldens.

### TDD record

**Red:** the AST/filesystem/schema/golden closure test and unknown-event compatibility proof were
added first. `go test ./protocol` failed only because `KnownEventTypes` did not exist.

**Green:** `KnownEventTypes` now returns the 23 events in architectural order and protects its
internal catalog with a defensive copy. All six representations agree exactly; no orphan or missing
event contract exists.

**Refactor:** closure does not weaken forward compatibility. The catalog enumerates what this
binding ships, while a syntactically valid future event still decodes and retains its data. Saga
chapter constants remain absent rather than being published before their state machine is frozen.

### Verification

```sh
go test ./protocol -count=1
go test -race ./protocol -count=1
go vet ./protocol
go test ./internal/arch -count=1
go list -deps -f '{{if and (not .Standard) (ne .ImportPath "github.com/onembyte/kolkrabbi/protocol")}}{{.ImportPath}}{{end}}' ./protocol
make check
```

The dependency filter printed nothing. The complete gate passed with 896 tests, five compile
targets, zero lint issues, a 6.34 MB binary, 4.9 ms cold-start p50, one root dependency, 110 site
checks, 13 mode/update-surface checks, 56 installer checks, 24 release checks, 41 release-workflow
checks, and 30 release-verifier checks. The familiar sandbox-only Go module-cache stat warning was
non-fatal; every gate exited zero.

### Next checkpoint

A6.2 now has one intentionally unresolved event family: A6.2d2 saga chapters. That remains gated on
PLAN item 10's state machine. Work can continue independently with A6.3b by freezing only the
shared entities whose owners are already stable, leaving chapter and span identities deferred.

---

## Architecture migration / A6.3b1 — usage entity

**Status:** done, 2026-08-23 · **Protocol checks:** 636 · **Host tests:** 897 ·
**Dependencies:** standard library only · **User-visible changes:** none

The previously frozen `usage.reported` row is now a shared entity rather than an event-private
shape. `schemas/entities/usage.json` owns the 19 fields, `Usage` is the one Go struct, and
`UsageReportedData` is an alias. The event schema references the entity and the event decoder calls
the entity validator.

### TDD record

**Red:** the entity schema, compact entity golden, schema-reference assertion, typed entity test,
alias identity proof, and byte-equality check were added first. `go test ./protocol` failed only
because `Usage` and `validateUsageEntity` did not exist.

**Green:** usage vocabularies, struct, and validation moved intact into `protocol/entity.go`.
Existing event tests still cover every required/optional field, unknown versus zero measurement,
cost provenance invariant, and additive unknown field through the shared validator.

**Refactor:** `usage.reported` now has a four-key schema whose only payload definition is a relative
reference to the entity. The entity golden is byte-identical to the event golden's data. The first
full gate found one redundant test type annotation through staticcheck; the alias proof was replaced
with a stronger runtime type-identity assertion, then the complete gate was rerun successfully.

### Verification

```sh
go test ./protocol -count=1
go test -race ./protocol -count=1
go vet ./protocol
go test ./internal/arch -count=1
go list -deps -f '{{if and (not .Standard) (ne .ImportPath "github.com/onembyte/kolkrabbi/protocol")}}{{.ImportPath}}{{end}}' ./protocol
make lint
make check
```

The final dependency filter printed nothing. The complete rerun passed with 897 tests, five compile
targets, zero lint issues, a 6.34 MB binary, 4.7 ms cold-start p50, one root dependency, 110 site
checks, 13 mode/update-surface checks, 56 installer checks, 24 release checks, 41 release-workflow
checks, and 30 release-verifier checks. The known sandbox-only module-cache warning remained
non-fatal.

### Next checkpoint

A6.3b2 applies the same no-duplication pattern to the already-frozen `score.recorded` evaluation.
Session, model, permission, chapter, and span entities remain correctly deferred to their owners.

---

## Architecture migration / A6.3b2 — score entity

**Status:** done, 2026-08-23 · **Protocol checks:** 637 · **Host tests:** 898 ·
**Dependencies:** standard library only · **User-visible changes:** none

The typed `score.recorded` evaluation is now a shared entity. `schemas/entities/score.json` owns the
nine fields and five conditional clauses, `Score` is the one public struct, and
`ScoreRecordedData` is an alias. Event schema and decoder both reuse the entity contract.

### TDD record

**Red:** the entity schema, compact entity golden, event-reference assertion, typed entity test,
alias identity proof, and byte-equality check landed first. `go test ./protocol` failed only because
`Score` and `validateScoreEntity` did not exist.

**Green:** score vocabularies, struct, and validation moved intact into `protocol/entity.go`.
Existing event tests still cover canonical session/turn targets, opaque spans, all four native JSON
value types, human/judge/implicit provenance, judge-model ownership, optional explanation, and
additive unknown fields through the shared validator.

**Refactor:** `score.recorded` now defines no entity fields; its four-key schema references
`entities/score.json`. The entity golden is byte-identical to event data, and `json.RawMessage`
continues to retain the decoded primitive without normalization.

### Verification

```sh
go test ./protocol -count=1
go test -race ./protocol -count=1
go vet ./protocol
go test ./internal/arch -count=1
make lint
go list -deps -f '{{if and (not .Standard) (ne .ImportPath "github.com/onembyte/kolkrabbi/protocol")}}{{.ImportPath}}{{end}}' ./protocol
make check
```

The dependency filter printed nothing. The complete gate passed with 898 tests, five compile
targets, zero lint issues, a 6.34 MB binary, 4.9 ms cold-start p50, one root dependency, 110 site
checks, 13 mode/update-surface checks, 56 installer checks, 24 release checks, 41 release-workflow
checks, and 30 release-verifier checks. The known sandbox-only module-cache warning remained
non-fatal.

### Next checkpoint

The two owner-stable shared entities are complete. A6.3b3-b6 remain dependency-gated rather than
guessed. The next independent A6 work is A6.3c command bodies whose target/event contracts are
already stable: turn creation/cancellation and permission resolution can be split and frozen one at
a time, while session fork/list remains deferred with the session entity.

---

## Architecture migration / A6.3c1 — permission resolve command

**Status:** done, 2026-08-23 · **Protocol checks:** 651 · **Host tests:** 912 ·
**Dependencies:** standard library only · **User-visible changes:** none

The first client-to-server command is now frozen. `permission.resolve` carries one non-empty opaque
pending request ID and one of the existing `allow`, `allow_session`, or `deny` decisions. It does
not accept a resolution reason: timeout and policy explanations are server-owned facts emitted on
the later `permission.resolved` event.

### TDD record

**Red:** command schema, compact golden body, typed round trip, all-decision matrix, malformed-field
matrix, forbidden-field schema checks, and additive-field validation landed first. The protocol
tests failed only because the command constant, type, binding, and validator did not exist.

**Green:** `protocol/command.go` now defines the command vocabulary and the two-field public
binding, reusing `PermissionDecision` rather than copying its enum. Missing, null, empty, wrongly
typed, and unknown correlation/decision values fail closed.

**Refactor:** the command remains transport-neutral. It does not yet decide how an HTTP path or
stdio command frame carries the name, how already-resolved conflicts surface, or how A8 stores and
expires pending requests.

### Verification

```sh
go test ./protocol -count=1
go test -race ./protocol -count=1
go vet ./protocol
go test ./internal/arch -count=1
make lint
go list -deps -f '{{if and (not .Standard) (ne .ImportPath "github.com/onembyte/kolkrabbi/protocol")}}{{.ImportPath}}{{end}}' ./protocol
make check
```

The dependency filter printed nothing. The complete gate passed with 912 tests, five compile
targets, zero lint issues, a 6.34 MB binary, 5.1 ms cold-start p50, one root dependency, 110 site
checks, 13 mode/update-surface checks, 56 installer checks, 24 release checks, 41 release-workflow
checks, and 30 release-verifier checks. The sandbox-only module-cache warning remained non-fatal.

### Next checkpoint

A6.3c2 can freeze `turn.cancel` as one canonical turn ID. Like permission resolution, it is forced
by mobile/native bindings and does not require the still-deferred session entity or turn-creation
semantics.

---

## Architecture migration / A6.3c2 — turn cancel command

**Status:** done, 2026-08-23 · **Protocol checks:** 661 · **Host tests:** 922 ·
**Dependencies:** standard library only · **User-visible changes:** none

`turn.cancel` is now a transport-neutral command carrying one canonical `turn_id`. It gives HTTP,
stdio, desktop, and mobile callers the value boundary they need without leaking a Go context or
accepting a client-authored cancellation reason.

### TDD record

**Red:** command schema, golden body, typed round trip, canonical-ID matrix, forbidden-field checks,
and additive-field validation landed first. The protocol tests failed only because the command
constant, binding, and validator did not exist.

**Green:** the command now validates through the same canonical typed-ID primitive as event
envelopes. Missing, null, empty, numeric, lowercase, short, session-prefixed, and task-prefixed IDs
fail; the canonical turn golden succeeds.

**Refactor:** cancellation reason, live-turn lookup, idempotency/conflict behavior, HTTP routing,
and `context.CancelFunc` ownership remain server/runtime work. The public command does not guess any
of those decisions.

### Verification

```sh
go test ./protocol -count=1
go test -race ./protocol -count=1
go vet ./protocol
go test ./internal/arch -count=1
make lint
go list -deps -f '{{if and (not .Standard) (ne .ImportPath "github.com/onembyte/kolkrabbi/protocol")}}{{.ImportPath}}{{end}}' ./protocol
make check
```

The dependency filter printed nothing. The complete gate passed with 922 tests, five compile
targets, zero lint issues, a 6.34 MB binary, 4.8 ms cold-start p50, one root dependency, 110 site
checks, 13 mode/update-surface checks, 56 installer checks, 24 release checks, 41 release-workflow
checks, and 30 release-verifier checks. The sandbox-only module-cache warning remained non-fatal.

### Next checkpoint

The two dependency-free commands are complete. Before leaving A6.3c at its explicit A10 blockers,
one closure test should prove exported command constants, the shipped-command catalog, command
schemas, IDs, and goldens are the same set with no placeholder for deferred commands.

---

## Architecture migration / A6.3c5 — command vocabulary closure

**Status:** done, 2026-08-23 · **Protocol checks:** 664 · **Host tests:** 925 ·
**Dependencies:** standard library only · **User-visible changes:** none

Protocol version 0 now publishes an ordered catalog of the two commands it actually ships:
`turn.cancel` and `permission.resolve`. A standard-library AST and filesystem conformance test
proves the exported constants, catalog values, schema filenames, canonical schema IDs, golden
filenames, JSON-object shape, and command validators agree exactly.

### TDD record

**Red:** the closure test landed first and failed only because `KnownCommandTypes` did not exist.

**Green:** the public catalog now returns commands in architectural order through a defensive copy.
Both goldens validate, and no missing or orphan contract file exists.

**Refactor:** `turn.create`, `session.fork`, and `session.list` remain absent rather than being
published with guessed session semantics. Adding any exported `Command…` constant or contract file
without completing its entire cross-representation set now fails the closure test.

### Verification

```sh
go test ./protocol -count=1
go test -race ./protocol -count=1
go vet ./protocol
go test ./internal/arch -count=1
make lint
go list -deps -f '{{if and (not .Standard) (ne .ImportPath "github.com/onembyte/kolkrabbi/protocol")}}{{.ImportPath}}{{end}}' ./protocol
make check
```

The dependency filter printed nothing. The complete gate passed with 925 tests, five compile
targets, zero lint issues, a 6.34 MB binary, 5.0 ms cold-start p50, one root dependency, 110 site
checks, 13 mode/update-surface checks, 56 installer checks, 24 release checks, 41 release-workflow
checks, and 30 release-verifier checks. The sandbox-only module-cache warning remained non-fatal.

### Next checkpoint

A6.3 is now explicitly parked at real subsystem dependencies instead of vague TODOs. A6.4 transport
closure can proceed independently on framing rules and conformance streams using the shipped event
and command catalogs; OpenAPI endpoint completion must still omit deferred session/turn-create
surfaces until A10 settles them.

---

## Architecture migration / A6.4a — single-event framing

**Status:** done, 2026-08-23 · **Protocol checks:** 668 · **Host tests:** 929 ·
**Dependencies:** standard library only · **User-visible changes:** none

`spec/stdio.md` now freezes exact event bytes for NDJSON and SSE. NDJSON is the validated compact
envelope plus one LF. SSE is decimal `id`, exact wire `event`, and one `data` line whose bytes equal
the NDJSON line after removing only its LF, followed by one blank line. Heartbeat is exactly the
non-event comment `: ping\n\n`.

### TDD record

**Red:** byte-exact NDJSON/SSE identity, Unicode/escaped-newline physical-line behavior, invalid
envelope rejection, heartbeat exactness, and heartbeat storage isolation tests landed first. They
failed only because the three framing functions did not exist.

**Green:** `EncodeNDJSON` and `EncodeSSE` both wrap the existing validated `Encode`; there is no
second JSON serializer. `EncodeSSE` derives its decimal ID and event name from the same envelope.
`SSEHeartbeat` returns fresh bytes on each call.

**Refactor:** one test initially assumed the existing message-delta golden used sequence 1; it
actually uses 412. Correcting the expected fixture value demonstrated that the encoder correctly
uses the envelope rather than a constant. All focused gates were rerun after that test correction.

### Verification

```sh
go test ./protocol -count=1
go test -race ./protocol -count=1
go vet ./protocol
go test ./internal/arch -count=1
make lint
go list -deps -f '{{if and (not .Standard) (ne .ImportPath "github.com/onembyte/kolkrabbi/protocol")}}{{.ImportPath}}{{end}}' ./protocol
make check
```

The dependency filter printed nothing. The complete gate passed with 929 tests, five compile
targets, zero lint issues, a 6.34 MB binary, 5.0 ms cold-start p50, one root dependency, 110 site
checks, 13 mode/update-surface checks, 56 installer checks, 24 release checks, 41 release-workflow
checks, and 30 release-verifier checks. The sandbox-only module-cache warning remained non-fatal.

### Next checkpoint

A6.4b can now define a bounded streaming decoder and multi-event fixtures against this fixed
grammar. It must handle NDJSON and Kolkrabbi's SSE blocks without silently accepting mismatched SSE
`id`/`event` metadata or unbounded lines.

---

## Architecture migration / A6.4b1 — bounded decoder grammar

**Status:** done, 2026-08-23 · **Protocol checks:** 700 · **Host tests:** 961 ·
**Dependencies:** standard library only · **User-visible changes:** none

The public protocol reader now decodes exact NDJSON or Kolkrabbi SSE through a callback without
collecting a stream. Envelope JSON is capped at 1 MiB in both transports. Complete LF termination,
SSE field order, canonical ID spelling, ID/sequence equality, event/type equality, and the exact
heartbeat block are checked before delivery.

### TDD record

**Red:** the normative reader rules and 32 focused checks landed first. The focused build failed
only on the absent `StreamFormat`, stream constants, size limit, stable overflow error, and
`DecodeStream` API.

**Green:** a small `bufio.Reader.ReadSlice` loop now accumulates only up to an explicit caller-owned
line bound, avoiding both Scanner's implicit 64 KiB ceiling and unbounded `ReadString` behavior.
NDJSON and SSE share the existing validated `Decode`; SSE comments are ignored only when they are
the exact heartbeat block.

**Refactor:** the callback-stop test deliberately asserts one delivery and original error identity,
not unread bytes in the underlying reader: a conforming buffered reader may prefetch while still
performing no later parse or callback. The 1 MiB limit was tested at the exact byte boundary and one
byte beyond in both transports.

### Verification

```sh
go test ./protocol -run 'TestDecode(NDJSON|SSE|Stream)' -count=1
go test ./protocol -count=1
go test -race ./protocol -count=1
go vet ./protocol
go test ./internal/arch -count=1
make lint
go list -deps -f '{{if and (not .Standard) (ne .ImportPath "github.com/onembyte/kolkrabbi/protocol")}}{{.ImportPath}}{{end}}' ./protocol
make check
```

The dependency filter printed nothing. The complete gate passed with 961 tests, five compile
targets, zero lint issues, a 6.34 MB binary, 4.7 ms cold-start p50, one root dependency, 110 site
checks, 13 mode/update-surface checks, 56 installer checks, 24 release checks, 41 release-workflow
checks, and 30 release-verifier checks. The sandbox-only module-cache warning remained non-fatal.

### Next checkpoint

A6.4b2 remains responsible for canonical whole-turn fixtures and cross-format fixture conformance.
The separately requested user-facing checkpoint now takes priority: make `kolk update` narrate its
current-version/check/update result and make the installer detect and upgrade an older installation.

---

## Update UX / U0.2e — narrated update progress

**Status:** done, 2026-08-23 · **Host tests:** 1,156 · **Dependencies:** unchanged ·
**User-visible changes:** `kolk update` and `/update` now narrate version checks

Both update surfaces print the running build version and the latest-release check before invoking
the existing updater. Equal versions say Kolk is up to date; a newer local build names both values;
successful replacement names the current-to-latest transition and installed path. Only an active
session asks for a restart.

### TDD record

**Red:** exact output and ordering assertions landed first. They failed at compile time only because
the app had no injectable current-version seam.

**Green:** `newApp` now reads the existing stamped `buildinfo` through one tiny function seam.
`applyUpdate` writes the two progress lines before calling the updater, while final rendering remains
shared by top-level and slash commands.

**Refactor:** the unchanged renderer distinguishes an equal latest release from a deliberately newer
local stable build, avoiding the false claim that an older release number is the running version.
No discovery, comparison, artifact, checksum, archive, or executable-replacement code changed.

### Verification

```sh
go test ./internal/cli -run 'Test(TopLevelUpdate|SlashUpdate)' -count=1
go test ./internal/cli -count=1
go test -race ./internal/cli -run 'Test(TopLevelUpdate|SlashUpdate)' -count=1
go vet ./internal/cli
go test ./internal/arch -count=1
make lint
make check
```

The focused test initially needed an unrestricted rerun solely because the managed sandbox refused
the existing local `httptest` listener. Assertions were green outside that network sandbox. The
complete gate passed with 1,156 enumerated tests, five compile targets, zero lint issues, a 6.34 MB
binary, 4.9 ms cold-start p50, one root dependency, 110 site checks, 13 mode/update-surface checks,
56 installer checks, 24 release checks, 41 release-workflow checks, and 30 release-verifier checks.

### Next checkpoint

T0.4b2 adds existing-version awareness to the website installer through its offline black-box
matrix. Explicit `KOLK_VERSION` remains a force/pinning path; only ordinary latest installation may
skip or avoid a downgrade.

---

## Installer / T0.4b2 — existing-install version awareness

**Status:** done, 2026-08-23 · **Installer checks:** 72 · **Host tests:** 1,156 ·
**Dependencies:** unchanged · **User-visible changes:** idempotent install/upgrade reporting

The website installer now asks an existing executable target for `kolk version` after resolving the
latest release and destination. An ordinary install upgrades older versions, returns before asset
downloads when equal, and refuses to replace a newer stable build. Explicit `KOLK_VERSION` still
forces the requested verified install.

### TDD record

**Red:** sixteen black-box assertions landed first across older, equal, newer, differing component
width, and explicit-pinning cases. The previous installer failed eight: it always downloaded and
replaced and had no version-aware output.

**Green:** stable installed identities are parsed from the existing keyless `kolk version` command.
A Bash-3.2-compatible comparator compares decimal component length and then equal-width digits, so
it neither overflows shell integers nor orders `0.10.0` below `0.9.10`. Equal/newer early returns
reuse one PATH guidance function; older versions continue through the existing checksum, archive,
and atomic replacement path.

**Refactor:** unreadable, malformed, non-stable, or failing existing executables do not become trust
signals and therefore cannot suppress a verified install. Pinned installs deliberately bypass the
skip/no-downgrade branch, retaining their reproducible exact-version meaning.

### Verification

```sh
bash -n site/install.sh
shellcheck site/install.sh scripts/test-installer.sh
./scripts/test-installer.sh
./scripts/test-site.sh
./scripts/test-v01-surface.sh
./scripts/test-release.sh
./scripts/test-release-workflow.sh
./scripts/test-release-verifier.sh
git diff --check -- site/install.sh scripts/test-installer.sh
make check
```

The complete gate passed with 1,156 enumerated tests, five compile targets, zero lint issues, a
6.34 MB binary, 4.9 ms cold-start p50, one root dependency, 110 site checks, 13 mode/update-surface
checks, 72 installer checks, 24 release checks, 41 release-workflow checks, and 30 release-verifier
checks. Bash syntax and ShellCheck were clean.

### Next checkpoint

Resume A6.4b2 with canonical whole-turn NDJSON/SSE fixture pairs and decoder conformance. Keep the
fixture slice independent of the future event bus, daemon, and HTTP server.

---

## Architecture migration / A6.4b2a — owner-stable turn streams

**Status:** done, 2026-08-23 · **Protocol checks:** 707 · **Host tests:** 1,163 ·
**Dependencies:** standard library only · **User-visible changes:** none

The language-neutral conformance suite now carries exact NDJSON/SSE twins for a simple code turn, a
denied Kolkrabbi-owned tool, and one parent/child agent fanout. Every stream is a contiguous
session-log sequence with monotonic timestamps, canonical transport bytes, one usage row per model
attempt, and an explicit final turn event.

### TDD record

**Red:** inventory, cross-format equality, canonical byte regeneration, lifecycle ordering,
permission correlation, agent turn scoping, and streamed-delta/final-message assertions landed
first. All three tests failed only because `spec/testdata/streams/` did not exist.

**Green:** three `.ndjson` sources and three exact `.sse` twins satisfy both strict decoders. Code
deltas concatenate to the authoritative completed message. The denied tool correlates request,
permission decision, and unsuccessful finish without a false start. Agent child lifecycle and usage
use the child turn; parent orchestration and final accounting return to the parent.

**Refactor:** canonical regeneration caught `.010Z`, which RFC3339Nano correctly renders as `.01Z`,
and strict SSE decoding caught one missing final blank-line LF. The fixtures were corrected rather
than weakening either rule. `saga-chapter` and `resume-after-drop` remain absent at explicit event
state and cursor/replay blockers.

### Verification

```sh
go test ./protocol -run 'Test(WholeTurn|CodeTurnFixture|PermissionDeniedFixture|AgentFanoutFixture)' -count=1
go test ./protocol -count=1
go test -race ./protocol -count=1
go vet ./protocol
go test ./internal/arch -count=1
make lint
go list -deps -f '{{if and (not .Standard) (ne .ImportPath "github.com/onembyte/kolkrabbi/protocol")}}{{.ImportPath}}{{end}}' ./protocol
git diff --check -- spec/testdata/streams protocol/stream_fixture_test.go
make check
```

The dependency filter printed nothing. The post-documentation complete gate passed with 1,163
tests, five compile targets, zero lint issues, a 6.34 MB binary, 5.0 ms cold-start p50, one root
dependency, 110 site
checks, 13 mode/update-surface checks, 72 installer checks, 24 release checks, 41 release-workflow
checks, and 30 release-verifier checks.

### Next checkpoint

A6.4c may now define only the OpenAPI endpoints supported by already-shipped commands and entities.
Session listing/detail and turn creation remain excluded until A10 owns their session format and
request semantics.

---

## Architecture migration / A6.4c — minimal OpenAPI shape

**Status:** done, 2026-08-23 · **Protocol checks:** 710 · **Host tests:** 1,166 ·
**Dependencies:** standard library only · **User-visible changes:** none

The first OpenAPI 3.1 contract publishes only the three routes whose input and output ownership is
already stable: hello, turn cancellation, and permission resolution. It uses JSON syntax, which is
valid YAML 1.2, so the standard-library-only protocol suite can parse the document without adding a
YAML dependency.

### TDD record

**Red:** exact path/method inventory, auth inheritance, response reuse, schema derivation,
command-catalog closure, deferred-path exclusion, and external-reference checks landed first. All
three focused tests failed only because `spec/kolk.openapi.yaml` did not exist.

**Green:** hello references the shipped payload and explicitly opts out of the global HTTP bearer
scheme. Cancellation derives its one canonical path ID from `turn.cancel` and has no duplicate JSON
body. Permission resolution derives its opaque path ID and decision-only body from
`permission.resolve`. Both mutations return an empty 204 and route every other response through the
shared safe error entity.

**Refactor:** the architecture wording now makes the hello exception consistent and distinguishes
the future target surface from each authoritative owner-stable cut. Session creation/list/detail,
SSE replay, models, stats, dashboard, and every secret-management path remain absent rather than
guessing contracts owned by later migration steps.

### Verification

```sh
go test ./protocol -run 'TestOpenAPI' -count=1
go test ./protocol -count=1
go test -race ./protocol -count=1
go vet ./protocol
go test ./internal/arch -count=1
make lint
go list -deps -f '{{if and (not .Standard) (ne .ImportPath "github.com/onembyte/kolkrabbi/protocol")}}{{.ImportPath}}{{end}}' ./protocol
git diff --check -- CHECKPOINTS.md docs/plan/02-architecture.md protocol/openapi_test.go spec/kolk.openapi.yaml spec/CHANGELOG.md
make check
```

The dependency filter printed nothing. The complete unrestricted gate passed with 1,166 tests,
five compile targets, zero lint issues, a 6.34 MB binary, 4.8 ms cold-start p50, one root
dependency, 110 site checks, 13 mode/update-surface checks, 72 installer checks, 24 release checks,
41 release-workflow checks, and 30 release-verifier checks.

### Next checkpoint

A6.4d adds the spec-change changelog guard and a whole-contract inventory, then runs the complete
A6 closure gate. It must not pull forward the dependency-blocked saga, session, replay, or server
work.

---

## Architecture migration / A6.4d — spec guard and owner-stable transport closure

**Status:** done, 2026-08-23 · **Protocol checks:** 711 · **Host tests:** 1,167 ·
**Spec-guard checks:** 29 · **Dependencies:** standard library only · **User-visible changes:** none

The owner-stable transport cut now has an exhaustive recursive `spec/` inventory and a committed
tree-to-tree changelog guard. A path-filtered read-only workflow compares the GitHub event base to
the checked-out head with full history, while ordinary CI and `make check` independently exercise
the guard implementation and its offline black-box matrix.

### TDD record

**Red:** the contract inventory and sixteen enforcement/workflow assertions landed first. The
inventory passed against the already-closed contract tree; all sixteen guard assertions failed
because no guard script, named Make target, CI step, or spec workflow existed.

**Green:** the Git guard validates explicit base/head treeish values, reads only committed trees,
requires a blob at `spec/CHANGELOG.md`, and distinguishes no contract change, documented change,
undocumented change, and invalid comparison failures. Its matrix covers modification, addition,
deletion, changelog-only changes, a missing changelog, an invalid base, and ignored dirty worktree
noise. The workflow uses pinned checkout/setup actions, read-only contents permission, full history,
and event-specific base selection including the empty tree for a repository's first push.

**Refactor:** ShellCheck caught one intentional literal containing GitHub environment-variable
syntax in the workflow test; the assertion now constructs the dollar sign explicitly with no
suppression. The recursive inventory derives events, commands, and stream pairs from their existing
catalogs and keeps only the currently owner-stable entities and foreign fixtures explicit.

### Verification

```sh
go test ./protocol -run 'Test(SpecContractInventoryIsClosed|OpenAPI|EventVocabulary|CommandVocabulary|WholeTurn)' -count=1
bash scripts/test-spec-change.sh
bash -n scripts/check-spec-change.sh scripts/test-spec-change.sh
shellcheck scripts/check-spec-change.sh scripts/test-spec-change.sh
make spec
go test -race ./protocol -count=1
go vet ./protocol
go test ./internal/arch -count=1
git diff --check -- .github/workflows/ci.yml .github/workflows/spec.yml CHECKPOINTS.md Makefile protocol/contract_inventory_test.go scripts/check-spec-change.sh scripts/test-spec-change.sh
make check
```

The complete unrestricted gate passed with 1,167 tests, five compile targets, zero lint issues, a
6.34 MB binary, 4.8 ms cold-start p50, one root dependency, 110 site checks, 13
mode/update-surface checks, 72 installer checks, 29 spec-guard checks, 24 release checks, 41
release-workflow checks, and 30 release-verifier checks.

### Next checkpoint

The A6.4 transport cut is closed. A6 remains open only for explicitly dependency-gated additions;
the next migration step is A7's internal event bus and byte-identical plain renderer, without
pulling forward A8 permissions, A10 session migration, or A11 serving surfaces.

---

## Release candidate / R1.1 — v1.1.0 installer-upgrade cut

**Status:** done, 2026-08-23 · **Requested label:** `v1.1` · **Release:** `v1.1.0` ·
**Release commit:** `638d12f` · **Workflow:** `32684499294` · **Host tests:** 1,167 ·
**Snapshot checks:** 21

The requested two-component label is normalized to the repository's mandatory three-part SemVer
tag. The binary has no independent hardcoded product version: GoReleaser stamps the immutable tag,
while protocol version remains `0`. Current surfaces now use the v1.1 release line without
rewriting historical v0.1 records.

### TDD record

**Red:** site and release-contract assertions changed first. The site matrix failed exactly because
the live badge still named v0.1, and the release matrix failed exactly because the snapshot identity
still used `0.1.0-dev`. The v1.1.0 installer, workflow, tag, and verifier fixture changes were
already green against their version-independent implementations.

**Green:** the website badge now names v1.1, untagged rehearsals stamp `1.1.0-dev.<commit>`, and
user-facing invalid-tag/version examples show v1.1.0. The installer matrix explicitly upgrades a
v0.1.0 binary to v1.1.0, skips downloads for an equal version, and leaves a v2.0.0 binary untouched.

**Refactor:** target release fixtures use named older/newer variables instead of repeating version
literals. Historical plan, protocol, and v0.1 release evidence remain unchanged.

### Pre-publication verification

```sh
./scripts/test-site.sh
./scripts/test-installer.sh
./scripts/test-release.sh
./scripts/test-release-workflow.sh
./scripts/test-release-verifier.sh
./scripts/check-release-tag.sh v1.1.0
GOCACHE=/private/tmp/kolkrabbi-go-cache \
  KOLK_GORELEASER_BIN=/private/tmp/kolk-goreleaser.2QHQK8/goreleaser \
  ./scripts/test-release-snapshot.sh
make check
```

The pinned GoReleaser v2.17.1 archive matched its official SHA-256 manifest. The snapshot produced
four `1.1.0-dev.8aa5533` archives and passed 21 archive, checksum, and host-identity checks. The
complete unrestricted gate passed with 1,167 tests, five compile targets, zero lint issues, a 6.34
MB binary, 4.4 ms cold-start p50, one root dependency, 110 site checks, 13 mode-surface checks, 72
installer checks, 29 spec-guard checks, 24 release checks, 41 workflow checks, and 30 verifier
checks.

### Publication and live verification

Candidate commit `638d12f` passed ordinary branch CI run `32684376303` before the annotated tag was
created. Tag `v1.1.0` peels to the exact 40-character candidate commit and release workflow run
`32684499294` completed both jobs successfully: the verify job reran the full repository gate and
four-archive rehearsal; the publish job keyless-signed the checksum manifest and then ran the
independent public verifier.

The public release is neither draft nor prerelease and contains exactly six assets: four versioned
Darwin/Linux amd64/arm64 archives, `checksums.txt`, and `checksums.txt.sigstore.json`. GitHub's
`/releases/latest` redirect resolves to `/releases/tag/v1.1.0`. The deployed installer SHA-256
`4016316b2025f2bf57365738c79d6fd9cce91d3a1e2f6816f52ebdcd0f05b740` is byte-identical to the
reviewed source.

The live installer was then exercised in an isolated temporary destination, not against the
owner's normal PATH:

```text
Downloading kolk v0.1.0 for darwin_arm64...
Installed kolk v0.1.0 to <temporary>/bin/kolk

Current version: 0.1.0
Updating kolk 0.1.0 → 1.1.0
Downloading kolk v1.1.0 for darwin_arm64...
Updated kolk 0.1.0 → 1.1.0 at <temporary>/bin/kolk
```

The upgraded executable reports `kolk 1.1.0`, commit
`638d12fd4d473ca75da0f0afa60481574d12fe71`, Darwin/arm64. A second installer run reports
`Kolk is up to date (1.1.0)`, and the shipped binary's own update command reports:

```text
Current version: 1.1.0
Checking for updates to latest version...
Kolk is up to date (1.1.0)
```

### Next checkpoint

R1.1 is closed and ready for the owner's real PATH test. Resume the dependency-ready A7 event-bus
migration one reversible TDD slice at a time; the still-open T0.5 clean-machine rehearsal remains a
separate environment proof.

---

## Architecture migration / A7.1 — bounded in-memory event journal

**Status:** done, 2026-08-24 · **Tests:** 1,175 · **Platforms:** 5 · **Lint:** 0 ·
**Binary:** 6.34 MB · **Cold start:** 4.6 ms p50

This leaf introduces the L2 `internal/bus` hinge without connecting it to the legacy engine or
changing one terminal byte. One journal owns one canonical protocol session, assigns contiguous
positive envelope sequences and nondecreasing UTC timestamps, validates complete envelopes through
the public protocol binding, and retains a count/byte-bounded replay window. `Subscribe(afterSeq)`
atomically returns the retained snapshot after a Last-Event-ID-style cursor and attaches a bounded
live channel; a full channel disconnects only that reader and leaves replay recovery available.

### TDD record

**Red:** `internal/bus/bus_test.go` was added before the package implementation. The focused test
failed at compile time only on the deliberately absent `Options`, `Bus`, `Event`, `Subscription`,
and cursor/backpressure errors. The first green attempt then exposed one test fixture with a
25-character rather than canonical 26-character ULID body; the fixture was corrected without
weakening runtime ID validation.

**Green:** `bus.go` now serializes concurrent publication under one mutex, computes retention from
the exact LF-terminated NDJSON frame size, rejects an individually unreplayable event before taking
its sequence, and snapshots replay while registering live delivery under the same lock. Count and
byte eviction, expired/ahead cursors, slow-reader isolation and resume, invalid clocks/payloads,
idempotent close, and concurrent ordering pass offline.

**Refactor:** payload bytes are cloned on input, retention, subscriber fan-out, replay access, and
the returned published envelope. The journal owns no goroutine, and `internal/arch` now registers it
at L2 with imports limited to the L0 typed-ID primitive and L1 protocol contract.

### Verification

```sh
GOCACHE=/private/tmp/kolkrabbi-go-cache go test ./internal/bus
GOCACHE=/private/tmp/kolkrabbi-go-cache go test -race ./internal/bus
GOCACHE=/private/tmp/kolkrabbi-go-cache go test ./internal/arch
GOCACHE=/private/tmp/kolkrabbi-go-cache go vet ./internal/bus
GOCACHE=/private/tmp/kolkrabbi-go-cache \
  GOLANGCI_LINT_CACHE=/private/tmp/kolkrabbi-lint-cache \
  golangci-lint run ./internal/bus/... ./internal/arch/...
GOCACHE=/private/tmp/kolkrabbi-go-cache \
  GOLANGCI_LINT_CACHE=/private/tmp/kolkrabbi-lint-cache make check
```

The full gate passed with 1,175 tests, Darwin/Linux amd64/arm64 plus advisory Windows/amd64,
zero lint issues, one root dependency, 110 site checks, 13 mode/update-surface checks, 72 installer
checks, 29 spec-guard checks, 24 release checks, 41 release-workflow checks, and 30 release-verifier
checks. The binary and startup budgets remain unchanged at 6.34 MB and 4.6 ms p50.

### Next checkpoint

A7.2 must implement the security plan's `bus.Publish` scrub chokepoint before the journal gains a
spill file or any engine, renderer, or transport consumer. The current package is an isolated,
in-memory seam and therefore creates no new persisted or user-visible secret surface.

---

## Terminal hotfix / U0.3c — Apple Terminal-compatible octopus

**Status:** published, 2026-08-24 · **Release:** `v1.1.1` · **Commit:** `0f0c87e` ·
**Workflow:** `32686977213` · **Tests:** 1,222 · **Snapshot checks:** 21 · **Platforms:** 5

The reported terminal captured every `🐙 thinking…` frame instead of replacing one activity
region. The renderer was emitting SCO `CSI s/u` cursor save/restore sequences, which Apple Terminal
may ignore. Because the engine writes `assistant ` before starting activity, clearing and repainting
the entire line would also destroy valid transcript output. The narrow fix uses the older, broadly
supported DEC save/restore pair and keeps erase-to-end cleanup scoped after that prefix.

### TDD record

**Red:** a compatibility model that deliberately ignores `CSI s/u` reproduced the screenshot: two
frames accumulated after `assistant ` and cleanup could not return the line to the prefix. Exact
byte assertions also failed while the renderer still emitted the unsupported pair.

**Green:** the renderer now emits DEC `ESC 7` / `ESC 8`. Two frames followed by an idempotent stop
leave exactly `assistant ` under Apple Terminal-compatible semantics. Fast replies, cancellation,
TTY gating, no-color output, and the engine-owned activity lifecycle retain their prior behavior.

**Release-version red/green:** candidate assertions were moved to `1.1.1` first. The release matrix
failed only on the old `1.1.0-dev` snapshot identity and the site matrix failed only on the old
badge. The production candidate and badge then moved to `1.1.1`; the installer matrix independently
proves a checksum-verified `1.1.0 → 1.1.1` replacement and an equal-version no-download result.

### Pre-publication verification

```sh
GOCACHE=/private/tmp/kolkrabbi-go-cache \
  go test ./internal/cli -run 'Test(Octopus|AttachInteractiveActivity)' -count=1
GOCACHE=/private/tmp/kolkrabbi-go-cache \
  go test -race ./internal/cli -run 'Test(Octopus|AttachInteractiveActivity)' -count=1
./scripts/test-release.sh
./scripts/test-site.sh
./scripts/test-installer.sh
./scripts/test-release-verifier.sh
./scripts/check-release-tag.sh v1.1.1
GOCACHE=/private/tmp/kolkrabbi-go-cache \
  GOLANGCI_LINT_CACHE=/private/tmp/kolkrabbi-lint-cache make check
GOCACHE=/private/tmp/kolkrabbi-go-cache \
  KOLK_GORELEASER_BIN=/private/tmp/kolk-goreleaser.2QHQK8/goreleaser \
  ./scripts/test-release-snapshot.sh
```

The complete gate passed with 1,222 tests, five compile targets, zero lint issues, one root
dependency, a 6.11 MB binary, 4.6 ms cold-start p50, 110 site checks, 13 mode/update-surface checks,
72 installer checks, 29 spec-guard checks, 24 release checks, 41 release-workflow checks, and 30
release-verifier checks. GoReleaser v2.17.1 produced exactly four
`1.1.1-dev.ab345b8` Darwin/Linux amd64/arm64 archives; all 21 archive, checksum, and host-identity
checks passed.

### Publication and live verification

Candidate commit `0f0c87e` passed ordinary branch CI run `32686840649` before the annotated tag was
created. Tag `v1.1.1` peels to exact commit
`0f0c87e7e77f04d729dd6ce60bd17a99bbb4ef83`; release workflow `32686977213` reran the complete gate,
rehearsed all four archives, keyless-signed the checksum manifest, published the release, and passed
the independent public verifier.

The release is neither draft nor prerelease and contains exactly six assets: the four versioned
Darwin/Linux amd64/arm64 archives, `checksums.txt`, and `checksums.txt.sigstore.json`. A live public
test installed `v1.1.0` into an isolated temporary directory and exercised the binary's own updater:

```text
Current version: 1.1.0
Checking for updates to latest version...
Kolk updated successfully (1.1.0 → 1.1.1)
kolk 1.1.1 (0f0c87e7e77f04d729dd6ce60bd17a99bbb4ef83, 2026-08-24T03:38:08Z) go1.25.0 darwin/arm64
```

A second update reported `Kolk is up to date (1.1.1)`. The exact unpinned public installer URL was
also run into a separate isolated directory and discovered, downloaded, checksum-verified, and
installed `v1.1.1` directly.

### Next checkpoint

Start U0.4 as separate terminal checkpoints: first the transcript/composer boundary, then recent
and prefix-filtered slash commands, and finally the visible status surface. The persistent UI was
intentionally not mixed into this cursor hotfix.

---

## Terminal UI / U0.4a — pure persistent-screen model

**Status:** done, 2026-08-24 · **Tests:** 1,227 · **Platforms:** 5 · **Dependencies:** 1 ·
**Binary:** 6.11 MB · **Cold start:** 4.4 ms p50

This leaf creates a dependency-free L6 screen model before terminal I/O exists. Transcript output,
ephemeral activity, compact status, and the exact input draft are independent regions. A bounded
view gives transcript rows up first and preserves the composer as the final region; narrow visual
wrapping never normalizes or changes the draft that will eventually be submitted.

### Framework spike

The official current releases were measured in temporary modules: Bubble Tea v2.0.9 and Bubbles
v2.2.0 cross-compile to Windows and stay under the binary-size budget, but the textarea prototype
expands the root graph to 18 modules. That fails the repository's hard two-module supply-chain
budget before production integration. The selected primitive is instead official
`golang.org/x/term` v0.45.0 behind `internal/term`; with the existing `x/sys` it keeps exactly two
modules, cross-compiles to Windows, and its standalone stripped prototype is 1.66 MB. U0.4a itself
adds no dependency; U0.4b owns the reviewed module addition.

### TDD record

**Red:** the first test failed only on the missing model API. It then proved transcript and activity
updates could not preserve a multiline draft because no region boundary existed. Two later red
cases showed unbounded transcript rows pushing the composer away and rune-count wrapping treating
`🐙` and combining accents as ordinary one-cell code points.

**Green:** `internal/tui.Model` now snapshots the four regions independently, retains only the
newest transcript rows that fit, keeps the composer at the bottom, wraps emoji/CJK as two cells and
combining/format code points as zero cells, and leaves stored draft bytes unchanged across status,
activity, output, width, and height changes.

### Verification

```sh
GOCACHE=/private/tmp/kolkrabbi-go-cache go test ./internal/tui -count=1
GOCACHE=/private/tmp/kolkrabbi-go-cache go test -race ./internal/tui -count=1
GOCACHE=/private/tmp/kolkrabbi-go-cache go vet ./internal/tui
GOCACHE=/private/tmp/kolkrabbi-go-cache go test ./internal/arch -count=1
GOCACHE=/private/tmp/kolkrabbi-go-cache \
  GOLANGCI_LINT_CACHE=/private/tmp/kolkrabbi-lint-cache make check
```

The complete gate passed with 1,227 tests, five compile targets, zero lint issues, one root
dependency, a 6.11 MB binary, 4.4 ms cold-start p50, and all existing site, installer, protocol,
release, workflow, and verifier contracts green.

### Next checkpoint

U0.4b adds the raw terminal boundary and editor event model behind strict TTY/fallback gating. It
must keep engine/provider policy out of `internal/tui`, preserve every non-interactive byte, and
remain within the two-module dependency gate before the existing REPL is switched over.

---

## Terminal UI / U0.4b–d — persistent runtime, slash discovery, and v1.1.2 candidate

**Status:** release candidate, 2026-08-24 · **Release target:** `v1.1.2` · **Tests:** 1,269 ·
**Snapshot checks:** 21 · **Platforms:** 5 · **Dependencies:** 2 · **Binary:** 6.22 MB ·
**Cold start:** 4.5 ms p50

This cut replaces the interactive line reader with a normal-screen terminal runtime while leaving
redirected and `TERM=dumb` invocations on the byte-compatible plain REPL. Transcript, ephemeral
octopus/tool work, status, slash suggestions, approval, and the multiline composer have separate
storage. Streamed output and tool activity can repaint while type-ahead remains in the fixed
composer; raw mode, bracketed paste, and cursor visibility are restored exactly once on every exit.

### TDD record

**Red:** runtime tests first failed on the absent event loop, renderer, raw terminal boundary, and
engine decision port. Editor tests then failed on fragmented UTF-8/escape/paste sequences and
multiline cursor/history behavior. Slash tests failed before recent-first prefix filtering and
keyboard selection existed. Live rehearsal found one further red case: successful turns were
marked interrupted because lifecycle was inspected after cleanup canceled the child context.

**Green:** `internal/term` owns `x/term` raw mode and size probes; `internal/tui` owns a bounded,
control-sanitized screen model, editor, decoder, controller, renderer, and concurrent runtime. `/`
shows recent commands, filters as letters arrive, selects with Up/Down, and completes with Tab or
an explicitly selected Enter. Empty-composer Up restores the last exact multiline message. One
Ctrl+C clears only the composer without canceling work; a second consecutive Ctrl+C exits. Tool
approvals have an isolated input overlay, and YOLO bypasses the engine decision port as before.

**Refactor:** slash help and discovery share one CLI-owned catalog. Visible model output now uses
`kolk-<mode>` rather than leaking the OpenAI `assistant` role. Tool calls become payload-safe logs
such as `Reading file — PLAN.md`, with a replaceable octopus work row while the action runs. ANSI
and cursor controls from provider/tool text are removed before rendering, transcript retention is
bounded to a valid UTF-8 tail, and engine/provider policy remains outside `internal/tui`.

### Verification

```sh
GOCACHE=/private/tmp/kolkrabbi-go-cache go test -race \
  ./internal/cli ./internal/engine ./internal/term ./internal/tui -count=1
GOCACHE=/private/tmp/kolkrabbi-go-cache \
  GOLANGCI_LINT_CACHE=/private/tmp/kolkrabbi-lint-cache make check
KOLK_GORELEASER_BIN=/private/tmp/kolk-goreleaser.olLZpM/goreleaser \
  GOCACHE=/private/tmp/kolkrabbi-go-cache ./scripts/test-release-snapshot.sh
```

The full gate passed 1,269 tests, Darwin/Linux amd64/arm64 plus advisory Windows/amd64, zero lint
issues, two root modules, a 6.22 MB stripped binary, 4.5 ms cold-start p50, 110 site checks, 13
surface checks, 72 installer checks, 29 spec checks, 24 release checks, 41 workflow checks, and 30
verifier checks. GoReleaser v2.17.1 produced exactly four `1.1.2-dev.e2ce552` archives; all 21
archive, checksum, member, and host-identity checks passed.

An isolated pseudo-terminal rehearsal used the offline `cmd/kolk-mock` server. It verified `/`
filtering, keyboard selection, mode/status updates, Up history, composer-only first Ctrl+C,
double-Ctrl+C exit, `kolk-code` responses, `Writing file — hello-from-mock.txt`, ephemeral tool
activity, a fixed composer during streaming, and a final `ready` lifecycle with no API call or cost.

### Next checkpoint

Commit and push only the reviewed U0.4/A8 decision-port/release files, wait for ordinary branch CI,
then create immutable tag `v1.1.2`. The release workflow must rerun the full gate, publish exactly
the signed six-asset release, and pass public updater plus installer upgrade rehearsals before
U0.4d closes.

### Publication evidence

Commit `ceb8c6b` reached `main`; branch CI run `32691049361` completed successfully with Linux,
macOS, lint, budgets, platform compilation, installer, site, protocol, release, workflow, signature,
and module-tidiness jobs green. The tag preflight accepted `v1.1.2`, then annotated tag `v1.1.2`
started release run `32691207514`. Its verify job reran the complete gate and rehearsed all four
archives; its publish job uploaded and independently verified four archives, `checksums.txt`, and
the Sigstore bundle. The public release is neither draft nor prerelease.

The public website installer was then exercised only inside
`/private/tmp/kolk-v112-public.XMWui6`: a pinned `v1.1.1` install reported its exact version, `kolk
update` replaced it with `v1.1.2`, `kolk version` reported commit `ceb8c6b`, and a second update
reported `Kolk is up to date (1.1.2)`. A separate unpinned invocation of the exact public install
command selected and installed `v1.1.2`. No developer binary, key, config, session, or PATH entry was
changed during the rehearsal. U0.4d is complete; Markdown/diff rendering remains an independent
open acceptance item under U0.4.

---

## Terminal UI / U0.4e — spinner-only free default and v1.1.3 candidate

**Status:** release candidate, 2026-08-24 · **Release target:** `v1.1.3` · **Clean-tree tests:**
1,021 · **Snapshot checks:** 21 · **Platforms:** 5 · **Dependencies:** 2 · **Binary:** 6.45 MB ·
**Cold start:** 4.5 ms p50

This patch removes every user-visible octopus and phase label from loading. Plain and persistent
terminal paths now show only the animated Braille spinner, while descriptive file and command work
remains durable transcript output. The persistent runtime owns exactly one cancellable activity
generation, so a stale or repeated stop cannot erase a newer spinner.

New sessions without an explicit model now query OpenRouter's intelligence-ranked, tool-capable,
text model catalog within a five-second deadline. Selection makes zero cost the first invariant,
then prefers coding suitability and catalog intelligence order. Only documented `:free` variants
and `openrouter/free` are trusted as guaranteed free; a temporary catalog price of zero is not.
The former all-tier `stealth/ox-alpha` preset is retired in memory because observed billing proved
that alias unsafe, while mixed tier maps, saved custom models, resumed models, and `--model` remain
exact user choices. Catalog failure falls back to `openrouter/free`; a provider with no free usable
model gets its cheapest candidate only after a visible charges-may-apply warning.

### TDD record

**Red:** fake-clock tests first failed on decorated `🐙 thinking…`/tool activity and the absence of
an interactive animation lifecycle. Catalog-policy tests then failed on the old paid-capable
`openrouter/auto` default, absent ranking filters, unsafe inference from zero-valued pricing, and
the stale documented preset overriding discovery.

**Green:** both activity implementations now emit only `⠋` through `⠏`; generation IDs, context
cancellation, and idempotent cleanup protect overlapping persistent activity. Provider decoding now
includes tool support and all relevant price components. Pure selection tests cover free-before-paid,
coding/tool preference, explicit-free trust, legacy exclusion, and cheapest-paid fallback; local
HTTP tests cover the exact ranked query, response decoding, outage behavior, precedence, migration,
and pre-turn warnings.

**Refactor:** catalog policy lives in the CLI, HTTP mechanics remain in the provider client, and the
engine still receives one resolved model. Spinner clocks are replaceable test seams. Release-facing
fixtures advance together from `v1.1.2` to `v1.1.3`.

### Verification

```sh
GOCACHE=/private/tmp/kolkrabbi-go-cache go test -race \
  ./internal/cli ./internal/provider ./internal/tui -count=1
GOCACHE=/private/tmp/kolkrabbi-go-cache \
  GOLANGCI_LINT_CACHE=/private/tmp/kolkrabbi-lint-cache make check
KOLK_GORELEASER_BIN=/private/tmp/kolk-goreleaser.olLZpM/goreleaser \
  GOCACHE=/private/tmp/kolkrabbi-go-cache ./scripts/test-release-snapshot.sh
```

The focused race gate passed. A fresh Git export containing only this checkpoint then passed the
complete 1,021-test gate, Darwin/Linux amd64/arm64 plus advisory Windows/amd64 compilation, zero
lint issues, two root modules, the 6.45 MB size and 4.5 ms cold-start budgets, and all site,
surface, installer, specification, release, workflow, and verifier contracts. GoReleaser v2.17.1
produced exactly four `1.1.3-dev` archives and passed all 21 snapshot checks.

An isolated pseudo-terminal rehearsal against `cmd/kolk-mock` showed one animated spinner cell with
no octopus, `thinking` label, or phase text; durable `Writing file — hello-from-mock.txt` output;
successful tool execution; final `ready`; composer-only first Ctrl+C; and double-Ctrl+C exit. A
separate real-catalog startup used the installed key only to list models, selected
`cohere/north-mini-code:free`, and exited before any inference request, so that rehearsal incurred
no model cost.

### Next checkpoint

Push only the U0.4e files, require ordinary branch CI to pass, then create immutable tag `v1.1.3`.
The release workflow, public `v1.1.2 → v1.1.3` updater rehearsal, second no-op update, and fresh
public installer must pass before U0.4e closes.

### Publication evidence

Commit `80213d1` reached `main`; branch CI run `32693114586` completed successfully with Linux,
macOS, lint, budgets, platform compilation, architecture, installer, site, protocol, release,
workflow, signature, and module-tidiness jobs green. The tag preflight accepted `v1.1.3`, then
annotated tag `v1.1.3` started release run `32693216415`. Its verify job reran the complete gate and
rehearsed all four archives. Its publish job uploaded and independently verified four archives,
`checksums.txt`, and `checksums.txt.sigstore.json`; the public release is neither draft nor
prerelease.

The live website installer was exercised only inside `/private/tmp/kolk-v113-public.k5Qxvv`.
A pinned `v1.1.2` install reported commit `ceb8c6b`; `kolk update` printed the current version,
checked the latest release, and replaced it with `v1.1.3`; `kolk version` then reported commit
`80213d1`; and a second update reported `Kolk is up to date (1.1.3)`. A separate unpinned invocation
of the public installer selected and installed `v1.1.3`. No developer binary, API key, config,
session, or PATH entry changed during this rehearsal. U0.4e is complete.

---

## Architecture migration / A7.2a — pure durable secret scanner

**Status:** complete, 2026-08-24 · **Shared-tree tests:** 1,296 · **Final clean-patch tests:** 1,320 ·
**Platforms:** 5 · **Dependencies:** 2 · **Binary:** 6.24–6.26 MB · **Cold start:** 4.9–5.1 ms p50

This leaf moves arbitrary-text credential scrubbing out of `internal/secret` and into the pure,
standard-library-only `internal/redact` package. The embedded key-shape table now supplies explicit
minimum suffix and alphabet facts for every inference and denial prefix. Durable scrubbing applies
exact process-known literals first, then shaped credentials, Bearer tokens, recognized JWTs and
private-key blocks, and finally keyword assignments. Placeholders and a committed false-positive
corpus remain byte-exact unless an exact registered literal matches them.

Matches become stable, idempotent, process-salted HMAC sentinels with a safe label and short
correlation fingerprint. They retain no reusable credential prefix or suffix. `secret.New`
registers its trimmed value at construction, so every credential decoded by the existing file store
is known before a provider request. The random salt now fails closed instead of silently accepting
an unseeded cross-process fingerprint. Architecture tests enforce that `internal/redact` remains
stdlib-only and regexp-free and that `internal/bus` cannot import credential-owning packages.

### TDD record

**Red:** the scanner contract tests initially had no `redact.Scrub` or registration API, while
`secret.Scrub` owned a regular-expression alternation with no durable keyword, JWT, private-key,
placeholder, exact-literal, or process-salted-sentinel semantics. Shape-table boundary tests also
required length/alphabet facts that the embedded rows did not contain.

**Green:** a first-byte-gated scanner now proves every embedded infer/deny shape, durable-only
pattern, exact literal, sentinel correlation, placeholder exception, minimum-length/alphabet
boundary, malformed UTF-8 case, and private-key surrounding bytes. Construction wiring is covered
from `secret.New`, and concurrent registration/scrubbing is exercised directly under the race
detector.

**Refactor:** pattern construction derives from the single embedded table; labels are sanitized;
known literal buckets are longest-first; text without a match returns unchanged with no allocation;
and provider/bus layers receive no scanner policy or credential type.

### Verification

```sh
GOCACHE=/private/tmp/kolkrabbi-go-cache go test -race \
  ./internal/redact ./internal/secret ./internal/arch -count=1
GOCACHE=/private/tmp/kolkrabbi-go-cache go test ./internal/redact \
  -run '^$' -fuzz '^FuzzScrubPreservesValidUTF8AndIdempotence$' -fuzztime=10s
GOCACHE=/private/tmp/kolkrabbi-go-cache go test ./internal/redact \
  -run '^$' -bench BenchmarkScrub12KiB -benchmem -count=5
GOCACHE=/private/tmp/kolkrabbi-go-cache \
  GOLANGCI_LINT_CACHE=/private/tmp/kolkrabbi-lint-cache make check
```

The focused race gate passed. The bounded fuzz campaign completed 580,934 executions with no
crasher, invalid-UTF-8 regression, or idempotence failure. Five benchmark samples measured
154.75–157.80 MB/s for 12 KiB with zero allocations. The shared tree passed 1,296 tests; a fresh
HEAD export containing the final A7.2a files independently excluded concurrent TUI work and passed
1,320 tests, all five compile targets, zero lint issues, budgets, and every site, installer,
protocol, release, workflow, and verifier contract.

The adjacent ox-alpha session independently verified the scanner working tree twice before closure:
focused race/fuzz coverage, platform compilation, lint, budgets, and complete repository gates were
green with no implementation edits. A7.2a closes here. A7.2b is the next security leaf and owns only
JSON string preservation; it must not be mixed with the requested TUI status/composer checkpoint.

---

## Terminal UI / U0.4f — bounded background-output hotfix

**Status:** complete, 2026-08-24 · **Release:** `v1.1.4` · **Clean tests:** 1,321 ·
**Platforms:** 5 · **Dependencies:** 2 · **Binary:** 6.24 MB · **Cold start:** 9.7 ms p50

A live Kolk session froze after running a local mock-server rehearsal shaped as
`cd ... && nohup go run ... &`, followed by foreground checks. Process and descriptor inspection
proved that the direct shell had exited but its orphaned background compound-list wrapper still
owned Kolk's `CombinedOutput` pipe. Because `os/exec` had already observed the direct child's exit,
the ordinary 120-second command context no longer bounded the pipe-copy wait.

### TDD record

**Red:** `TestSuccessfulBackgroundListCannotFreezeOutputCapture` reproduces the same compound-list
grammar with a three-second background process. Before the fix, the foreground completed in roughly
50 ms but `Run` returned after 3.02 seconds, failing the two-second bound.

**Green:** every shell command now gets a 500 ms post-exit `WaitDelay`. When the direct child exits
successfully but a descendant retains stdout/stderr, Kolk closes only its capture side, keeps the
result successful, preserves captured foreground output, and appends a durable note that the
background process may still be running. The regression returns in 0.56 seconds.

**Refactor:** foreground timeouts, cancellation, Unix process-group teardown, non-zero exit
classification, and intentional background-process lifetime remain unchanged. The detachment case
is handled at the one `internal/shell` execution chokepoint and is visible to every bash tool caller.

### Verification

```sh
GOCACHE=/private/tmp/kolkrabbi-go-cache go test ./internal/shell \
  -run '^TestSuccessfulBackgroundListCannotFreezeOutputCapture$' -count=1 -v
GOCACHE=/private/tmp/kolkrabbi-go-cache go test -race ./internal/shell -count=1
GOCACHE=/private/tmp/kolkrabbi-go-cache \
  GOLANGCI_LINT_CACHE=/private/tmp/kolkrabbi-lint-cache make check
KOLK_GORELEASER_BIN=/private/tmp/kolk-goreleaser.olLZpM/goreleaser \
  GOCACHE=/private/tmp/kolkrabbi-go-cache ./scripts/test-release-snapshot.sh
```

A fresh Git export containing only the committed base and U0.4f candidate passed 1,321 tests,
architecture/purity/build-tag checks, Darwin and Linux amd64/arm64 plus advisory Windows/amd64,
zero lint issues, budgets, and all site, surface, installer, protocol, specification, release,
workflow, and verifier contracts. GoReleaser v2.17.1 produced four `1.1.4-dev` archives and all 21
snapshot checks passed.

### Publication evidence

Commits `1700653` and `70ab704` reached `main`; branch CI run `32695380700` passed Ubuntu and
macOS tests, lint, budgets, architecture, platform compilation, site, installer, protocol, release,
workflow, verifier, and module checks. Annotated tag `v1.1.4` triggered release run `32695492111`;
its verify job reran the full gate and all four archives, then its publish job uploaded and
independently verified the signed release artifacts.

The live installer was exercised only inside `/private/tmp/kolk-v114-public.3gBrlT`. A pinned
v1.1.3 install reported commit `80213d1`; `kolk update` printed the current version and replaced it
with v1.1.4 commit `70ab704`; a second update reported `Kolk is up to date (1.1.4)`; and a fresh
unpinned install selected v1.1.4. The Cloudflare homepage advertises v1.1.4 and live `install.sh`
returns HTTP 200 with `Cache-Control: no-store`. No developer binary, key, config, session, or PATH
entry changed during the rehearsal. U0.4f closes here.

---

## Terminal UI / U0.4g — persistent purple composer and raw-row fix

**Status:** release candidate, 2026-08-24 · **Target:** `v1.1.5` · **Focused race:** green ·
**PTY:** green · **Platforms:** 5 · **Dependencies:** 2

The owner reported that v1.1.4 opened with every line shifted farther right and then repeated the
startup banner while text was typed. A raw-terminal inspection showed that the renderer emitted LF
without CR after terminal output post-processing had been disabled. Cursor-up cleanup therefore
started from the wrong column and missed the rows it owned.

### TDD record

**Red:** the first real PTY rehearsal exposed a separate submission fault: Apple Terminal could
deliver ordinary Enter as bare LF, but the decoder classified LF as a multiline key. The focused
decoder regression passed for CR and failed for LF. After that fix, a renderer regression proved
that a three-row frame still emitted `top\nmiddle\nbottom` rather than the raw-mode-safe
`top\r\nmiddle\r\nbottom`. The full architecture gate then caught a direct `os.UserHomeDir` call
outside the one platform owner.

**Green:** bare CR and LF now submit; the existing explicit Shift+Enter escape sequences still add
newlines. The renderer converts logical LF boundaries to CRLF only at the terminal-write boundary,
preserving pure model output and owned-row counts. Home-directory discovery is exposed through
`internal/paths`, keeping the working-folder label compact without weakening the OS-owner rule.

**Refactor:** the boxed composer is replaced by two full-width text rules. Session name and current
model occupy the first footer row; effort, working folder, approval, and lifecycle occupy the second.
Purple ANSI is applied only by the terminal render path to rules, spinner, suggestions, and status;
the pure view, transcript, Markdown/diffs, and typed draft remain unstyled. The duplicated startup
mode/model/session banner is removed because those values now persist in the footer.

### Candidate verification

Focused race tests pass after the final path-owner refactor across TUI, paths, architecture, and the
affected CLI surface. A fresh isolated mock server, config, data directory, work directory,
binary, and `expect` PTY session submitted `create the hello file`, wrote
`hello-from-mock.txt`, streamed descriptive tool activity and the final response, changed the footer
from the session ID to `create the hello file`, and exited on the second Ctrl+C. Its capture contains
CRLF row boundaries and no old box, octopus, `thinking` label, or duplicated startup banner.

The release contracts were advanced test-first: release and site contracts failed against 1.1.4,
then passed at 1.1.5 (release 24, site 110, installer 72, verifier 30). Architecture, purity,
build-tag, five-platform, lint, budget, site, surface, installer, spec, release, workflow, and
verifier gates are green; binary size is 6.27 MB, cold start is 4.4 ms p50, and the dependency count
remains two. The full test target cannot yet be re-run in this sandbox because its local `httptest`
listeners require an unavailable escalation approval. No tag or public-release claim is made until
that clean gate, snapshot, CI, signed assets, updater, and installer rehearsal pass.

### M8.2 free-model routing checkpoint

The ranking contract now rejects paid, non-tool-capable, and sub-32k candidates,
then applies deterministic coding/context/ID ordering. Rotation tests cover
per-turn candidate bounds and pinned-model protection. Focused race tests for
provider, engine, and CLI packages and the full `go test ./... -count=1` suite
passed on 2026-08-26.

### R1.2 v1.2.0 capability release

**Review:** `site/capabilities.html` was re-checked card by card against the source, not against
`PLAN.md`. The page had last been touched at E13.2 and still named `yolo` as the way to skip a
confirmation — a surface `--yolo is gone` had removed — while listing as planned or designed the
work of phases C, D, E, F, G and I. Cards moved to "available now" only where the behavior exists in
the current binary and the offline suite covers it; the gated half is stated on the card instead of
being quietly dropped (localia's unpinned runtime, the gateway key a subscription session still
expects, a paired device that watches and approves but cannot send a turn). A REACH section was added
for item 26, which the page had never covered.

**Ratchet:** `scripts/test-site.sh` now requires the reach section and the six shipped claims most
likely to be lost in a future rewrite — orchestrated runs, saga, permission rules, the dashboard, the
event service, per-device tokens — and excludes `yolo` from the catalog outright. The site contract
went from 110 to 118 checks. `README.md` lost the same class of stale claim: no compaction,
sequential subagents, a `-y` flag, quick/standard/deep/ultra widths, and a fixed 120 s bash timeout.

**Gate:** `make check` green before the tag — 1992 root-module tests, binary 8.65 MB, cold start
3.2 ms p50, two third-party modules, site 118, surface 15, installer 72, spec 29, release 24,
workflow 41, verifier 30.

**Publication:** commit `792a53c4` on `main`, tag `v1.2.0` on that exact commit. The release workflow
(run 33027192164) passed both jobs: tag validation, the complete repository gate, GoReleaser
validation and a four-archive rehearsal, then publication and the independent verifier over the
published assets. `checksums.txt`, its Cosign bundle, and the four Darwin/Linux amd64/arm64 archives
are public, the release is neither draft nor prerelease, and GitHub's latest redirect resolves to
`v1.2.0`, which is what the website installer discovers.

### R1.3 v1.2.1 composer release

**Gate:** `make check` green before the tag — 2022 root-module tests, 0 lint issues, cold start
3.3 ms p50, site 137, release 24, and every other contract.

**Publication:** commit `ce42b9c0` on `main`, tag `v1.2.1`. Release workflow run 33065757135 passed
both jobs, including the independent verifier over the published assets. Four Darwin/Linux
amd64/arm64 archives plus the Cosign-signed `checksums.txt` are public, the release is neither draft
nor prerelease, and the latest redirect resolves to `v1.2.1`.

**A wrong diagnosis, corrected the same day.** Before tagging, `git merge-base --is-ancestor v1.2.0
HEAD` failed and `git describe --tags --abbrev=0 HEAD` resolved to `v1.1.6`, so this session reported
that a force-rewrite had orphaned nine published tags, and said so in the release notes. It had not.
The rewrite moved every tag onto its rewritten equivalent, and `git diff` proves the trees are
byte-identical. The stale refs were local: `git fetch` will not move an existing local tag without
`--force`, so this clone kept pointing at pre-rewrite commits that exist in no other repository.
`git fetch --tags --force` fixed it; `git describe` then reported `v1.2.1` and every tag was
reachable. The check that would have caught it in one command is `git ls-remote --tags origin`,
which reads the published refs instead of the cached ones.

**Also predicted wrongly:** the generated changelog was expected to span `v1.1.6..v1.2.1` and be
useless. GoReleaser resolved the previous release correctly and listed the six real commits. The
notes were replaced by hand anyway; commit subjects are not what an upgrader reads.

## Agent-mode field report — three defects from one run (2026-08-28)

One agent-mode run reported by the user produced all three. The session was killed at 02:51 after it
had been running long enough to look hung. The directory it was building, `~/ecommerce-webapp`,
contained **eighteen directories and zero files** — the run had created and re-created the same
skeleton without ever writing code, which is the first defect below observed directly.

### A33.1 the doom-loop guard missed alternating cycles

**Scope:** a tool cycle is stopped whether or not the repeated call follows itself. Non-goals:
changing what counts as a repeat, or the once-per-loop reporting contract.

**Red:** the guard compared each call only against the one before it, so `d.repeats` reset on every
alternation. A probe drove `bash` and `list_dir` alternately ten times: the guard fired zero times.
`TestAnAlternatingCycleIsALoop` and `TestAThreeCallRotationIsALoop` reproduce it.

**Green:** a nine-call window — a three-call rotation seen three times, `doomThreshold * 3` — and a
count of how often each settled signature comes round again. Both halves of the existing rule still
apply inside the window: arguments *and* result must match, so a test that fails differently each run
is still progress rather than a loop.

**Two bugs the new tests caught in the fix itself**, both worth recording because each would have
shipped as a regression:

1. The first `wouldRepeat` counted by tool and arguments and ignored the result. That broke the
   documented invariant directly and failed the existing
   `TestAPendingCallIsNotARepeatWhenTheResultsMoved`. A command whose output keeps moving is
   progressing; counting it by arguments alone would stop a test run that is fixing itself one
   failure at a time.
2. A six-call window could not see a three-call rotation three times, and the `reported` flag was
   cleared on every differing call, so one cycle reported six times instead of once —
   prompt-per-lap noise, which is exactly what the single report exists to prevent.

### A33.2 the spinner vanished while work was still running

**Scope:** the activity row reflects whether anything is running. Non-goal: showing more than one
activity at a time.

**Red:** the row was one slot with one owner. Agent mode runs up to three subagents concurrently, so
the first to finish blanked the row while the others worked, and their animators had already stood
down when a newer activity replaced them. The session then looked frozen for the rest of the turn.
A probe confirmed it: two overlapping activities, stop the newer, row `""`.
`TestFinishingOneOfSeveralActivitiesKeepsTheRow` covers it.

**Green:** every in-flight activity is tracked. The row shows the newest — the most specific
description of what is happening — and falls back to whatever is still running when that one ends,
going blank only when nothing is left. One animator serves them all, spawned and retired under the
same lock that adds and removes activities, so a start racing a retirement either spawns a
replacement or is seen by the incumbent, never neither.

**A deadlock the fix introduced, caught by an existing test.** Making a stop join the animator meant
it waited for the animator to notice the empty list — which it only did on its next frame. Against
the injected clock in `TestRuntimeToolWorkUsesOnlyTheEphemeralActivityRegion` no next frame ever
came, and the package hung rather than failed. Emptying the list now wakes the animator directly.
`TestStoppingTheLastActivityDoesNotWaitForATick` pins it. Worth noting that the symptom was a ten
minute timeout, not a failure: a hang reads as an infrastructure problem and is easy to misattribute.

### A33.3 the transcript was overwritten instead of scrolled

**Scope:** output that leaves the frame reaches the terminal's scrollback. Non-goals: scrollback
navigation inside kolk, and any change to what the frame itself shows.

**Red:** the frame is repainted in place. Once the transcript filled the screen, every new line
shifted the rest up a row and the top one was overwritten — the "printing upwards" in the report.
It also meant nothing that scrolled past could be read afterwards, because it had never been written
to the terminal at all, only drawn over.
`TestOutputThatLeavesTheFrameIsCommittedNotOverwritten` renders sixty lines through a ten-row frame
and asserts every one reached the writer.

**Green:** what no longer fits is cut from the transcript and printed above the frame in the same
write, so the terminal scrolls it into history. The cut is only ever made at a block boundary — a
point where the markdown renderer holds no state — so what goes to scrollback renders exactly as it
did on screen. `TestCuttingAtABoundaryChangesNothing` checks that invariant directly across seven
samples at two widths. An unclosed fence is not a boundary, since it is still streaming; a single
block taller than the screen is left whole and clipped as before rather than committed in half.

**One-write ordering:** the committed lines and the new frame go out in a single write, so a line is
never on screen twice and never missing for a frame.

### Verification

`make fmt-check`, `make vet`, `make arch`, `make purity`, `make buildtags`, `make budgets` and
`./scripts/test.sh` all green — 2340 tests across all modules. `internal/tui` and `internal/engine`
additionally under `-race -count=2`.

**A false diagnosis, corrected within the minute.** A per-test hang hunt reported all five new
activity tests as HANG/FAIL. They were passing; macOS has no `timeout(1)`, so every invocation had
failed with `command not found` and the loop read a non-zero exit as a hang. `go test -timeout` is
the portable form and named the one genuinely hanging test immediately.

### A33.4 questions the user answers by picking, not by typing

**Scope:** the model can put a fixed-option decision to the person, who answers with the arrow keys.
Non-goals: free-text questions, which prose already handles; questions from subagents; more than one
question at a time.

**Red:** there was no way for the model to ask anything. It asked in prose, and the answer had to be
typed back — which meant re-reading the options, choosing a spelling, and hoping the model read it
the way it was meant. `TestArrowKeysAndEnterAnswerTheQuestion` is the smallest statement of what was
missing.

**Green:** an `ask_user` tool carrying a question and two to eight options, answered by the surface
rather than by the tools package, because the answer comes from a person and nothing below the
surface can reach one. The engine intercepts the call before the work indicator and the confinement
guard, neither of which means anything for a question waiting on a human. The screen shows a picker:
`↑`/`↓` move, Enter takes the highlighted row, the number beside a row takes it outright, Ctrl+C
dismisses. The first option is preselected and the model is told to put its recommendation there, so
Enter alone is a real answer rather than an accident.

**Four ways to get this wrong, each pinned by a test:**

- **Dismissing is not choosing.** Reporting a dismissal as the first option would put words in the
  user's mouth and let the model build on a decision nobody made. `ok` is carried separately from the
  answer the whole way through, and the model is told to decide and say what it assumed.
- **Subagents never ask.** Up to three run at once, so two asking would race for one terminal, and an
  answer could not say which of three parallel tasks it belonged to. `mayAsk` is false on the
  subagent path.
- **A second question is refused, not queued.** Stacking questions in front of the person is worse
  than making the model wait for the first answer.
- **An interrupted turn takes its picker down.** Otherwise it sits on screen waiting for an answer
  nobody is listening for.

A question with fewer than two distinct options, or none, is refused before it reaches the screen and
the model is redirected to prose: blank and repeated options are choices that cannot be made
meaningfully.

**Ratchet:** `TestDoctorReportsWhatSchemasCost` asserts the tool count literally and went from five
to six. That is the assertion working — a new tool is sent on every request of every turn, so adding
one should require saying so out loud. `make budgets` and the schema budget test confirm the sixth
schema still fits.

**Verification:** all gates green — `fmt-check`, `vet`, `arch`, `purity`, `buildtags`, `budgets`,
`surface`, `plan-check`, `spec`, and `./scripts/test.sh` at 2356 tests. `internal/tui`,
`internal/engine` and `internal/cli` additionally under `-race -count=2`.

### R1.4 v1.2.15 agent-mode release

**Gate:** `make check` green before the tag — 2356 tests across all modules, plus `internal/tui`,
`internal/engine` and `internal/cli` under `-race -count=2`.

**Publication:** commit `b16d1ec` on `main`, tag `v1.2.15` on that exact commit. Release workflow run
33147365805 passed both jobs, verify and publish. The four Darwin/Linux amd64/arm64 archives and the
Cosign-signed `checksums.txt` are public, the release is neither draft nor prerelease, and the latest
redirect resolves to `v1.2.15`, which is what the website installer discovers.

**Version line:** the user chose to stay on `1.2.x` rather than take the minor bump SemVer would
suggest for the new `ask_user` capability, keeping the agent-mode fixes as one continuous series.
