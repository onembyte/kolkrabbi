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
