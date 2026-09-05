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

## FR1 agent-mode field report — three defects from one run (2026-08-28)

**Numbering: FR, and why it is not a letter-number.** These were written as A33 by a session that had
not pulled, renumbered to A34 when A33 turned out to be the agentic-mode group, and renumbered again
when A34 was taken too — by commits that are published and cannot be moved.

Twice was enough. `A33`, `A34`, `L13`, `B12` are *plan items*: work someone chose in advance, whose
identifier is allocated by PLAN.md. These are not that. They are defects a person hit while using
kolk, discovered in the order they were hit, and they were colliding because they were borrowing a
namespace that belongs to planning.

`FR` is field reports, numbered per report and per defect within it. Nothing in PLAN.md allocates it,
so a parallel session cannot take it by working on the next plan item. If you are adding a defect
someone reported from real use, it is the next `FR`.

One agent-mode run reported by the user produced all three. The session was killed at 02:51 after it
had been running long enough to look hung. The directory it was building, `~/ecommerce-webapp`,
contained **eighteen directories and zero files** — the run had created and re-created the same
skeleton without ever writing code, which is the first defect below observed directly.

### FR1.1 the doom-loop guard missed alternating cycles

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

### FR1.2 the spinner vanished while work was still running

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

### FR1.3 the transcript was overwritten instead of scrolled

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

### FR1.4 questions the user answers by picking, not by typing

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

### FR1.5 the picker, made reachable

**Scope:** the model is told when to ask, and Esc backs out of the picker. Non-goals: free-text
questions, and Esc anywhere other than the picker.

**The gap FR1.4 left.** The tool, the schema and the picker were built and tested, and none of it
could fire. Nothing in the system prompt mentioned `ask_user`, and the surrounding text pushed the
other way — *"keep using tools until that checkpoint is complete, or state a concrete blocker"*. A
model reading that decides for itself. A capability the model is never told about is one it does not
have, and the unit tests could not see it because every one of them called the tool directly.

`TestTheCodePromptSaysWhenToAsk` is the test that was missing. It also asserts the *bounds*: without
them an invitation to ask turns the session into a questionnaire, so the prompt names what is the
user's (framework, database, which of several designs, work not asked for) and what is not
(permission to continue, confirmation of something readable, anything answered the same way either
way). Chat mode has no tools, so `TestTheChatPromptDoesNotOfferATool` keeps the promise out of it.

**Esc did nothing, anywhere.** There was no `KeyEscape` at all: a lone escape byte returned
incomplete from `decodeEscape` and sat in the buffer until the next key absorbed it. Esc cannot be
told from the start of a sequence by looking at bytes — every sequence begins with it — so the read
boundary is the evidence: terminals emit a sequence in one write and it arrives whole. A lone escape
left at the end of a read is now the Esc key.

The trade is deliberate and recorded: a sequence split across two reads would be read as Esc plus its
tail as text. Against that, Esc was otherwise a key that never did anything at all, and the worst a
spurious one does is close a picker. Four tests hold the line — arrows, Shift+Tab and Delete must not
decode as Esc, an escape mid-chunk is not the key, and an escape inside a bracketed paste is content.

### FR1.6 a concurrent map write in the new subagent events

Found by running `-race` over the tree after merging the A33 group, which `make check` does not do.

`subagentTaskID` mints and memoizes one id per task index. `subagentRunning` beside it was correctly
guarded by `subagentMu`; the memo was not, and it is written from the per-task goroutines — so two
subagents starting together read and write the same map at once. The detector called it on
`TestARealOrchestratedRunEmitsThePairs`. This is not a wrong-number bug: a concurrent map write is a
runtime panic, mid-turn, in exactly the mode being released.

Fixed by taking the same lock. No nesting risk: `noteSubagents` releases before `publishSubagentStarted`
calls this, and the observer was already called outside the lock so a slow renderer cannot stall the
run. `TestSubagentIDsAreMintedSafelyInParallel` starts sixteen tasks on one channel close and checks
both properties the memo exists for — every index gets a distinct id, and an index keeps the id it
was given, or a finish can never be paired with its start and the count never comes back down.

**Standing note:** `make check` does not run the race detector, so a concurrency bug passes every
gate. `go test ./... -race` belongs in the checkpoint routine for anything touching goroutines.

### R1.5 v1.2.16 reachable-picker release

**Gate:** `make check` green before the tag, and `go test ./... -race` clean over the whole tree —
the run that found FR1.6, and the reason it is now part of the routine rather than a thing this
session happened to do.

**Publication:** commit `7f9da2a` on `main`, tag `v1.2.16`. Release workflow run 33189793519 passed
verify and publish. Four archives plus the Cosign-signed `checksums.txt` are public, the release is
neither draft nor prerelease, and the latest redirect resolves to `v1.2.16`.

**What v1.2.15 got wrong.** It published a feature that could not fire. The lesson is narrow and
worth keeping: a tool is reachable only if the prompt invites it, and a test that calls the tool
directly proves nothing about whether the model ever will.

## FR2 three things a person hit while using it (2026-08-28)

### FR2.1 the composer sat at the top and jumped on resize

Reported from a 125×57 window: the composer near the top, fifty empty rows below it, and the box
moving upward whenever the window was resized.

The frame was only as tall as its content. With a full transcript that already came to exactly the
terminal height — the transcript is clipped to `height − chrome` — so the composer sat on the last
row and looked right. With little or no output the frame was four rows, drawn wherever the cursor
happened to be, anchored to nothing. So the composer started near the top of a fresh session, crept
down as output arrived, and on a resize appeared to jump: a terminal adds its new rows *below*.

The frame is now padded above to the terminal's full height, always. One height, one position, from
the first frame on. `TestTheComposerDoesNotMoveAsOutputArrives` walks sixty lines of output past a
twenty-row frame and fails if the composer moves a single row.

Three tests had encoded the old layout by reading `view[0]` — the composer rule is no longer the
first line. They were rewritten to find what they were looking for rather than to assume where it
falls, which is what they meant in the first place.

### FR2.2 `/plogin` opened Claude Code instead of a login

`runConnectorLogin` called `handover(ctx, selected.Connector, nil, "")`. Nil arguments: it ran the
bare `claude`, which is Claude Code itself. Someone asking kolk to sign in to their Max plan got
another agent's entire interface in place of the one they were using, with no indication of how to
get back, and the login they wanted somewhere inside it.

A connector's login is a subcommand: `claude auth login`, `codex login`. Both verified against the
installed CLIs rather than assumed — `claude auth --help` and `codex login --help`.

`gemini` is deliberately absent from the table. Its subcommand could not be verified because the CLI
is not installed here, and an unknown connector falls back to the bare executable: the fallback runs
the program the user named, while a wrong guess fails in a way that sounds like a broken
subscription. `claude setup-token` was also rejected — it issues a long-lived credential kolk would
then be responsible for, and the point of a provider-CLI connector is that the provider keeps it.

**A gate that had to be re-read rather than re-pointed.** `TestPlansLoginUsesHandoverAndPersistsMetadata`
asserted `len(args) != 0` under the message "want no provider credential inputs". The rule it was
protecting is that kolk passes no credential to the provider CLI — not that it passes no arguments.
It now checks the arguments for anything credential- or path-shaped and requires the login subcommand
by name.

### FR2.3 there was no way to uninstall

Asked directly: how do I uninstall? There was no answer. No command, no script, nothing in the
README — an install path with one command and an uninstall path with none.

`kolk uninstall` lists every path it will remove, what each holds and how big it is, then asks once.
`--keep-data` spares the API key, settings and sessions, which is the difference between "I am done
with this" and "I am reinstalling". `--yes` skips the question for scripts.

The directories come from the same resolver the rest of kolk uses, so an install steered by
`KOLK_DATA_DIR` is removed from where it actually is. A directory that does not exist is not listed:
a list of paths that were never kolk's reads as a threat to delete them. The binary goes last, so a
directory that cannot be removed leaves the command that reported it still on disk.

Anything that is not an explicit yes is a no, end of input included —
`TestUninstallTreatsAClosedStdinAsNo` exists because a closed stdin must never delete an API key.

**Three surface gates fired, all correctly.** The verb is nine letters against a six-letter rule, so
it joins `longVerbs` with its reason: uninstall is the one verb people look for while frustrated, and
a short alias nobody guesses is worse than nine letters. The parity gate wanted a `/uninstall` slash
twin; it is in `batchOnly` instead, because removing the running binary and the session's own state
mid-session would delete the sessions file being written to. And the two length gates were checking
the same rule from two lists — they now share `longVerbs`, so they cannot disagree.

**Verified by running it**, not only by unit tests: a throwaway install under `KOLK_*_DIR` with a
fixture key, a config, a cache and a session. Refused with no answer, then accepted with `--yes`, and
every path was gone including the binary, which unlinks itself cleanly on Unix.

### R1.6 v1.2.17 uninstall release

**Gate:** `make check` green before the tag, and `go test ./... -race` clean over the whole tree.

**Publication:** commit `3f61a98` on `main`, tag `v1.2.17`. Release workflow run 33207424596 passed
verify and publish. Four archives plus the Cosign-signed `checksums.txt` are public, the release is
not a draft, and the latest redirect resolves to `v1.2.17`.

**Note on the uninstall path.** `kolk uninstall` covers the case where kolk still runs. It does not
cover a broken or missing binary, where the files are still on disk and nothing can remove them but
`rm`. A `site/uninstall.sh` alongside `site/install.sh` would close that, and is not built yet.

### M1.1 merging two sessions' work, and the one conflict that mattered

Two sessions were committing into this checkout at once. One had five commits local and unpushed —
four TUI/engine fixes and a `v1.2.18` bump; the other had six pushed — the `a34` group teaching the
vendor connector to carry kolk's mode, effort and session across processes. 856 lines against 1592,
overlapping in exactly two files.

**The conflict was in `Agent.streamChat`, and both sides were right.** One had made
`lastPromptTokens` an `atomic.Int64` because the TUI meter reads it mid-turn from another goroutine.
The other had changed the line beside it, because a provider that runs its own tool loop returns a
message with no calls in it, so `len(msg.ToolCalls)` undercounts and the backend's `meta.ToolCalls`
is the only real number. Taking either side alone would have silently dropped the other's fix: a
race, or a tool count that reads zero for every vendor-connector turn. Resolved by keeping both —
the atomic store, then the larger of the two counts.

Every other use of `lastPromptTokens` was already on the atomic API (`compact.go`, `Context()`), so
the resolution needed no follow-on edits. Checked rather than assumed.

**Verification after the merge, not before:** `make check` green, and `go test ./... -race` clean over
the whole tree. A merge that compiles is not a merge that works, and the two halves had never seen
each other.

### R1.7 v1.2.18 merged release

**Gate:** `make check` green and `go test ./... -race` clean *after* the merge, on the tree that
actually shipped — the two halves had never seen each other before this.

**Publication:** commit `c4a87ff` on `main`, tag `v1.2.18`. Release workflow run 33222459642 passed
verify and publish. Four archives plus the Cosign-signed `checksums.txt` are public, the release is
neither draft nor prerelease, and the latest redirect resolves to `v1.2.18`.

**A stale tag caught before it shipped.** A local `v1.2.18` already existed, created by the other
session and never pushed. It pointed at `50081ce` — a commit the rebase had rewritten, so it was
orphaned: not an ancestor of `main`, and missing the entire `a34` connector group. Pushing it would
have published a release built from history nobody could reach, whose contents did not match its own
notes.

Two checks catch this, and both are one command. `git merge-base --is-ancestor <tag> HEAD` says
whether the tag is on the history being shipped. `git ls-remote --tags origin` says whether the
remote has it at all, reading published refs rather than this clone's cached ones — the same check
R1.3 arrived at after misdiagnosing exactly the opposite problem. The tag was deleted and recreated
on the merged HEAD.

### M1.2 main arrived broken, and what the three failures had in common

A pull brought the `a34.3`–`a34.5` group and `main` did not pass its own gates. Three failures, none
of them in the new feature, all three the same shape: **the change was right and something that
still described the old world was not brought along.** That is gate 8 of the checkpoint contract —
the one that looks behind — and it is the gate that goes unrun when a session is moving fast.

**The spec inventory was no longer closed.** `a34.5` added three codex fixtures
(`codex-error.jsonl`, `codex-plain.jsonl`, `codex-tool-use.jsonl`) without adding them to
`TestSpecContractInventoryIsClosed`. Working as designed: the inventory is closed on purpose so a
spec file cannot appear without someone saying it should. Declared, with a note on why codex is
JSONL where claude is NDJSON.

**A test asserted a model that had just been deleted.** `TestSessionRefusesAPlanModelWithNoAdapterYet`
used `o3` as its example of a plan model whose connector has no adapter. `a34.5` removed `o3` and
`gpt-4.1` from the codex rows for being dead on current codex — so the reference stopped resolving as
a plan model at all, fell through to OpenRouter, and the test failed for a reason unrelated to
adapters. The rule it protects is still exactly right: codex still has no adapter, and `run.go` still
has only `case "claude"`. Only the example had died. It now reads the first codex row out of the
catalogue instead of naming one, so the next dead model cannot rot it, and skips with a reason if the
catalogue ever carries none.

**A type assertion on an error.** `session.go:255` did `err.(*providerError)` to recover the cause of
a plan-limit failure. The vendor's failure reaches that function through the run and translate
layers, so the moment either wraps it the assertion stops matching and the limit message silently
loses the cause it exists to report. `errors.As`, which is what the linter was saying.

**None of this is visible without running the gates.** The tree compiled and the feature worked. The
lesson is the cheap one: `make check` before the push, not after the pull.

### R1.8 v1.2.19 codex-connector release

**Gate:** `make check` green — 2523 tests — and `go test ./... -race` clean. Both run *after* the
three repairs in M1.2, because the tree as pulled did not pass either.

**Publication:** commit `f3ffc19` on `main`, tag `v1.2.19`, checked with
`git merge-base --is-ancestor HEAD origin/main` before tagging — the check R1.7 added after a stale
tag pointed at an orphaned commit. Release workflow run 33226897598 passed verify and publish. Four
archives plus the Cosign-signed `checksums.txt` are public, the release is neither draft nor
prerelease, and the latest redirect resolves to `v1.2.19`.

### S1.1 the site catches up with the product (2026-08-28)

The version badge had been tracking releases correctly — Pages builds from `main`, so each release
bump deployed itself. The prose had not.

**Uninstall was absent from the entire site.** Not a stale sentence: no mention anywhere, in either
page. It is the thing someone looks for while already frustrated, and they will not go hunting, so it
is now the fourth step in the install terminal, next to the three that got them there.
`--keep-data` is named beside it, because "I am reinstalling" and "I am done with this" are different
intentions and only one of them should cost you your API key.

**Five focused tools was four words and one wrong number.** `ask_user` made it six. The count matters
enough to state because every tool's schema is sent on every request of every turn — the same reason
`TestDoctorReportsWhatSchemasCost` asserts it literally.

The doom-loop card said three identical calls. It now also says alternating calls count, which is the
FR1.1 fix: remove, list, remove, list is a cycle even though no call ever follows itself.

Two new cards — the question picker and uninstall — and both, plus the corrected count, are ratcheted
into `scripts/test-site.sh`. 96 checks to 117. The picker is pinned for the reason FR1.5 earned: a
capability nobody is told about is one the product does not have.

**Audited rather than assumed.** `kolk help` was diffed against both pages, the method that found the
missing self-update entry earlier. Nothing else substantive was missing: `model`, `mode`, `config`
and the plan commands are covered in prose by cards that do not spell the verb, and `help`,
`version` and `completion` do not earn cards.

**Verified live**, not just in the repo: the deploy landed in about twenty seconds, and
`kolk uninstall`, `Six focused tools`, `Leaving is one command too` and `A question you answer by
picking` all came back from the public hostname.

One thing left alone: `index.html` links `/capabilities.html`, which Cloudflare 308s to
`/capabilities`. It works, browsers follow it, and changing the link is a cosmetic edit to a
published page for no reader-visible gain.

## FR3 four things asked for while using it (2026-08-29)

Investigated with a four-way parallel read of the code, each investigation then checked by an
adversarial reader. Every one of the four came back `needs-work`, and the critiques were worth more
than the investigations: one falsified a proposed pty call by **running it** (`TIOCSWINSZ` on the
master before the slave is opened returns `ENOTTY` on darwin), one found the seam the investigation
said it could not promise already existed three times over in the same file, and one found the test
file the investigation proposed creating.

### FR3.1 the Ollama plan, and what it is not

`/plans ollama` said "no plans match", because there was no Ollama row. Added one.

The distinction that matters and was nearly lost: kolk **already** supports Ollama, as a local
runtime — `kolk localia`, `internal/local/*`, models running on this machine. What was asked for is
ollama.com's paid tier, which is a different thing reached a different way. Both are true at once,
and the provider wall now says so on one tile rather than growing a second Ollama.

`Sandbox: false`, alone among the provider-CLI rows. Sandbox means the vendor's CLI enforces its own
tool-execution jail — that is what `claude --permission-mode` and `codex --sandbox` are. `ollama run
--help` has no such flag, because ollama runs inference, not an agent. Claiming `yes` would have
printed a jail that does not exist into the SANDBOX column and persisted it into the connector
manifest.

`"ollama": {"signin"}` joins the login table, verified against the installed CLI (`ollama signin
--help` → "Sign in to ollama.com"). No plan-model row was added: `planBackendFor` has only
`case "claude"`, so a row claiming `Access: "provider CLI"` would be a trap that fails at the user's
first turn rather than at login.

### FR3.2 searching plans the way people describe them

"make plans search better, like when searching models."

Both filters were substring matches, but per-field: each field was tested against the *whole* filter.
So the words had to land inside one field, in the order that field prints. `kolk plans claude max`
worked only because "claude max" is verbatim the plan's name; `max claude` and `anthropic max` — a
provider and a tier, which is how anyone actually describes a plan — found nothing.

Now every word must appear somewhere in the row, in any order. For a single-word filter this is
byte-for-byte the old behaviour, because a word cannot span the space the fields are joined by, so
every existing assertion still holds. The model and settings pickers route through the same matcher,
because four pickers disagreeing about what a search means is four things to learn.

**A bug found on the way.** `tuiPlans` fed all fifteen catalogue rows into the `/plogin` picker,
including the six signed into with an API key — which `runPlanLogin` refuses outright. Those were
menu entries whose only possible outcome was an error message. The picker now offers only what the
command can act on. A `PlanSpec` with no stated `Auth` is still offered: it is a presentation struct,
and a caller filling only the display fields should still get a menu.

**The site gate caught the rest.** Adding a provider to `plans.go` fails `make site` until that
provider has a tile on the wall — which is how the duplicate Ollama tile was found immediately, and
why the existing live tile was amended instead.

### R1.9 v1.2.20 Ollama release

**Gate:** `make check` green — 2530 tests — and `go test ./... -race` clean, both run after pulling
the E1–E4 host-Ollama group rather than before.

**Publication:** commit `f7a3253` on `main`, tag `v1.2.20`, checked with
`git merge-base --is-ancestor HEAD origin/main` before tagging. Release workflow run 33277647165
passed verify and publish. Four archives plus the Cosign-signed `checksums.txt` are public, the
release is neither draft nor prerelease, and the latest redirect resolves to `v1.2.20`.

**Two sessions converged on Ollama from opposite ends and did not collide:** this one added the
plan-catalogue row and its login subcommand, while the other built host discovery, the
model-to-backend router and the host-model decoder. The rebase was clean and the merged tree passed
on the first run, which is what the E-group's separate `internal/local` files bought.
## S10 codex spawn backend — verified by a session that did not write it (2026-08-29)

**Gate:** `make check` exit 0 — **2,529 tests, 0 lint issues**, cold start 3.8 ms p50, site 155 /
surface 15 / installer 72 / spec 29 / release 24 / workflow 41 / verifier 30 / smoke 18 / plan 98.
Before it: `go build ./...` clean, and `go test -count=1 -race` green on
`internal/provider/agentcli`, `internal/cli`, `internal/provider`, `internal/shell`.

**Why this needed a second session at all.** The session that wrote S10 was driven by
`glm-5.3-flash:cloud` over the Ollama integration and was killed by its vendor mid-step —
`429 · you have reached your session usage limit` — about five minutes after `codex.go` landed. It
had written 3,701 lines across the §11 S-group and recorded **none of it** in `CHECKPOINTS.md`, so
the work existed only in a chat transcript that had already been summarised past its own context
twice. Nothing was broken; nothing was proven either. Reproducing the commands:

```console
$ go -C . build ./...
$ go test -count=1 -race ./internal/provider/agentcli/ ./internal/cli/ ./internal/provider/ ./internal/shell/
$ make check
```

**Two defects, and the difference between them is the lesson.** `make check` failed the first run on
exactly two lint issues, both in the unexercised code.

`session.go` pulled the vendor's cause out of a plan-limit failure with `err.(*providerError)`.
`Collect` returns that type bare, so the assertion worked — today. One `fmt.Errorf("…: %w")`
anywhere on that path drops the cause and leaves `claude plan limit reached: the seven-day window is
fully used` with no reason attached: a message that still reads as complete. The red test wraps a
`providerError` and asserts the cause survives; it fails on the assertion and passes on `errors.As`.
This was a latent bug.

`codex.go` returned `nil, nil` after a failed `json.Unmarshal`, which `nilerr` flags and which is
**correct** — a line that is not JSON is a version-manager shim announcing itself before the first
frame, real output on a shimmed machine, documented in `spec/testdata/foreign/README.md`. It was
written so that nothing in the code said so. Fixed with the `//nolint:nilerr` and reason that
`cmd_sessions.go` and `cmd_uninstall.go` already carry for the same deliberate skip, rather than a
second spelling of the same idea.

One was a bug; the other was correct code that could not be told apart from a bug. A gate cannot
distinguish those, which is why both stop it, and why "it compiles and it is wired" was never
evidence that this worked.

**Left open, deliberately, as S10.1b.** `scripts/capture-foreign.sh`, `internal/mockagent`'s four
fake binaries and the L0 integration test, `spec/testdata/foreign/synthetic/`, and the six
priority-1 `claude-*` fixtures. §10.3 prices priority 1 at **$0** with four of six already observed
live, so this is cheap and was passed over rather than refused — §11 sends an implementer to S3
first precisely because its fixtures were already committed.

## H0 a shared fuzzy matcher — and a scoring term that turned out to be free (2026-08-30)

**Gate:** `make check` exit 0 — **2,614 tests, 0 lint issues**, `-race` green on `internal/tui`.

The owner asked for every TUI option surface, not only `/model`, to filter live while typing and
feel like Claude Code's and Codex's own pickers. Two concrete gaps: `/model`'s full-screen overlay
has arrow-key navigation but no text filter once open, and `/config` bare has no overlay at all —
it falls through to the static CLI dump. `internal/tui/fuzzy.go`'s `fuzzyScore`/`fuzzyMatches` is
the first leaf: the one matching primitive every picker will route through, replacing
`matchesFilter`'s whole-substring-per-token matching with case-insensitive **subsequence**
matching — "cld" now finds "claude" the way "claude" itself already did — while preserving
`matchesFilter`'s own reason to exist: "claude max" and "max claude" both still find the row, since
each whitespace token is matched independently and every token must be found, in any order.

**Two design decisions were made explicit before writing code, specifically so a later reader would
not have to reconstruct why the simpler path was chosen.** Matched-character highlighting (bolding
the letters that matched, the way fzf and Claude Code's own model picker do) is deferred rather than
shipped alongside this: `writeStyled`/`viewRow` in `internal/tui/model.go` support exactly one style
per whole rendered row today, and giving them per-character spans touches code every screen region
in the TUI depends on — the same class of change that produced U0.4g's raw-terminal row-displacement
bug. And in the `/model` overlay (a later leaf, H3), left/right keeps cycling effort exactly as it
does today rather than being freed for filter-text cursor movement, so the query box only ever
appends and backspaces — zero regression to an already-shipped keybinding.

**A three-term score collapsed to two, and mutation is why.** The first draft scored a
word-boundary bonus (does the match start where a person's eye would look for it), a contiguity
bonus (do consecutive characters land next to each other), and a gap penalty (how far apart do they
land when they don't). Mutating the boundary bonus to zero failed
`TestFuzzyScoreRanksAWordBoundaryStartAboveAMidWordOne`. Mutating the gap penalty to a no-op failed
`TestFuzzyScoreRanksATighterRunAboveAScatteredOne`. Mutating the contiguity bonus to zero **failed
nothing** — because a fully contiguous run already pays zero gap-penalty, which already outscores a
scattered run's negative one. The bonus was reproducing an effect the penalty already produced for
free. Writing a test to justify keeping it would have been proving code was needed by asserting
that it was; deleting it was the honest move, and the two-term version passes the same seven tests
with nothing left unproven.

**The session's own red-first discipline slipped, and was caught before green.** The implementation
was written before its test — `fuzzy.go` existed when `fuzzy_test.go` was still being drafted. Caught
on review of the very discipline this session has been enforcing on every other leaf: the
implementation file was deleted, the test written first, `go test -run TestFuzzy` observed failing
to compile against undefined symbols, and only then was the implementation restored. Worth recording
plainly rather than quietly fixing, since the discipline only means something if slipping on it gets
named.

**Mutation verification used `go test`, not hand arithmetic**, after a hand-calculated prediction
disagreed with what a first mutation attempt actually did — the `sed` pattern's spacing didn't match
gofmt's actual output, so no mutation was applied at all, and a silently-passing suite looked
identical to a validly-caught one. Every mutation below was confirmed by `diff` against the original
before trusting its test result, and reverted to a byte-identical file after.

## H9 post-H8 hardening (2026-08-30)

**Gate:** `make check` exit 0 — **2,802 tests, 0 lint issues**, platform compilation and all
release/workflow/verifier/smoke/plan gates green; final package `-race` tests on session, shell,
provider, provider CLI, engine, TUI, and CLI green.

The first full gate after H8 exposed two real gaps. `SubscriptionModelShortcut` had become a
test-only export after every production caller moved to the plan-aware helper, and a cancelled
`LinesProcess` could kill a provider immediately from its reader while `exec.CommandContext` was
starting the provider's SIGINT → SIGTERM → SIGKILL ladder. Under scheduling pressure that skipped
SIGINT, violating the provider's graceful cancellation contract. Session path entry points now
validate IDs before joining paths and reject decoded IDs that do not match their filenames. A
cancelled reader waits for the ladder to reap the child; unrelated reader failures still kill and
reap immediately. The obsolete shortcut was removed, and the formerly empty `workflow-pin-check`
Make target now runs its existing 43-check script.

The two Codex errors are now distinguished. The current bounded `ReadSlice` provider reader has no
`bufio.Scanner` ceiling, and exact first-turn and resume invocations with `gpt-5.6-sol` succeeded
through the installed signed-in Codex CLI. `Reading prompt from stdin...` is stderr from a Codex
process that exited nonzero; it is not the current reader's large-frame error.

**Red/green:** the full gate first caught the dead export and the interrupt race; the signal test
then passed 20 normal repetitions and 10 race repetitions after the wait-for-ladder change. Session
security tests, all affected package race tests, the 43 workflow-pin checks, and the final end-to-end
`make check` are green. No commit or release tag was created here.

## H8 Codex output and subscription model selection (2026-08-30)

**Gate:** `make check` exit 0 — **2,780 tests, 0 lint issues**, platform builds and all release
workflow/verifier/smoke/plan gates green; focused `-race` tests on shell, provider, provider CLI,
TUI, and CLI green.

The first live use of the newly routed Codex plan model failed before any event reached the
translator: `bufio.Scanner` rejected a provider JSONL line above its arbitrary 1 MiB buffer and
surfaced the opaque `bufio.Scanner: token too long`. Because the same failure appeared beside
sign-in guidance, it looked like authentication had failed even though the connector was enabled.

Both one-shot and persistent provider readers now use one bounded `bufio.Reader.ReadSlice`
accumulator. It accepts lines through 16 MiB, enough for the large tool-result frames these CLIs
emit, and gives a named bounded-output error above that limit. The red reproduction also revealed
that the old failure path killed only the shell leader: a pipeline child retained stdout and made
the wait stall. Reader failures now kill and reap the whole provider process group.

Model selection had two independent gaps. Bare `/model` in the plain REPL depended on the API
catalog and gave no plan choices when that request failed, while the TUI picker emitted an effort
suffix the slash dispatcher treated as part of the model id. Bare `/model` and singular `kolk model`
now show subscription rows with exact ids, `claude-pro`/`claude-max` and `gpt-plus`/`gpt-pro`
shortcuts, and the exact sign-in command when needed. `/model <id|alias> <effort>` applies the model,
session effort, and provider backend together. Help, shell completions, welcome text, and TUI rows
use the same vocabulary; ordinary aliases such as `flash` retain their existing API route.

The race gate found one adjacent ownership defect: raw PTY output and a pending renderer frame
could write the same output writer concurrently. Runtime now shares one synchronized writer between
the renderer and attached child, preserving escape-sequence boundaries and making embedded
non-thread-safe writers safe.

**Red/green:** the large-line regression failed under the old Scanner and then passed at 12 MiB;
the over-limit regression rejects 16 MiB+1 with a clear bounded error; alias and picker-effort
tests failed before their routing changes and pass now; the existing `flash` alias regression
caught and forced the plan-alias narrowing; the CLI race test caught and forced the synchronized
writer. No release tag or commit was created here.

## H7 five ways to own the terminal, and only some of them knew about each other (2026-08-30)

**Gate:** `make check` exit 0 — **2,700 tests, 0 lint issues**, `-race` green on `internal/tui`,
release/plan/spec guards all passing.

Found while verifying a merge, not by anything failing on its own. `main` had diverged from
`origin/main` for the entire duration of this group: H0–H6 on one side, a PTY-based provider-login
feature (`RunAttached`) and new agentic/subagent orchestration on the other, both touching
`controller.go` and `runtime.go`. `git merge-tree` reported it clean — no conflict markers, the two
sides never edited the same lines — but a clean text merge is not a proof of correctness, and
reviewing the merged `runtime.go` by hand instead of trusting the green diff is what surfaced this.

Five separate places can claim exclusive ownership of the terminal's input: `Question`, `Approval`,
the `/model` picker, the `/config` picker, and now `RunAttached`'s raw PTY forwarding. Each guarded
only against the ones that already existed when it was written:

- `Ask`/`Decide` predate `modelPick`/`configPick` (H3) and were never revisited when those landed.
- `AskModel`/`AskConfig` correctly check all four overlay fields — written last, with the others
  already in front of them — but neither checks `attached`, which did not exist yet.
- `RunAttached`, new on the other branch, only refuses a *second* attach; none of the four overlay
  fields existed from that branch's perspective either.

The failure mode is a hang, not a crash. While `attached` is set, the read loop forwards raw bytes
straight to the child and never calls `HandleKey`. While a picker is open, `HandleKey` checks it
ahead of `question`/`approval`. Either way, an overlay opened on top of another exclusive state gets
no keys, ever, and blocks on its reply channel until its caller's context is cancelled — which, for
a subagent's own turn, may be a long time. The newly-merged concurrent-subagent orchestration makes
two of these five colliding a real scenario now, not a theoretical one.

The fix is the same change at all five entry points: each now refuses when *any* of the other four
are active, not just the ones that predate it. `RunAttached` keeps its own `ErrAlreadyAttached`
return rather than adopting `AskModel`'s bare bool, since that is the contract its existing callers
already depend on. Each of the six new tests runs its blocking call on its own goroutine with a
bounded timeout specifically so a still-buggy guard times out that one test instead of the whole
suite — and the failure observed really was the hang the bug describes, not a compile error. Five
targeted mutations — one per newly-added clause across `Decide`, `RunAttached`, and `AskModel` —
each caught by exactly the test written for it, all reverted byte-identical.

## H6 both filter boxes silently dropped a paste (2026-08-30)

**Gate:** `make check` exit 0 — **2,643 tests, 0 lint issues**, `-race` green on `internal/tui`.

The composer's own `Editor.Update` has always treated `KeyText` and `KeyPaste` as the same act of
adding text (`case KeyText, KeyPaste: return EditResult{Changed: e.insert(...)}`). H3's and H4's
filter-box key handlers wired only `KeyText`. A model ID or setting key pasted into either overlay's
search box did nothing at all — the key fell through the switch to the default no-op branch.

Red: `TestModelPickerFiltersOnPasteTheSameAsTyping` and its config-picker twin, both sending
`Key{Kind: KeyPaste, Text: "..."}` and asserting the same filtered result typing the same text would
give. Both failed with the filter still empty. Green: `case KeyText, KeyPaste:` in both overlays'
switches, matching the composer's own contract exactly. Mutation on both: removing `KeyPaste` again
fails only the paste test and leaves the typing test green, confirming each test proves what it
claims to and nothing more.

## H5 infrastructure that named its own reason for existing, then went unused (2026-08-30)

**Gate:** `make check` exit 0 — **2,641 tests, 0 lint issues**, `-race` green on `internal/tui`.

Found by re-reading H2's own commit, not by anything failing: `scrollWindow` was justified in the
same sentence that named "a future /model filter box and a future /config picker" as the reason it
existed. Both shipped in H3 and H4 without ever calling it. `filteredModelIndices`/
`filteredConfigIndices` narrow and rank a catalog on every keystroke, but the line-builders still
rendered every surviving row — a settings list or model catalog longer than the terminal would
overflow it the moment either overlay opened with no filter typed yet, exactly the
worse-than-arrow-keys failure this whole group exists to fix.

Nothing tested this because nothing was asked to: H3's and H4's own tests all used two- or
three-row lists, so the gap was invisible to every test either leaf actually wrote. The same shape
as H0's dead scoring term and H3's marker-reset bug — a real gap survives not because it is hard to
catch, but because coverage never looked at the case that would reveal it.

The fix mirrors the suggestion dropdown exactly: `modelTop`/`configTop` fields track the first
visible row via `scrollWindow` on every key that moves the marker or changes the filtered set, and
the line-builders slice to `c.windowSize()` rows with the same `"  ↑"`/`"  ↓"` indicators — the same
fixed window size the suggestion dropdown already uses, not one derived from the terminal's actual
height. `Question` is untouched on purpose: a fixed, small, model-proposed menu, never a searchable
catalog, and never in this leaf's scope.

One red test was red for a mistaken reason before it was red for the right one: the first version
undercounted how many `KeyDown`s it takes to walk from row 0 to the last of fifteen rows — caught by
reading the failure output before trusting it as a mutation-proof.

## H4 the literal ask, and the one place its answer had to differ from /model's (2026-08-30)

**Gate:** `make check` exit 0 — **2,639 tests, 0 lint issues**, `-race` green on `internal/tui` and
`internal/cli`.

Bare `/config` fell straight through to the non-interactive dump `kolk config` itself prints.
`ConfigPicker` gives it the overlay `/model` already has: `filteredConfigIndices` filters and ranks
`SettingSpec` rows through H0's `fuzzyScoreFields`, the twin of H3's `filteredModelIndices`, and the
key handling — type to filter, Backspace to edit, Escape clears-then-closes — is deliberately
identical in shape, so learning one picker in this app does not mean learning a second rulebook for
the other. `tuiSettings(a)`, which already built this exact row shape for the inline `/config`
suggestions, feeds the picker too — no new CLI-side data plumbing needed.

**The one place it could not just copy /model: what Enter does.** `/model`'s Enter answers with a
complete command; `/config`'s cannot, because a setting still needs its value typed and no picker
should guess it. `resolveConfigPicker` calls the identical two lines `completeSuggestion` already
uses for the inline dropdown's Tab-completion — `c.editor.setDraft(...)`, `c.screen.SetDraft(...)`
— rather than inventing a second way to say "a setting was chosen." Wiring the CLI side is one
`else if` beside the existing `/model` check, because `AskConfig` mirrors `AskModel`'s blocking
contract exactly, including the mutual-exclusion guard now extended in both directions.

**A second red-first slip, corrected the same way as the first one this session made.** The CLI
wiring was written and working before its two tests existed, so they passed on arrival — never
observed failing for their own reason. Caught by the same self-check applied to every other leaf:
mutating away each test's exact claim (force the bare-`/config` guard to always refuse; loosen the
args-guard to a prefix check) confirmed both fail for precisely the reason they exist, before
either was trusted.

**A genuine data race, not a flake.** `internal/tui`'s own picker tests read controller state
directly under its lock because they live inside the `tui` package; a test in `internal/cli` cannot
reach that private field, and polling it without the lock raced `AskConfig` mutating the same state
from the picker's own goroutine — caught by `-race`, not by inspection. Fixed with
`Runtime.ConfigPicker()`, a thin locked passthrough mirroring the existing `Snapshot()`/`SetStatus()`
pattern, rather than working around the race inside the test. Ten repeated `-race` runs after the
fix, all clean.

## H3 a row's identity and its screen position stop being the same variable (2026-08-30)

**Gate:** `make check` exit 0 — **2,632 tests, 0 lint issues**, `-race` green on `internal/tui`.

The `/model` overlay could always be arrowed through; it could not be typed into. `c.modelIndex`
now indexes a filtered, ranked view of `c.modelPicker` — recomputed by `filteredModelIndices()`
from H0's `fuzzyScoreFields` on every keystroke — instead of the catalog itself, and typing or
Backspace narrows or widens that view live.

**The bug worth naming: `KeyLeft`/`KeyRight` mutated `c.modelPicker[c.modelIndex]` directly**, which
was only ever safe because the displayed order and the catalog order were the same order. Once a
filter can show a different row at index 0 than the catalog's own row 0, that line turns the
*wrong model's* effort — silently, since both reads succeed and nothing panics unless the row it
happens to hit has no `Efforts` at all. The fix keeps `modelPicker` as the one source of truth and
reaches it only through `c.modelPicker[indices[c.modelIndex]]`. Mutation: revert to the direct
index, and turning the dial on a single filtered claude row silently turns "vendor/mock"'s absent
dial instead — the claude row's effort never moves, caught by exactly one test and none of the
pre-existing ones, which is the point: the bug is specific to filtering, not a general regression.

**A second bug in the same family, caught only because a test went looking for it.** Move the
marker down an unfiltered list, then type a filter that narrows the list below the marker's raw
position, and `indices[c.modelIndex]` reads past the end of a list that just got shorter. The reset
line that prevents this already existed when the leaf was written; what didn't exist was a test
requiring it, and mutating it away passed the whole suite silently. Same lesson H0 already taught
with its dead scoring term: a mutation surviving is a claim about missing coverage, worth chasing
down rather than shrugging off because the rest of the suite stayed green.

Escape backs out one step — clears an active filter first, closes the overlay only once there is
nothing left to clear — matching fzf and the rest of this group's own stated aim, implemented
exactly as the owner scoped it before any of H's code existed rather than re-litigated here.

## H2 two small pieces, not a shared skeleton — the plan changed on contact (2026-08-30)

**Gate:** `make check` exit 0 — **2,627 tests, 0 lint issues**, `-race` green on `internal/tui`.

The scoped plan for this leaf was an embedded `*Editor` as a filterable overlay's query line,
reusing its existing rune buffer. Wrong, on contact with the keys `Editor` claims: `Up`/`Down` do
history navigation and vertical cursor movement there, and `Left`/`Right` move a text cursor — but
a filterable overlay's `Up`/`Down` mean "select the next row", and `/model`'s `Left`/`Right`
already mean "cycle this row's effort," by the owner's own decision earlier in this group. There is
no cursor position left for `Editor` to move and no history for it to navigate. Wrapping it and
disabling most of what makes it `Editor` would not be reuse of a rune buffer; it would be carrying
a multiline, history-aware input type into a role obligated to refuse nearly everything it does.
`filterBox` is fifteen lines — append text, remove the last rune, report whether removing one did
anything — because that is the actual size of a query line that only ever appends and backspaces.

**No shared overlay skeleton either, for the same reason `ModelPick` and `Question` already don't
share one.** This codebase has no generic container anywhere (checked: no `[T any]` under
`internal/`), and the two existing pickers are already separate concrete types despite looking
alike — their own row shape, their own key handler, their own line-builder. What a future `/model`
filter box and a future `/config` picker actually share is not the row list, which differs by
picker, but two pieces that don't: the query buffer, and the "scroll the least amount that keeps
the selection visible" arithmetic every windowed list needs regardless of what it lists.

**The second piece already had a body; it just didn't have a name yet.** The suggestion dropdown's
`showSelectedSuggestion` had this exact rule inlined. Writing a second, textually identical copy for
the next overlay instead of naming the first one and calling it twice is the specific duplication
the refactor gate exists to catch. `scrollWindow(selected, top, window)` is that rule as a pure
function of three integers; `showSelectedSuggestion` now calls it. The regression net is the
existing thirty-row scroll test, unchanged and still green, since the arithmetic did not move —
only where it lives did.

## H1 a field join for fuzzy search reopened the bug the join was meant to stop (2026-08-30)

**Gate:** `make check` exit 0 — **2,623 tests, 0 lint issues**, `-race` green on `internal/tui`.

Every list-filtering surface in the TUI — slash commands, `/model`, `/plogin`, `/config`,
`@`-mentions — now routes through H0's `fuzzyScore`, ranked by score instead of catalog order.
`matchesFilter` is deleted outright: all three of its call sites moved, so there was nothing left
to leave behind for someone to forget was dead.

**Nine of ten new tests were the expected shape.** A query whose letters are a scattered
subsequence rather than one contiguous run now finds the row a literal-substring match could not —
`/cfg` finds "config", `@mdl` finds "model.go". The tenth went red on an already-passing suite,
which is the more useful kind of red: `/config eff` matched "auto_restart_after_update" as well as
"effort".

**The cause was inherited, not introduced.** `SuggestSettings` has always joined a setting's key,
summary and value with spaces into one haystack before matching — `matchesFilter`'s own design.
That join carries a hidden assumption: a query, once split into whitespace tokens, will never find
a match by threading through the seam between two unrelated fields. Literal substring matching
keeps that assumption true almost by accident, because a real cross-field literal run barely
happens in natural text. Subsequence matching breaks it outright: `auto_restart_after_update` and
`restart into the new version after an update` each carry exactly one `f`. Joined, "eff" is a valid
subsequence — an `e` and an `f` from the key, a second `f` borrowed from the summary three fields
later. Nothing about the setting says "effort"; the letters just lined up once concatenation made
that possible.

**The fix could not just search fields one at a time, because a legitimate feature depends on
crossing them.** `SuggestPlanLogins` needs "anthropic max" to find a plan whose provider is
"anthropic" and whose name is "Claude Max" — one query, two tokens, two different fields, by
design. So the fix is not "never let a query cross a field boundary" but "never let one *token's*
subsequence cross one" — `fuzzyScoreFields` tries every token against every field independently
and keeps whichever field scored that token best, rather than concatenating fields before matching
at all. Cross-token, cross-field distribution survives; cross-field spanning inside a single
token's own subsequence does not. `fuzzyScore` is now `fuzzyScoreFields` called with one field,
removing a second copy of the same token loop rather than keeping two in sync.

**A malformed mutation earned its own line in the record instead of a quiet redo.** Proving the
field-isolation mattered meant reverting `fuzzyScoreFields` to call `fuzzyScore` on the joined
string — except `fuzzyScore` had just been rewritten to delegate *back* to `fuzzyScoreFields`,
turning the "mutation" into infinite mutual recursion and a stack-overflowing test run rather than
a result. Not a finding; a broken experiment, discarded and redone by inlining the join directly
so the mutation could actually run to a verdict.

**Seven mutations, one per call site plus the fix itself, all reverted to byte-identical diffs.**
Each isolated to its own surface: reverting `SuggestModels`' wiring failed only its two tests,
reverting `SuggestCommands`' failed only its one, and so on — confirming the leaf did not
accidentally let one picker's behavior leak into another's just because the code looks similar.

## S10.2 a fixture nobody replayed hid a dead code path (2026-08-29)

**Gate:** `make check` exit 0 — **2,531 tests, 0 lint issues**, cold start 3.2 ms p50.

**The find came from asking a bookkeeping question, not a debugging one.** While correcting the
fixture README (S10.1a) the obvious next question was which tests replay these files. The answer was
none. `claude-plain.ndjson` and `claude-tool-use.ndjson` appear in exactly one place in the tree — a
filename list in `protocol/contract_inventory_test.go` that asserts they exist. Their own README
says they exist so the translator can replay real vendor output *"offline, forever"*. The codex half
of the same package does it properly, replaying all three of its fixtures in six places.

```console
$ grep -rn "claude-plain\|claude-tool-use" internal/ protocol/
protocol/contract_inventory_test.go:22:  "testdata/foreign/claude-plain.ndjson"
protocol/contract_inventory_test.go:23:  "testdata/foreign/claude-tool-use.ndjson"
```

**The bug that hid behind it.** The vendor nests `rate_limit_event` under `rate_limit_info` in
camelCase; `wireFrame` declared the fields flat and snake_case at the top level. `jq` on the
committed capture settles it in one line:

```console
$ jq -c 'select(.type=="rate_limit_event")' spec/testdata/foreign/claude-plain.ndjson
{"type":"rate_limit_event","rate_limit_info":{"status":"allowed_warning","resetsAt":1787731200,
 "rateLimitType":"seven_day","utilization":0.78,…},…}
```

So `frame.Status` was `""` on every real frame, the status check dropped it, and two things that
look implemented were not: the plan warning never reached the user, and `s.rejectedLimit` was never
set — leaving `classifyLimitFailure` unreachable against real vendor output. A user hitting their
Claude plan limit got a bare "credit balance too low" rather than the window and its reset time.
The wrapped-cause fix made under S10 hours earlier was, against a real machine, repairing a path
nothing could enter.

**Five tests asserted the invented shape and passed.** Hand-written flat snake_case JSON across
`translate_test.go` and `session_test.go`, testing this package's assumption against itself. They
have been moved onto the shape the vendor sends. This is A33.1's vacuity lesson from the other
direction: there the fixture was too easy to fail, here the fixture was never opened.

**Mutation, because a green test proves nothing about a test written after the fix.** Disabling the
nested branch with `if false &&` fails three tests — the fixture replay, the rate-limit projection,
and `TestClaudeSessionWarnsThenClassifiesThePlanLimit` — confirming the session-level path is
genuinely covered and not merely adjacent. `translate.go` was diffed byte-identical against a
pre-mutation copy afterwards.

**The flat spelling is kept, and pinned.** No vendor frame has ever carried it, so it is evidence of
nothing — but a rate-limit frame this package cannot read is a plan limit the user never hears
about, and an extra tolerated spelling costs four lines. It has its own test so that it stays a
decision someone can find and delete, rather than a shape that merely still happens to decode.

## S10.1d7 async stdin — the pipe was reporting itself instead of the child (2026-08-29)

**Gate:** `make check` exit 0 — **2,544 tests, 0 lint issues**, `-race` green on `internal/shell`.

Amendment A6 wants an asynchronous stdin write that ignores `EPIPE`, and its own sentence is the
leaf: *"a 200 KB prompt against a child that exits on a bad flag blocks past the 64 KiB pipe buffer
and reports 'broken pipe' as the cause, discarding the real diagnosis."*

A diagnosis bug, not a plumbing one. The child has already said what is wrong — `claude: unknown
flag --nope` — and it waits in stderr for the reader, which S10.1d1 bounded and the exit path
reports. `Send` wrote synchronously and returned first, so the turn failed with a symptom and the
sentence the user needed was never printed. Red:

```
Send reported writing provider request: write |1: broken pipe
```

**The write error is not returned at all, rather than ranked below the exit code.** A6 says it may
never outrank the exit code and stderr. Ranking implies a comparison every call site has to get
right; never producing the error deletes the comparison. The reader owns diagnosis.

**One writer goroutine, not one per Send.** This is a line-delimited protocol, so ordering is part
of the contract and N goroutines racing on a pipe do not preserve it. A buffered channel with a
single consumer keeps sends FIFO while letting `Send` return before the child has read anything.

**A discarded mutation, recorded because throwing it out was the point.** The first attempt at
restoring synchronous behaviour left a `select` whose only remaining case was `<-p.exited`, so
`Send` blocked forever against `cat` and the run was killed at the timeout. A hang is not evidence
about the code — it is a broken experiment, and counting it as "the mutation was caught" would have
been self-deception dressed as rigour. Re-run with the exact pre-fix write, it fails the new test
with the original message.

## S10.1d6 the drain was reading the receipt and throwing it away (2026-08-29)

**Gate:** `make check` exit 0 — **2,543 tests, 0 lint issues**, `-race` green.

§2.5 starts the cancel ladder at SIGINT for one stated reason: the vendor still produces a `result`
frame, so a cancelled turn is accounted rather than a hole in the dashboard. Three leaves went into
making that frame exist and arrive. `abandonTurn` was already draining it, looking only for the
completion event to resynchronise the stream, and discarding everything else — so the benefit the
ladder was built for was never actually collected.

**The second defect is worse and has nothing to do with cancellation.** `chargeTurn` converts the
vendor's *running session totals* into a per-turn delta by rebasing `s.spent*` on every report. An
abandoned turn never charged, so those totals stayed stale and the next turn's delta silently
absorbed the abandoned turn's usage. Cancel a turn, retry immediately, and the cancelled turn is
billed twice — once invisibly, once inside the retry. The test pins both halves:

```
next turn cost = 0.8, want only its own share (0.3)
```

That is the mutation output from removing `chargeTurn` while keeping the reporting fix — the
plausible half-fix, since reporting the cancelled turn and *charging* it are separable and only the
first is visible in a returned meta.

**B12.11 is amended rather than left standing.** Its entry read "a session turn records its own cost
and tokens, not the provider's running totals" without qualification. That was true only of turns
that completed. Gate 8's job is to notice when a leaf makes an older claim finally true, or reveals
it was never quite true, and this was the second.

**Not recovered, deliberately: the partial message.** The drain collects accounting only. The turn
failed and still says why; returning half an answer as if it were an answer is §1.2's `Truncated`
and A4's flattening rule, and is not decided here. `Collect`'s own error on this path is discarded
for the same reason — the turn already failed, `cause` is the diagnosis, and a parse error from a
stream interrupted mid-flight would replace it with a symptom.

## S10.1d5 (part) saying a conversation was retired, and knowing when not to (2026-08-29)

**Gate:** `make check` exit 0 — **2,542 tests, 0 lint issues**, `-race` green.

S10.1d4 made the retirement safe and left it silent. The vendor has lost its own record of the
session and nothing said so.

**The wording is the decision.** §2.5 names this warning `WarnHistoryLost`, and taken literally that
is the wrong thing to tell the user: nothing of theirs was lost. `promptFromMessages` replays the
entire conversation every turn, so kolk's transcript is intact and the vendor's copy is the only
casualty. The line reports what changed — the next turn starts a fresh conversation, your transcript
is intact — instead of announcing data loss that did not occur. A warning that overstates itself is
one people learn to skip, including on the day it matters.

**Silence is a feature with its own test.** §2.5 marks a user cancellation `Silent:true`. Someone who
just pressed Ctrl-C knows why the provider stopped, and the notice would appear on *every*
cancellation, bolted onto the thing they deliberately did. "Does not appear" is invisible in a test
that only asserts presence, so it gets its own — and the mutation (announcing unconditionally) fails
it with the whole unwanted line printed in the failure.

**Routed through `onToken`, not `watch`.** `watch` sets `streamed`, which decides whether a turn may
be retried, and a notice is not half an answer. Keeping the trail off that flag preserves what the
retry logic needs `streamed` to mean.

**The typed code is deliberately absent.** `provider.Meta` has no warnings field — §2.6's amendment
A2 is unbuilt, and A7's three `Warn*` codes with it. Minting a private warning vocabulary inside
`agentcli` would create exactly the second vocabulary those amendments exist to prevent. Prose
through the existing trail channel is the honest interim, recorded as `[~]` rather than ticked.

## S10.1d4 a hard exit retires the conversation — and the bug mutation found in the fix (2026-08-29)

**Gate:** `make check` exit 0 — **2,540 tests, 0 lint issues**, `-race` green on `agentcli` and
`shell`.

§2.5's starred rule: a SIGTERM/SIGKILL exit invalidates the vendor conversation, because the vendor
**continues an unfinished turn** on `--resume`. So resuming after a hard exit lets it silently
execute the tool calls kolk already told the user were cancelled — editing files after a "cancelled"
turn. S10.1d3 gave the vendor its chance to finish; nothing acted when it did not take it. Red was
the second turn spawning with `--resume <same handle>`, printed in the failure.

**Retiring the handle costs nothing.** `promptFromMessages` serialises the whole conversation every
turn, so kolk replays its own transcript regardless of what the vendor remembers. That is what made
it right to ship the correctness half alone and split §2.5's `<prior-conversation>` label and
`WarnHistoryLost` out as S10.1d5 — a notification gap should not hold up the fix that stops files
being edited after a cancellation.

**The predicate was wrong, and only mutation testing says so.** `exitedHard` first excluded SIGINT,
reasoning from §2.5's "SIGINT ends the turn gracefully and still produces a result frame":

```go
return status.Signal() != syscall.SIGINT   // the wrong line
```

Deleting the exclusion **failed no test**. The branch was unreachable: the test meant to cover it
used a child that exits *cleanly* after SIGINT, whose wait status is not signalled, so the
comparison never ran.

Reading it again with that fact in hand showed the line was not just dead but wrong. **A process
that handles a signal is never signalled** — it exits with a code of its own choosing. A signalled
status means no handler ran, so there is no result frame and the turn is unfinished, and that is as
true of SIGINT as of SIGTERM. The exclusion would have called a SIGINT-killed vendor resumable,
which is precisely the failure the rule exists to prevent. `mockagent.KilledByInterrupt` — no traps
at all, so SIGINT's default action kills it — now covers the branch, and reinstating the exclusion
fails a test where before it failed none.

The lesson is not "write more tests". It is that a mutation surviving is a *claim about coverage*
worth chasing to its source, because the uncovered line is disproportionately likely to be the
wrong one. Two of the four leaves in this loop have now had a defect found this way.

## S10.1d3 the cancel ladder, and the fake vendors that make it provable (2026-08-29)

**Gate:** `make check` exit 0 — **2,536 tests, 0 lint issues**, `-race` green on `internal/shell`.

Cancellation went straight to SIGKILL; §2.5 says start at SIGINT. The reason is not politeness. The
vendor documents that SIGINT ends the turn gracefully and **still produces a `result` frame**, which
carries the turn's accounting. And the starred rule is heavier: a SIGTERM/SIGKILL exit *invalidates
the vendor session*, because the vendor resumes an **unfinished** turn on `--resume`. Killing first
risks the vendor later executing tool calls kolk already reported as cancelled, editing files after
a "cancelled" turn. This was a correctness bug dressed as a manners bug.

**`internal/mockagent` finally earned its place.** Two leaves ago it was deliberately not built,
because `sh -c` and `cat` were reaching the defects without it. This leaf is where that stopped
being true: no stock POSIX tool ignores SIGINT on request, so escalation cannot be observed without
a child written to ignore it. Two fakes — one exits on SIGINT, one traps it and leaves on SIGTERM —
each appending received signals to a log. **The log is the evidence**, because an exit status cannot
tell the rungs apart once a child chooses its own exit code. Only the two kinds this leaf needed
exist; the write-end holder arrives with the drain that needs it.

**The race detector found a bug in this leaf's own design.** The graces are variables so a test can
walk three rungs in 0.17 s instead of seven seconds, and the ladder goroutine read them while a test
adjusted them:

```
WARNING: DATA RACE
  exec_unix.go:82  (ladder goroutine reading sigintGrace)
  ladder_test.go:22 (test writing it)
```

Fixed by capture rather than by locking: the schedule is read on the goroutine that starts the
child. The graces belong to the child **as configured at spawn**, not to whatever the package holds
when cancellation lands — which is also the more truthful model.

**Mutation:** starting the schedule at SIGTERM fails both tests, reporting `[TERM]` where
`[INT, TERM]` belongs. That is the §2.5 violation verbatim.

**A test that taught POSIX rather than kolk.** `TestLinesProcessCancelKillsTheWholeProcessGroup`
started failing once the ladder existed: its grandchild is `sleep 30 &`, and POSIX makes a
background job in a non-interactive shell **ignore SIGINT**. It really does survive rung 1 and die
on rung 2. The test was right and the ladder was right; the expectation had been written when
cancellation meant an immediate SIGKILL.

**The arch gate rejected the first draft** for comparing `runtime.GOOS`, which
`docs/plan/02-architecture.md` §8 forbids in favour of build-tagged files. Rightly: the split is now
`mockagent_unix.go` and `mockagent_windows.go`, and the Windows one **refuses** rather than faking a
child, because a green signal-ladder test on a platform whose `groupChild` is a documented no-op
would mean nothing.

**Recorded as still open, not ticked — S10.1d4, and it is the dangerous one.** The ladder now gives
the vendor its chance to produce a `result`; nothing yet acts on the case where it did not. Honouring
§2.5's starred rule needs `agentcli` to mint a fresh `--session-id` after a hard exit and replay
kolk's own transcript as a labelled `<prior-conversation>` prelude with `WarnHistoryLost`. Until
that lands, a `--resume` after SIGTERM/SIGKILL can still let the vendor continue the unfinished turn.

## S10.1d2 (part) the long-lived child gets the rule the one-shot already had (2026-08-29)

**Gate:** `make check` exit 0 — **2,534 tests, 0 lint issues**, `-race` green on `internal/shell`,
windows/amd64 cross-build green.

`command()` has put every one-shot in its own process group since the beginning, kept honest by
`TestTimeoutKillsTheWholeProcessGroup`. `StartLinesProcess` — the child that lives for the whole
session rather than one command — had neither `Setpgid` nor a group-aware cancel. The rule matters
*more* here: running the vendor's own tool loop is the premise of this backend, so whatever it
starts is kolk's grandchild, and `exec.CommandContext`'s default cancel signals only the leader.

**The test runtime is the measurement.** Red: 30.01 s, because the orphaned `sleep 30` held the
pipe and the deferred `Close` waited on it. Green: 0.04 s. A cancellation that takes 750× the
cancel is not a cancellation — the same observation the Run-path test already records.

**Mutation:** dropping `Setpgid` while keeping the `Cancel` hook restores the 30 s failure, because
`killGroup`'s negative pid then names no group and falls back to killing the leader alone. That is
the plausible half-fix, and it is caught.

**Windows is worse and says so.** `groupChild` is a no-op there for the reason `command()` already
gives — grouping needs a job object, which A13 owns — so a persistent provider child leaks its
grandchildren. Recorded in `exec_windows.go` beside the existing note.

**Deliberately not closed, and marked `[~]` rather than ticked.** §2.5/P6 wants a bounded 3 s drain,
then a **ladder** — SIGINT first, escalating — then closing the stdout **read end**. Today
cancellation goes straight to SIGKILL. P6 names the hang the rest prevents and this leaf does not:
a background `Bash` grandchild inherits the stdout write end, so an unbounded drain never sees EOF
and hangs the CLI after a complete answer. Testing that honestly needs a child holding the write end
and a child that ignores SIGINT, which is the concrete job `internal/mockagent` exists for — and the
reason it was right not to build it speculatively two leaves ago.

## S10.1d1 the stderr ring — a session-long buffer nobody was emptying (2026-08-29)

**Gate:** `make check` exit 0 — **2,533 tests, 0 lint issues**, cold start 3.2 ms p50, `-race`
green on `internal/shell`.

S2 asks for a stderr **ring**; `StartLinesProcess` had a `bytes.Buffer`. B12.5 made this process
persistent — one child serves every turn of a session — so that buffer was appended to for the whole
life of the session and never drained. A vendor that narrates to stderr grows kolk's memory for as
long as somebody keeps working.

**The visible half was worse than the leak.** On a failed exit the entire buffer was interpolated
into the error string that reaches the terminal and the transcript:

```go
err = fmt.Errorf("provider process exited unsuccessfully: %s: %w", stderr.String(), waitErr)
```

A chatty vendor turned one failed turn into as much output as it had ever written, with the line
that says *why* — the last one — at the far end of it.

**Keeping the tail is the whole design.** The ring discards the head and names the elision, because
"the last 8 KiB" and "all of it" send a reader to different next questions and the text alone cannot
tell them apart. 8 KiB holds a stack trace or a login refusal comfortably.

**The mutex is load-bearing.** `os/exec` writes stderr from its own goroutine while the reader
goroutine reads it at exit; a `bytes.Buffer` across that boundary races only when a child is chatty
at the moment it dies, which is the flavour that survives CI for months.

**Mutation:** keeping the head — `r.buf = r.buf[:stderrRingBytes]` — is the plausible wrong
implementation, and the test reports `noise line 0` retained with the cause gone. Reverted and
re-verified.

**`internal/mockagent` was not built, deliberately.** S2 names four fake binaries, but this package's
tests already drive real children through `sh -c` and `cat`, and that reached the defect without a
new package. It earns its place when S10.1d2 needs a child that ignores `SIGINT`, and not before.

**Gate 8, and the part that stings.** The walk-back found nothing promising the flat shape — because
`04-subscription-backends.md` had it right the whole time. It names `rate_limit_info.utilization`
twice, at §7.3's dashboard column and again at §11, and even records *"0.78 of the seven-day window
in both fixtures"* — the exact number this checkpoint's test now asserts. So this was not a spec gap
and not vendor drift. It was implementation drift away from a correct spec, invisible because the
tests were written from the same assumption as the code and the one artifact that disagreed —
the captured fixture — was never opened by anything. Two independent records were right and the
third was wrong, and nothing compared them.

### R1.10 v1.2.21 host-Ollama release

**Gate:** `make check` green — 2607 tests — and `go test ./... -race` clean, both on the merged
tree: the other session's 14 unpushed commits rebased onto the E series, three conflicts resolved
by hand, and a pre-release review confirming all three preserved their intent.

**Publication:** commit `352fdee` on `main`, tag `v1.2.21`. Release workflow run 33287783208
passed verify and publish. Four archives plus the Cosign-signed `checksums.txt` and its Sigstore
bundle are public, the release is neither draft nor prerelease, and the latest redirect resolves to
`v1.2.20` — it still read `v1.2.20` for the first minute after publication, which was propagation
and was re-checked rather than recorded.

**Two things the release almost shipped wrong, both caught before the tag.** macOS CI failed on the
cancel ladder: the mock vendors looped on a `sleep` child, and macOS's `/bin/sh` re-raises SIGINT
on itself when a foreground child dies of it, so a trap that said `exit 0` read as a hard exit; the
mocks now block in `read`, a builtin with no child. And the pre-release review found a kolk-started
Ollama was killed the moment the request that started it ended — `StartManagedProcess` is an
`exec.CommandContext`, and the test fake never honoured its context — so the server now starts
detached from the caller, and a fake that does honour its context pins it. Six sentences the code
no longer honoured went with it, and the four catalogue tests `a34.6` dropped without saying so
are restored.

**Tagged at the head, not the release commit.** `chore(release): v1.2.21` was two fixes behind by
the time CI was green; a tag on it would have shipped the macOS-broken fixture and the
request-bound server.

### FR3.3 the login happens inside the session

"i dont want to login outside kolk. all need to be inside the same session."

Then, from a real run, the reason it had become urgent:

```
❯ /plogin anthropic Claude Max
Signing in to Claude Max — a separate terminal window is opening for claude.
plan login error: no terminal emulator found to open a login window; set $TERMINAL
```

**The window path cannot work on a stock macOS at all.** `LoginWindow` searches `$TERMINAL`,
`$TERM_PROGRAM` and a list of emulator binaries; Terminal.app and iTerm are `.app` bundles, not
executables on `PATH`, so the search finds nothing and the login fails outright. The other branch —
defer the login, end the session, run it after the screen is down — was not reachable in production
anyway, because `newApp` always sets `handoverWindow`.

**Why the child could not simply run in the session.** `Runtime.Run` owns `os.Stdin` from a read
goroutine while the terminal is in raw mode, and `Handover` gave the child that same descriptor. Two
readers on one fd, each taking half the keystrokes.

**A pty, and one reader.** The child gets a terminal of its own that kolk owns both ends of. The read
goroutine stays the only reader on the real terminal and *forwards* raw bytes to the pty while a
child is attached — nothing is decoded on the way through, so a password typed at a vendor's prompt
cannot also be interpreted as one of kolk's keys, and kolk remains a wire rather than a reader.

Zero cgo, `golang.org/x/sys` only, and it lives in `internal/term` because `internal/arch` permits
that import in exactly one package. Everything above receives `*os.File` and never learns which
ioctl opened it. `pty_other.go` returns `ErrNoPTY` so windows/amd64 still compiles for release, and
the window and handover paths remain as fallbacks.

**Two things verified by running them rather than by reading a man page:**

- *The size must be set after the slave is opened.* An earlier design sized the master immediately
  after unlocking it, which on darwin returns `ENOTTY` — a call that looks correct and silently is
  not. `TestAChildOnThePTYSeesARealTerminal` asserts the child reports `30 100`, so a regression here
  fails rather than producing an invisible zero-sized terminal.
- *The name comes from `Minor(rdev)`, not `TIOCPTYGNAME`.* The naming ioctl wants a 128-byte buffer
  and reading it back would need `unsafe`. The device number gives the same answer with neither cgo
  nor unsafe.

**A data race the detector caught in the new code.** `RunInSession` closed the master by clearing
`pty.Master` while both copy pumps were reading that field. `make check` does not run `-race`, so it
passed every gate; `go test ./... -race` failed three tests immediately. The master is now held
through a local, and the struct is never written while a pump is alive.

**The keyboard pump is deliberately not joined.** It is parked in a read on the session's terminal,
which does not unblock when the child exits — waiting for it would hang the login until the user
happened to press a key. `TestItReturnsWithoutWaitingForAKeystroke` pins that.

### R1.10 v1.2.22 in-session-login release

**Gate:** `make check` green — 2625 tests — and `go test ./... -race` clean.

**Publication:** commit `ba40df3` on `main`, tag `v1.2.22`. Release workflow run 33288720527 passed
verify and publish. Four archives plus the Cosign-signed `checksums.txt` are public and the latest
redirect resolves to `v1.2.22`.

**The first attempt failed the gate, and the release gate is the reason it was caught.** Run
33288587029 failed at `verify`, on a test written in this session:
`TestALoginRunsInsideTheSessionOnItsOwnTerminal` asserted the child reported `/dev/tty`. A pty slave
is `/dev/ttysNNN` on darwin and `/dev/pts/N` on linux, so the assertion described the machine it was
written on rather than the behaviour it meant to check. Everything ran green locally; nothing was
wrong with the code.

The fix asks `tty` the actual question — the output must name a device and must not be "not a tty" —
and the two sibling tests in `internal/term` and `internal/shell` carried the same trap and were
hardened with it. Worth keeping: a pty test that names a device path is testing an operating system,
not a program. CI also proved `/dev/ptmx` is available in the runner, since the failure was the
assertion rather than the allocation.

The tag was deleted and recreated on the fixed commit rather than left pointing at a build that
never published.

### FR4.1 the selected model is a ceiling

Asked for while designing agent mode over a subscription:

> "never use a superior model than selected without asking thats needs to be coded... not asked to
> the model. for example if i select sonnet model, and select agentic, only sonnet and haiku have to
> be available. not opus or fable... and the same for other vendor subs connections"

**The rule.** Orchestration routes *downward* freely — that is what the slots are for, and running a
commit or an mkdir on the cheapest model is how a subscription lasts the day. It must never route
*upward*. Selecting Sonnet makes {Sonnet, Haiku} the entire reachable set.

**Coded, not prompted.** "that needs to be coded... not asked to the model" is the load-bearing half.
A system-prompt line saying "prefer cheaper models" is a request, and a model reading it may decide —
reasonably — that this particular task deserves the strong one. A filter over the candidate set is a
guarantee, and a spending limit has to be a guarantee.

**Applied at one point.** `modelForKind` was split into `routeKind` (the choice) and `modelForKind`
(the choice held to the ceiling), so every branch — configured slot, fast lane, catalogue ranking,
effort model — passes through `underCeiling`. A ceiling with an exception is not a ceiling, and the
next branch someone adds would not know it was supposed to ask. A configured slot normally beats
every ranking; it does not beat this, because the slot was configured once and the model was selected
just now.

**Ranked per vendor**, from what each vendor says about its own models: Claude's picker calls Fable
"most capable", Sonnet "efficient for routine tasks", Haiku "fastest for quick answers". Codex and
Gemini have their own ladders. Matching is by prefix, so `claude-sonnet`, `claude-sonnet-5` and
`anthropic/claude-sonnet-4` all land on one rung.

**Two deliberate refusals to act.** A model on no ladder is never clamped — a ceiling that guessed
would be worse than one that admits it does not know. And a ceiling does not reach across vendors: a
Claude ceiling says nothing about a codex model, because that is a different bill, and silently
rewriting a model configured for another provider would be a surprise of its own.

**Made visible.** Entering agent mode now prints what the run may use and says the selection is the
ceiling. A limit nobody can see is one people find out about by being surprised. Nothing is printed
for an unranked model or for a user already on the cheapest rung, because neither line would say
anything true and new.

### FR4.2 a test that read the maintainer's own config

`TestSlashUpdateReportsRestartAndKeepsSessionAlive` began failing locally while passing on CI.

`replFixture` built its `app` directly and never called `isolateHome`, so `armRestart` resolved the
real `~/.config/kolk/config.json`. The maintainer had turned on `auto_restart_after_update`, which is
exactly what the setting is for — and the test then saw `/update` exit the REPL, which is exactly
what the setting does.

Nothing was wrong with the code. A suite that reads the developer's settings reports the wrong thing
on precisely one machine: the maintainer's. `replFixture` isolates now, like `newTestApp` already
did.

### FR4.3 two corrections the adversarial pass found in FR4.1

A four-way design investigation into agent mode over vendor CLIs, each investigation then checked by
an adversarial reader, found two defects in the ceiling shipped an hour earlier. Both were in code
already pushed, and neither had failed a test.

**A whole id namespace the ceiling could not see.** `modelRank` normalised by stripping everything up
to the last `/`, so `claude/haiku` became `haiku`, which does not prefix-match the rung
`claude-haiku`. An unranked model is deliberately never clamped — so any `vendor/model` route would
have been invisible to the ceiling, silently exempt from the one guarantee it exists to provide. All
three spellings a model arrives in are now tried: the bare plan name, the catalogue's
`provider/model`, and the namespaced `claude/haiku` folded to `claude-haiku`.

**A message that promised what the router does not do.** Entering agent mode printed
`agent runs may use: claude-sonnet, claude-haiku`. The ceiling is real, but that line predicted a
routing decision rather than stating a guarantee — and the prediction is wrong today. On a plan
session `a.Catalog` is still the gateway catalogue (`run.go` loads it regardless of backend), so
`SlotExplore` ranks gateway rows and `SlotFast` returns `FastLaneModel`, which on a non-free session
model is `google/gemini-2.5-flash`. The clamp then correctly leaves it alone — a different ladder is
a different bill — but the user had been told to expect their plan's own models.

It now says what is guaranteed: `agent runs are capped at claude-sonnet — claude-fable and
claude-opus stay out of reach`. Refusal is a guarantee; selection is not yet. `ModelsAtOrBelow` was
deleted rather than allowlisted when the dead-export gate caught it losing its caller, and
`ModelsAboveCeiling` replaced it.

**Recorded as the honest state of the work:** the ceiling is enforced. Routing a subagent to the
plan's own cheap model is the other half and is not built — on a plan session the slots still resolve
gateway ids. Saying so here matters more than the code did, because the previous line said otherwise
to the user's face.

## FR5 Stigi — agentic orchestration over vendor subscriptions (2026-08-30)

Ten checkpoints, 54 tasks, built one at a time with the failing test first. The whole feature in one
sentence: a session on model X plans a big task, labels each subtask by the capability it needs, and
each subtask runs on its own vendor process at a rung of a ladder whose top is X.

**The design was chosen by a panel, and the panel's value was not the winner.** Four architectures
were designed independently and scored by independent readers. All four judges found the same fatal
flaw — `planModelCatalog` has no `claude-haiku` row, so `ResolvePlanModel` returns `ErrNotAPlanModel`
and `planBackendFor` answers with a nil backend AND a nil error. Every one of the four designs would
have shipped looking built and changed nothing. The winning shape (capability levels, not names) was
worth less than that single finding.

**The guarantee is structural, not enforced.** X states `trivial | routine | hard`; code binds that to
an index into a roster whose element 0 is the user's own model. A stronger model is *unrepresentable*
rather than rejected — the clamp in `ceiling.go` stays as defence in depth for paths that still take
a model by name, but nothing on this path can produce one. That is why the planner prompt must never
contain a model name, and why a test checks it against `vendorLadders` itself rather than a list.

**Two real bugs were fixed on the way, both pre-existing:** an unguarded `a.Model` / `a.Backend`
written from a subagent goroutine while every other subagent read it, and an unbuffered `finished`
channel that stranded senders when a run was cancelled.

**Three tests failed for the right reason and corrected a wrong belief:**

- The cancelled-run leak took three attempts to see. `runtime.NumGoroutine()` is too blunt; counting
  by stack name is precise and still passed, because a task with the **zero `Kind` writes files** and
  `writesFiles` serialises those — all four tasks ran one at a time, so nothing was ever in flight to
  strand.
- A roster assertion encoded a miscount (`claude-opus` has two cheaper rungs, not one) rather than
  its own intent, which was that the ceiling is never checked for availability.
- The plan-limit question goes through `a.Ask.Choose`, not the `Decider`. The first version of that
  test was asked zero times.

**And one bug caught before it shipped, by checking rather than assuming.** Having lifted codex's
agent-mode refusal at C7, `subagentBackend` still handled only claude — so a codex session would have
returned `(nil, nil)`, shared one backend, and interleaved its subagents into one vendor thread:
exactly the failure the refusal existed to prevent, reintroduced by the change that removed it.
Lifting a refusal is safe once the reason is gone, not once the plan says so.

**Verification:** `make check` green at 2739 tests, `go test ./... -race` clean, `make spec` green.
Every checkpoint verified against a built binary where it had observable behaviour.

See [`STIGI.md`](../STIGI.md) for the checkpoint ledger and for what was deliberately left unbuilt.

### R1.11 v1.2.24 Stigi release

**Gate:** `make check` green at 2739 tests, `go test ./... -race` clean, `make spec` green.

**Publication:** commit `09ff233` on `main`, tag `v1.2.24`, checked with
`git merge-base --is-ancestor HEAD origin/main` before tagging. Release workflow run 33311971921
passed verify and publish. Four archives plus the Cosign-signed `checksums.txt` are public, the
release is neither draft nor prerelease, and the latest redirect resolves to `v1.2.24`.

**What a tester should know before trying it.** Agent mode works on a claude subscription; codex was
opened in the same change. The lane prints on entering agent mode, so what a run may spend on is
visible before it spends anything. Three things are deliberately unbuilt and would otherwise read as
bugs: cross-vendor rungs do not yet join the roster (the gate is enforced, the ordering is not),
`/model claude-haiku` still reports it is not a plan model (the subagent path bypasses that catalogue
on purpose), and the fast lane still returns a gateway id on a plan session.

## H10 — ordered agent-work ledger and trace polish (2026-08-31)

**Status:** complete, pending the user's separate commit/release decision.

Agent mode now preserves an ordered, durable `work.updated` ledger for the main agent and every
subagent. Each task has a stable ID and child turn, a monotonic update sequence, an observed state
and phase, plus one bounded/sanitized current step. Provider and local-tool boundaries publish into
that ledger without exposing raw tool arguments or unbounded output. The TUI projects the latest row
as `agent [i/n] · model · effort · state: summary — step`, with semantic colour that remains explicit
under `NO_COLOR`; concise milestones stay chronological while full child reports flush in plan order.

**Hardening:** concurrent task updates survive a one-event in-memory journal, an unread subscriber,
spill-file reopen, retry, provider fallback, cancellation, and wide/narrow/wide terminal resize.
The final gate also corrected Saga artifact discovery so a world-writable ancestor such as `/tmp`
cannot make an unrelated temporary checkout inherit its `SAGA.md`; normal non-Git project-ancestor
discovery remains covered.

**Verification:** affected bus/engine/TUI/CLI/provider/protocol packages passed normally and under
`-race`; `make spec` passed 29 checks; final `TMPDIR=/var/tmp make check` passed at **2,946 tests**,
with 0 lint issues, four platform compile matrices, and every budget/site/surface/installer/spec/
release/workflow/plan/pin gate green. No tag or release was created here.

## E11.0 — Cloud catalogue contract and boundaries (2026-08-31)

The Cloud catalogue work starts with a corrected contract. The public
`https://ollama.com/api/tags` response is metadata only and is never given a
Kolkrabbi or Ollama credential. Each direct model name will be normalized to
the local Ollama Cloud selector (`:cloud` for an untagged name,
`-cloud` after an explicit tag) and verified through the local `/api/show`;
capabilities and context are accepted only from that response, and only a
response identifying a remote host becomes a Cloud row.

Rows absent from the local `/api/tags` result are explicitly **not pulled** and
will show `ollama pull <name>`. They are not classified as local or free, and
no command path will pull them implicitly. Public-catalogue or proxy failure
is best effort: already-known host rows remain available and startup or
`/model` does not fail solely because discovery is unavailable.

The previous plan sentence claiming Cloud models ran with no prior pull was
wrong for current Ollama behavior and was corrected. The official Cloud
contract requires pulling the Cloud stub before local use; the local source
parser confirms the `:cloud`/`-cloud` normalization and `/api/show` proxy
boundary. `git diff --check` and `make plan-check` passed (98 checks). No
production code changed in this leaf.

### E11.1 bounded public catalogue (2026-08-31)

Added `internal/local/cloudcatalog.go` with a fixed
`https://ollama.com/api/tags` endpoint and an injectable test helper. The
request has no authorization header, strips caller cookies, refuses redirects,
and carries the caller's cancellation through a three-second ceiling. The
response is bounded to 1 MiB and 256 rows; names and retained metadata fields
are bounded and terminal-hostile names are rejected. A valid `models: null`
means an empty catalogue, while a null document, malformed JSON, non-200
response, oversized body, excessive rows, or invalid field is an error.

Verification: `TMPDIR=/var/tmp go test ./internal/local` and
`TMPDIR=/var/tmp go test -race ./internal/local` pass. Seven targeted mutations
were caught and reverted: cookie isolation, body limit, row limit, null
document guard, unsafe-name guard, metadata-field limit, and redirect refusal.
`git diff --check` is clean. The public client is not wired into the picker in
this leaf; that belongs to E11.2/E11.3.

### E11.2 local proxy enrichment (2026-08-31)

Added `internal/local/cloudmodels.go`. Public entries are normalized using
Ollama's source rules: an untagged name becomes `name:cloud`, an explicitly
tagged name becomes `name-tag-cloud`, and already normalized selectors remain
stable. Each candidate is sent to the local `/api/show`; a row is accepted
only when that response identifies a non-empty `remote_host`. Capabilities,
thinking/tools/vision flags, and context length come only from that response;
public metadata supplies display size/family/parameter/quantization fields.

The existing `/api/show` decoder now bounds response bodies to 1 MiB. Cloud
enrichment is bounded to the public row limit and five seconds, honors
cancellation, rejects invalid candidates, and reuses only cache entries whose
name, Cloud flag, and remote-host proof all match the current digest/version.
Returned Cloud rows are marked `NotPulled`; merge and presentation remain the
next leaf.

Verification: `TMPDIR=/var/tmp go test ./internal/local` and
`TMPDIR=/var/tmp go test -race ./internal/local` pass. Focused mutations for
remote proof, cache proof, alias selection, and the `/api/show` body limit all
failed and were reverted. `git diff --check` is clean.

### E11.3 merged presentation (2026-08-31)

Added separate CLI seams for public Cloud discovery and local enrichment. A
running host contributes pulled rows first, then verified Cloud catalogue rows;
duplicate normalized names keep the pulled record. Cloud discovery is
best-effort, so a public fetch or proxy failure does not hide known host rows.
The `/model` picker and `kolk models` now both distinguish a pulled Cloud row
from an unpulled catalogue row. The latter says `ollama pull <name>` while
retaining the Cloud subscription or sign-in-first label; it is never classified
as local or free. Tests keep the new production seams disabled unless a test
explicitly supplies them, preventing accidental network access.

Verification: `TMPDIR=/var/tmp go test ./internal/cli` and
`TMPDIR=/var/tmp go test -race ./internal/cli` pass. Targeted mutations removing
picker merge, command merge, deduplication, picker pull guidance, and command
pull guidance all failed and were reverted. `git diff --check` is clean.

### E11.4 final hardening and gate (2026-08-31)

The final audit added one regression for partial `/api/tags` results followed
by manifest fallback, proving that a pulled model is not duplicated when the
host listing is degraded. Its merge guard mutation failed and was reverted.
All E11 code was then formatted and checked without changing the earlier H10
work already present in this dirty worktree.

Final verification: `TMPDIR=/var/tmp make check` passed at **2,975 tests**;
architecture, purity, build tags, Darwin/Linux/Windows platform matrices,
lint, budgets, site, v0.1 surface, installer, protocol/spec, release,
workflow, plan, and workflow-pin gates all passed. Focused local and CLI race
suites passed earlier in E11. The local environment has Ollama installed but
no server listening on `127.0.0.1:11434`, so live `/api/show` and picker
rendering could not be smoke-tested; deterministic HTTP fixtures cover those
paths. No temporary mutation or review artifact remains in the repository.
No commit, push, tag, or release was created.

## V34.0a — release baseline (2026-09-01)

**Scope:** establish the reproducible pre-change baseline for the V34 completion program. No
production code, release artifact, tag, or external configuration changed.

**Baseline:** the record began on clean commit `5074e6206780c5590417a21da9512c25fea04207`
(`release: v1.2.32`, 2026-09-01T00:04:04-03:00). The host uses Go 1.27.0 on Linux amd64; `go.mod`
declares Go 1.25.0. Git is 2.55.0 and golangci-lint is 2.13.1. GoReleaser and Cosign are not
installed.

**Supported product boundary:** macOS and Linux on amd64/arm64 are runtime targets. Windows amd64
cross-builds in the advisory matrix but is not a runtime support claim. OpenRouter-compatible
endpoints and a host Ollama are supported. Subscription execution is available through Claude
Pro/Max and ChatGPT Plus/Pro provider CLIs; the current Codex catalog contains `gpt-5.6-sol`,
`gpt-5.6-terra`, and `gpt-5.6-luna`. Gemini remains visible as explicitly unsupported subscription
metadata, not a runnable backend.

**Verification:** `make fmt-check vet plan-check`, `make platforms`, and `git diff --check` passed;
the plan ratchet reported 101 checks. `make test` reached 1,874 tests, then exited 2 because this
sandbox denies `httptest` IPv6 loopback (`listen tcp6 [::1]:0: socket: operation not permitted`),
not because an assertion failed. `TMPDIR=/var/tmp make check` is also environment-blocked before
tests because `/var/tmp` is read-only. A release rehearsal remains pending until GoReleaser and
Cosign are installed in a release-capable environment.

**Review:** an independent baseline collector recorded the source, toolchain, provider, and gate
facts; the checkpoint owner verified the declared runtime boundaries and documentation walk-back.

## V34.1f — delegated execution capability envelope (Leaf A complete 2026-09-01)

**Scope:** make delegated provider children receive an explicit, host-verified workspace and network
capability while keeping provider authentication owned by the provider CLI and excluding Kolkrabbi's
ambient credentials. SAGA entrypoint/state progression remains V34.3f Leaf B and was not changed.

**Implementation:** `verifiedProjectRoot` canonicalizes and validates the project directory before
agent construction. `engine.SubagentCapabilities` is copied per task and mapped by the CLI to
`agentcli.ExecutionOptions`. Shell process runners validate/canonicalize cwd and use the existing
credential-scrubbed inherited environment. Codex uses `--cd`, explicit `--add-dir`, and
`sandbox_workspace_write.network_access=true` only for a network-enabled workspace-write child.
Claude uses explicit cwd/additional roots and fails closed for a network-disabled non-empty envelope,
because its enabled Bash/WebFetch/WebSearch tool set has no equivalent narrow network-off control.
Capability status names the workspace and network state; invalid handoffs do not silently run from
the parent directory.

**Focused verification:**

- `TMPDIR=/tmp go test ./internal/shell ./internal/provider/agentcli ./internal/engine ./internal/cli -count=1`
  passed.
- `TMPDIR=/tmp go test -race ./internal/shell ./internal/provider/agentcli ./internal/engine ./internal/cli -count=1`
  passed.
- Adversarial tests covered relative/missing/file roots, nested and sibling checkouts, symlink roots,
  duplicate additional roots, API/token sentinel variables, Codex network omission, Claude fail-closed
  network-off, provider startup failure, and cancellation. The Codex runner-handoff test was tightened
  after review so it verifies the actual option-aware seam.

**Independent review:** a separate read-only Codex 0.149.1 run, limited to three shell commands and
the changed execution-boundary files, reported `CLEAN`. It checked workspace/symlink escape, provider
network flags, environment leakage, capability propagation, and compatibility without editing.

**Repository gate:** `TMPDIR=/tmp make check` passed with **3,051 tests**. Architecture, purity, build
tags, Darwin/Linux/Windows platform matrices, lint, budgets, site, v0.1 surface, installer, protocol/
spec, release, release workflow, release verifier, smoke workflow, plan, and workflow-pin checks all
passed. During the first run, `internal/arch` correctly caught the new option-aware builders leaving
legacy exported builders test-only; the empty-envelope runtime compatibility path fixed that, and the
rerun was fully green. No commit, push, tag, or release was created.

## V34.3f B1 — typed internal SAGA posture (complete 2026-09-01)

Added `engine.Posture` with the internal `PostureSaga` value. The SAGA CLI path passes that marker
when constructing its agent; the engine appends one fixed progression directive to system prompt
construction only. Ordinary modes retain the previous prompt, while user/chapter requests and the
durable conversation do not receive a repeated SAGA paragraph.

Verification: focused engine/CLI tests and their `-race` variants passed for `TestSagaPosture`,
`TestDefaultPosture`, and existing SAGA tests. A temporary mutation disabling the SAGA branch was
caught by the positive posture test and restored. A separate read-only Codex review reported
`CLEAN`. `git diff --check` passed. B2 (the exactly-one-bounded-wake front door) remains queued;
no commit, push, tag, release, scheduler, or provider turn was performed for B1.

## V34.3f B2.1 — one bounded SAGA wake (complete 2026-09-01)

`SagaRunner.RunWake` now plans and executes at most one chapter per invocation, preserving the
explicit wake boundary instead of entering the older continuous loop. The selected chapter number is
recorded in `ActiveChapter`; successful and failed outcomes are persisted, and the CLI gives a
resumable next-wake or retry command. Artifact-write errors now fail the wake without falsifying the
chapter's real outcome. Worker and verification cancellation preserve a resumable `executing`
chapter without strikes, rollback, or repair after cancellation.

Verification: `TMPDIR=/tmp go test ./internal/engine ./internal/cli -count=1` and their `-race`
variants passed; focused wake/CLI/persistence/active-chapter/cancellation tests passed; and
`git diff --check` passed. Targeted mutations disabling the bounded return, active-chapter marker,
persistence propagation, and cancellation guards were caught and restored. An independent
read-only review found three defects in the first pass—swallowed artifact-write errors, false worker
cancellation strikes, and false verification-cancellation strikes/rollback—and reported no further
 B2.1 defect after correction. B2.2-C1 (inline-only SAGA surface) follows as the next integration
 checkpoint. No commit, push,
tag, release, scheduler installation, or provider turn was performed.

The final `TMPDIR=/tmp make check` rerun passed with **3,063 tests** and all repository gates. It also
confirmed the B1 cleanup removing the unused exported `PostureDefault` zero-value constant; no
runtime behavior changed.

## V34.3f B2.2-C1 — inline-only SAGA surface (complete 2026-09-01)

SAGA is now a workflow marker inside an ordinary Kolkrabbi request, for example `build an ecommerce
web app /saga`. The standalone `kolk saga` command and its `run`, `resume`, `status`, and `stop`
subcommands were removed from the public command surface. The marker parser accepts only a
whitespace-delimited `/saga`, preserves the remaining goal text, and rejects URL/path-like text from
accidentally changing workflow posture. A bare `/saga` gives inline usage guidance.

Verification: `TMPDIR=/tmp go test ./internal/cli -count=1` and `git diff --check` passed. A targeted
mutation replacing the marker search was caught and restored. README, site, command-surface,
roadmap, SAGA plan, architecture notes, and durable checkpoint records were updated; stale command
tests were removed or rewritten while artifact-root confinement coverage remained. C2 (routing the
inline marker through the SAGA posture and bounded wake in both REPLs) is next. No provider turn,
commit, push, tag, release, or scheduler action was performed.

## V34.3f B2.2-C2.1 — session-preserving SAGA posture seam (complete 2026-09-01)

The current engine agent can now enter the internal `PostureSaga` value and return to the ordinary
empty posture without creating a second session. `Agent.SetPosture` accepts only those two internal
values, refreshes the existing session system message, persists the transition, and leaves arbitrary
posture text rejected and state-neutral. The fixed SAGA directive appears exactly once while active
and the ordinary system message is restored byte-for-byte afterward.

Verification: `TMPDIR=/tmp go test ./internal/engine -run
'^(TestPostureCanEnterAndLeaveSAGAOnTheCurrentSession|TestSagaPostureIsAnInternalSystemDirective|TestDefaultPosturePreservesTheOrdinarySystemPrompt)$' -count=1`, its `-race` variant, and
`git diff --check` passed. A temporary mutation removing the system refresh was caught by the new
regression test and restored. C2.2a (plain REPL inline routing) follows; no provider turn,
commit, push, tag, release, or scheduler action was performed.

## V34.3f B2.2-C2.2a — plain REPL inline routing (complete 2026-09-01)

The ordinary REPL now recognizes a non-empty whitespace-delimited `/saga` marker before slash
dispatch, persists the cleaned goal, and runs one bounded wake through the existing agent and session.
The SAGA posture is active only for that wake and is restored afterward. `runSagaLoop` no longer
constructs a second agent. Goal persistence refuses outside a Git repository before creating an
artifact, and wake failures remain errors while ordinary posture is restored.

Verification: `TMPDIR=/tmp go test ./internal/cli -count=1`, focused inline-routing tests with
`-race`, and `git diff --check` passed. The routing mutation that disabled the REPL branch was
caught (the test reached the ordinary provider path and failed) and restored. C2.2b, TUI routing
with status and Esc behavior preserved, follows; no provider turn, commit, push, tag, release, or
scheduler action was performed.

## V34.3f B2.2-C2.2b — TUI inline routing (complete 2026-09-01)

The TUI turn callback now recognizes a non-empty inline `/saga` marker before slash-command and
picker dispatch, then uses the shared current-agent/session boundary. The existing Runtime owns the
per-turn cancellable context and final lifecycle. Escape cancels a held SAGA wake, queued work is
dropped according to the existing interrupted-turn contract, and the TUI exits cleanly through EOF.

Verification: `TMPDIR=/tmp go test ./internal/cli -count=1`, focused inline-routing tests with
`-race`, and `git diff --check` passed. A mutation disabling the TUI inline branch was caught by the
current-session integration test and restored. C3 (one-wake progression, durable log, and Esc
lifecycle hardening) is next; no provider turn, commit, push, tag, release, or scheduler action was
performed.

## V34.3f C3.1 — durable executing-before-work state (complete 2026-09-01)

`RunChapter` now persists the chapter's `executing` marker before invoking its worker, including when
the chapter was just appended by the planner. A failed pre-work artifact write stops before provider
or repository mutation, leaves the in-memory chapter resumable, and returns the typed persistence
failure without spending a strike or reporting a false chapter failure. The normal failed-worker
path records the truthful `executing` then `failed` artifact sequence.

Verification: `TMPDIR=/tmp go test ./internal/engine -count=1`, focused saga/chapter/wake/posture
tests with `-race`, and `git diff --check` passed. A mutation removing the pre-work persistence
guard was caught by the new worker-observation test and restored. C3.2 (wake cancellation and
terminal-state hardening) remains queued; no provider turn, commit, push, tag, release, or scheduler
action was performed.

## V34.3f C3.2a — durable terminal-state normalization (complete 2026-09-01)

`SagaRunner.Run` and `RunWake` now treat durable `completed` and `blocked` saga statuses as
authoritative terminal states, preventing a later wake from reopening pending chapters in either
artifact. Goal completion and planner exhaustion persist `completed` through one terminal boundary;
resumable wake, chapter, budget, and timeout stops remain `in-progress`. A failed terminal artifact
write is returned as an error rather than reported as a successful stop, while the in-memory marker
remains truthful for diagnostics.

Verification: `TMPDIR=/tmp go test ./internal/engine -count=1`, focused saga/chapter/wake/posture
tests with `-race`, and `git diff --check` passed. A mutation clearing the terminal status was caught
by the completion persistence and persistence-failure tests and restored. C3.2b (wake cancellation
boundaries) remains queued; no provider turn, commit, push, tag, release, or scheduler action was
performed.

## V34.3f C3.2b1 — cancellation-error preservation (complete 2026-09-01)

Both `RunWake` and the continuous `Run` API now preserve the active context cancellation together
with any failure raised while persisting the resumable `executing` marker. A cancelled chapter stays
resumable and non-striking; a cleanup-write failure is not hidden behind a bare `context canceled`
result and the wake is never reported as successful.

Verification: `TMPDIR=/tmp go test ./internal/engine ./internal/cli -count=1`, focused
saga/chapter/wake/posture tests with `-race`, and `git diff --check` passed. A mutation removing the
joined-cause preservation was caught by `TestSagaCancellationResultPreservesJoinedCauses` and
restored. C3.2b2 (TUI interrupted/ready lifecycle) remains queued; no provider turn, commit, push,
tag, release, or scheduler action was performed.

## V34.3f C3.2b2 — TUI interrupted/ready lifecycle (complete 2026-09-01)

The inline SAGA TUI path now refreshes metadata with the turn's truthful terminal lifecycle instead
of briefly relabeling a returned wake as `working`. Runtime remains the owner of the final locked
transition and interruption marker. A staged-input regression proves that Escape cancels an inline
SAGA wake, leaves the lifecycle `interrupted`, clears queued work and the draft, starts no second
turn, and exits cleanly; the CLI integration still proves posture restoration.

Verification: `TMPDIR=/tmp go test ./internal/cli ./internal/tui -count=1`, focused CLI/TUI tests with
`-race`, and `git diff --check` passed. A mutation changing cancellation classification to `working`
was caught by `TestTUITurnLifecycleDoesNotRelabelTerminalTurnsAsWorking` and restored. C3.2 is now
closed; C4 (consolidated tests, documentation, and release gates) is next. No provider turn, commit,
push, tag, release, or scheduler action was performed.

## V34.3f C4.1 — consolidated repository gate baseline (complete 2026-09-01)

The accumulated C3 worktree was put through the complete repository gate before the next review leaf.
The first `TMPDIR=/tmp make check` run reached lint and found one real formatting defect in the new
`internal/tui/runtime_test.go` regression test. The installed `golangci-lint v2.13.1` formatter
required the repeated `[]byte` element type to be elided in the staged Escape/EOF literals; the
canonical forms are `{0x1b}` and `{0x04}`. This was a test-formatting-only repair with no runtime or
assertion change.

Focused verification passed with `TMPDIR=/tmp go test ./internal/tui -count=1`,
`gofmt -l internal/tui/runtime_test.go`, `golangci-lint run ./internal/tui`, and `git diff --check`.
The final `TMPDIR=/tmp make check` passed all gates: 3,079 tests, architecture, purity, build tags,
Darwin/Linux/Windows platform matrices, lint, budgets, site, v0.1 surface, installer, protocol/spec,
release, release workflow, release verifier, smoke workflow, plan, and workflow-pin checks. The
binary budget measured 9.46 MB, cold-start p50 measured 3.7 ms, and the plan ratchet passed 101
checks.

This records the initial red result, exact repair, independent formatter check, and final green rerun.
No provider turn, commit, push, tag, release, or scheduler action was performed. C4.2 — independent
ledger and release-line review — is the next leaf.

## V34.3f C4.2a — ledger inventory and stale-claim correction (complete 2026-09-01)

The V34 plan and historical checkpoint ledger were reconciled without changing production behavior.
The inventory records two complete V34 leaves (`V34.0a`, `V34.1f`), `V34.3f` as partial because its
visible running TUI progress promise belongs to queued C5, and the remaining V34.0b/c, V34.1a–e,
V34.2a–f, V34.3a–e, V34.4a–d, V34.5a–e, and V34.6a–c as queued or owner-dependent. Historical
SQLite/sidecar items remain explicitly superseded; Windows, generated clients, platform clients,
clean-machine proof, typed warning/replay work, and provider captures are named as deferred or
owner-dependent rather than treated as shipped.

The review also corrected one stale open claim: A12.5's budget/architecture verification is now
verified by the completed full gate (3,079 tests, 9.46 MB binary budget, 3.7 ms cold-start p50, all
architecture/purity/build-tag checks green). The parent A12 remains partial because its superseded
SQLite and open API decisions remain. `make plan-check` passed 101 checks; `gofmt -l .` and
`git diff --check` were clean.

No provider turn, commit, push, tag, release, or scheduler action was performed. C4.2b — release-line
consistency — is next.

## V34.3f C4.2b — release-line consistency (complete 2026-09-01)

The release identity is consistent across the repository and remote refs: `main`, `origin/main`, and
the annotated `v1.2.32` tag all resolve to `5074e6206780c5590417a21da9512c25fea04207`. GoReleaser
stamps `.Version` and keeps snapshot rehearsals on `1.2.32-dev.{{ .ShortCommit }}`; the site badge
and release contract identify `v1.2.32` as current. README's `v1.2.3` occurrence is an instructional
signature example, not a stale current-version claim.

The unstamped development command reported `kolk dev go1.27.0 linux/amd64`. A release-shaped local
binary stamped with `1.2.32` reported the expected release identity and help surface; its `+dirty`
commit suffix was expected from this intentionally dirty worktree. `./scripts/check-release-tag.sh
v1.2.32` passed, `make release-check release-workflow-check release-verifier-check` passed 24, 41,
and 30 checks, and `./scripts/test-site.sh` passed 162 checks. No mismatch or code defect was found.

No provider turn, commit, push, tag, release, or scheduler action was performed. C4.2c — independent
closeout rerun and disposition — is next.

## V34.3f C4.2c — independent closeout rerun and disposition (complete 2026-09-01)

A separate read-only reviewer inspected the V34 plan, `PLAN.md`, `CHECKPOINTS.md`, build log, release
configuration/scripts/workflows, and refs. It reported `CLEAN`: the V34 statuses and queued,
deferred, and superseded mappings are coherent; the `v1.2.32` release line is consistent; and the
next disposition is V34.0c owner scope freeze. No files or remote refs were changed by the reviewer.

The local final race pass passed `internal/cli`, `internal/engine`, `internal/tui`,
`internal/provider/agentcli`, and `internal/shell`. The final post-walk-back `TMPDIR=/tmp make check`
passed all repository gates with 3,079 tests, including architecture, purity, build tags, all
Darwin/Linux/Windows platform matrices, lint, budgets, site, v0.1 surface, installer, protocol/spec,
release, release workflow, release verifier, smoke workflow, plan, and workflow-pin checks. The plan
ratchet passed 101 checks.

C4.2 is closed and V34.0b is complete. V34.3f remains partial because C5's visible TUI progress-log
work is still queued. The V34 program remains open; V34.0c owner scope freeze is the next leaf. No
provider turn, commit, push, tag, release, or scheduler action was performed.

## V34.0c.1 — owner-scope evidence inventory (complete 2026-09-01)

This read-only leaf compared the current user-facing claims and executable boundaries before any
owner scope decision. `go run ./cmd/kolk help` exposed chat/code/agent modes, model/effort/mode,
plans/pmodels, localia, sessions, stats/dash, serve, devices, version, completion, doctor, and no
standalone SAGA command family. `go run ./cmd/kolk pmodels` exposed Claude Pro/Max and ChatGPT
Plus/Pro subscription rows; Gemini rows were explicitly marked `unsupported subscription`.

The current platform script compiles Darwin amd64/arm64, Linux amd64/arm64, and Windows amd64;
README's boundary correctly calls Windows advisory and unsupported at runtime. README and the local
model contract describe OpenRouter-compatible endpoints and host Ollama. The provider matrix keeps
other subscription providers as unaccepted inventory, and the OS sandbox remains deferred. The
inline `/saga` path is current, while its visible TUI progress-log promise remains queued as C5, so
V34.3f is still partial.

The release-line evidence was consistent: `v1.2.32`, `HEAD`, `origin/main`, and the annotated tag
resolve to the same commit; `./scripts/check-platforms.sh`, `./scripts/check-release-tag.sh v1.2.32`,
`make release-check release-workflow-check release-verifier-check` (24/41/30 checks), and
`./scripts/test-site.sh` (162 checks) passed. No production code or remote state changed.

The owner-facing proposal was awaiting an explicit decision at the end of this leaf. V34.0c.2 now
records that decision separately below; the inventory itself made no scope assumption.

## V34.0c.2 — owner scope acceptance (complete 2026-09-01)

The owner accepted the bounded v1 scope and explicitly included OS-level sandboxing. The accepted
scope is the current macOS/Linux CLI/TUI product, OpenRouter and compatible endpoints, host Ollama,
Claude/Codex subscription handoff, current agent/SAGA surfaces, sessions/dashboard/service, existing
permission boundaries, and OS-level sandboxing. Windows runtime support, desktop/iPad/Android clients,
additional subscription providers, generated clients, and still-open provider/local implementation
work remain deferred.

This is a scope decision, not a claim that the OS sandbox is already shipped. Its implementation and
platform-specific negative proof remain V34.1e work; until then it must not be labeled available.
The owner also confirmed that the clean-machine/provider proof was performed. That owner-provided
evidence is accepted for scope disposition, while the exact reproducible transcript remains attached
to V34.5b and the release-candidate evidence remains independent under V34.5c–e.

No production code, release ref, credential, or remote state was changed. V34.0c.3 must now walk this
decision back through the current plan, README, capabilities site, contracts, and release wording;
V34.0c.4 then performs the independent scope-exit review.

## V34.0c.3 — accepted-scope documentation walk-back (complete 2026-09-02)

The accepted matrix is now explicit across every current-facing scope surface. README says the
current binary has no OS sandbox and identifies native Linux/macOS isolation as accepted-but-
unshipped v1 work. The capabilities page uses its designed/not-shipped state for the same boundary;
Windows runtime and desktop/mobile clients remain planned rather than available.

`PLAN.md` plus plans 13, 23, and 34 record the owner amendment. The sandbox matrix distinguishes
shipped in-process controls, accepted Linux/macOS isolation under V34.1e, and post-v1 containerized
SAGA work. Plan 24 now reflects the existing Claude/Codex handovers and defers every additional
subscription provider; plan 25 freezes host Ollama into v1 while retaining V34.4d's proof burden.
The top-level clean-shell, clean-machine, and T0.5 boxes record the owner's 2026-09-01 provider proof,
with V34.5b retaining ownership of its durable transcript link.

`git diff --check`, `make plan-check` (101 checks), `./scripts/test-site.sh` (162),
`./scripts/test-v01-surface.sh` (15), and the release, release-workflow, and release-verifier gates
(24/41/30) passed. `CHANGELOG.md` remains unchanged because no released runtime behavior changed.
No production code, provider turn, commit, push, tag, release, credential, or remote state changed.
V34.0c.4 is the independent scope-exit review.

## V34.0c.4 — independent scope-exit review (complete 2026-09-02)

McClintock independently reviewed the accepted scope matrix and the current-facing README, site,
PLAN/checkpoint mirrors, build log, and plans 13/23/24/25/34 without editing files. The result was
`CLEAN`: OS sandboxing is accepted but unshipped under V34.1e; Windows, desktop/mobile, additional
providers, generated clients, and containerized SAGA remain deferred; Claude/Codex and host Ollama
remain subject to V34.4; and owner-provided clean-machine/provider evidence stays separate from
repository-local gates with V34.5b owning its transcript link.

The independent rerun passed `git diff --check`, plan 101, site 162, surface 15, and release
24/41/30. Its stale-claim search found only dated or explicitly superseded historical records. It
did not close V34.1e, V34.3f/C5, V34.4, V34.5c–e, or V34.6.

V34.0 is closed. No production code, provider turn, commit, push, tag, release, credential, or
remote state changed. V34.1a credential-to-endpoint binding is the next mandatory leaf.

## V34.1a.0 — credential endpoint threat model and red evidence (complete 2026-09-02)

The current startup path loads the OpenRouter credential, constructs `provider.NewClient` with an
always-authenticating `secret.AuthTransport`, and only then overwrites the public `Client.BaseURL`
from `--base-url`, `OPENROUTER_BASE_URL`, or saved config. The transport attaches the bearer to the
first request on that replacement host; redirect refusal does not protect that initial request. Both
catalog `/models` and turn `/chat/completions` use this client.

The exploit already existed as a passing assertion: `TestStoredCredentialCompletesOfflineDefaultTurn`
requires an arbitrary `httptest` server selected by saved/environment base URL to receive the stored
OpenRouter bearer. `TestModeAgentFlagRunsTheOrchestratedPipeline` follows the same path, and provider
test `TestKeyNeverAppearsInAnythingPrintable` mutates an authenticated client's BaseURL and expects
the key to arrive. Focused CLI/provider reproductions passed, proving the vulnerable contract is
live; host Ollama's separate keyless client and redirect refusal also passed and are not the hole.

V34.1a uses a trusted-endpoint model: an OpenRouter credential may be attached only to the canonical
`https://openrouter.ai` origin. General compatible endpoints become keyless; authenticated custom
endpoints require a future explicit origin-bound credential rather than implicit reuse of the only
secret Kolkrabbi holds. The checkpoint is split into origin-bound transport, startup construction,
adversarial/compatibility, and independent closeout leaves. No production code or remote state
changed; V34.1a.1 is next.

## V34.1a.1 — origin-bound credential transport (complete 2026-09-02)

Three red tests first proved that an unbound token transport, an authenticated client's mutated
`BaseURL`, and a replacement OpenRouter verification URL all reached their underlying transport.
The new transport constructor now privately binds a credential to one normalized scheme/host/
effective-port origin and returns `ErrCredentialOrigin` before network I/O on an absent or mismatched
binding. Normal OpenRouter clients and key verification use only the compiled canonical OpenRouter
origin. Redirect refusal is enforced both by the provider client and independently by the transport.

Independent review found a high zero-to-nonzero token race after the first implementation: one read
could skip validation while a second read attached a concurrently installed credential. The token is
now private and synchronized, and `RoundTrip` validates and attaches one snapshot. The re-review then
found a nil-auth initialization race in `Client.SetKey`; that unused topology mutation was removed.
`SetKey` now rotates only an already-bound OpenRouter transport and returns `ErrCredentialBinding` for
keyless/custom/host clients. Concurrent transport- and provider-level tests cover both boundaries.
The reviewer's final result was `CLEAN` with no file changes.

Scoped package tests passed ten repetitions and `-race`; vet, architecture, whole-module compile,
and diff checks passed. Removing the origin comparison failed all five leak regressions. Removing the
token write lock produced race reports in both concurrent regressions. Each mutation was restored
byte-identically; final transport SHA-256 was
`0d435b8d0ca4aeb6d0096c54fdc87bbe219ec58a6610e5709ea13d2da7f1edcc`.

The two CLI tests that encoded the original leak now stop safely at `ErrCredentialOrigin`; they stay
red until V34.1a.2 builds custom compatible endpoints keylessly. No full-suite-green claim, provider
turn, commit, push, tag, release, credential, scheduler action, or remote mutation occurred.
V34.1a.2 is next.

## V34.1a.2 — endpoint-first startup/client construction (complete 2026-09-02)

Endpoint resolution now precedes credential resolution in both ordinary startup and `/models`.
`providerClientForEndpoint` centralizes the decision: canonical OpenRouter gets its bound credential;
every other OpenAI-compatible endpoint gets a keyless client with no OpenRouter attribution and never
touches the OpenRouter credential manifest. The endpoint precedence remains flag → environment →
saved config → default. The old ambiguous `provider.NewClient` API was removed, and local test-server
fixtures now use `NewCompatibleClient` unless they explicitly test a bound auth transport.

The two corrupt-manifest custom-endpoint regressions and the four-case precedence test were red under
the old construction and green after the change. Mutating the endpoint guard to force credential
resolution made the custom-manifest and precedence tests fail; restoration returned
`internal/cli/provider_client.go` to SHA-256
`09803b67f5cdc19fc8ff5d92ebfc6198692c0396d02fe141707f78b38a15abeb`. Full `make check` passed with
3,099 tests, all platform/build/release/workflow gates, `-race` focused packages, vet, and diff
checking. A separately spawned reviewer was unavailable because the provider usage limit was reached;
the broader adversarial URL matrix and independent final closeout remain V34.1a.3/.4. No provider turn,
credential, commit, push, tag, release, scheduler action, or remote state changed.

## V34.1a.3 — adversarial and compatibility matrix (complete 2026-09-02)

The credential-origin rule is now judged against one shared table rather than a handful of
hand-picked URLs. `replacementOrigins` holds eighteen shapes an attacker, a typo, or a well-meaning
proxy config can produce — lookalike hosts on either side of the name, the canonical host hidden in a
path or query, a trailing-dot FQDN, HTTP downgrades with and without `:443`, wrong and zero-padded
ports, three userinfo shapes, scheme-relative, schemeless, loopback, and empty — and
`canonicalSpellings` holds seven ways of writing the one trusted origin. The client's catalog and
turn requests, the key verifier, and the startup builder are all run over both tables, so a row
added for one is added for all. Every replacement is refused before the transport is called and no
refusal mentions the key; every canonical spelling lands on `https://openrouter.ai` with the bearer
and nowhere else.

The matrix found one thing worth changing: `StreamChat` decided whether to send OpenRouter's
`usage.include` extension by whether the URL *contained* `openrouter.ai`, so a compatible endpoint
at `…/openrouter.ai/api/v1` got OpenRouter's request shape. It now follows the client's origin.
Not a leak — the same substring habit the binding exists to replace. Two of the new assertions were
wrong as first written (they expected the wire host lower-cased); the guard was right and the test
was corrected, and the dossier says so.

Two observations are handed forward rather than fixed here: a userinfo-bearing endpoint gets a
keyless client but Go's `http.Client` still sends the URL's own userinfo as Basic auth to the host
the user named (V34.1d owns rejecting userinfo), and a query or fragment on the canonical URL binds
correctly but produces a malformed request path (config validation, not credential work).

## V34.1a.4 — walk-back and independent closeout (complete 2026-09-02)

The words caught up with the code: the flag help, the setting description, `kolk config
set-base-url`, README, `SECURITY.md`, the capabilities page, and the plan doc all say a non-OpenRouter
endpoint is used without a key. Eight guards, one mutation each, eight focused failures, eight
byte-identical restores.

Then the part that earned its place. An independent reviewer was told to break the binding, not to
read it, and did: `strings.ToLower` folds U+0130 — the Turkish dotted capital I — to a plain ASCII
`i`, so `https://openrouter.aİ/api/v1` compared equal to the canonical host at every layer while
net/http, applying IDNA, dialed `openrouter.xn--ai-sub`. The CLI loaded the stored key for it. Low
practical severity (no such TLD, and TLS verifies the name before the header is written), but the
invariant says a lookalike *cannot* attach the credential, and this one could. It is the only rune
above ASCII with that property against this host name; the reviewer proved that by scanning them all.

The fix refuses any non-ASCII host before lowering, which makes the accepted set exactly the case
variants of `openrouter.ai` and nothing cleverer. Re-review returned CLEAN after a 7,054-candidate
scan in the other direction — every ASCII insertion, substitution, and percent escape at every
position — found nothing the guard accepts that dials elsewhere. `make check` green at 3,190 tests.

Worth keeping from this one: a case-fold is a Unicode operation and a host name is not, and a review
that only reads the diff would not have found the difference. V34.1a is closed. V34.1b is next.

## F1 — the inline SAGA advances, keeps its goal, and resets (complete 2026-09-02)

The inline saga could not get past its first chapter, and the reason was a guard doing the right
thing for a loop that no longer existed. In the old multi-chapter `Run`, planning happened inside
the same call, so "every chapter is done" meant the run was over. `RunWake` plans one chapter per
wake, so "every chapter is done" is exactly the state in which the planner must be asked — and the
guard in front of it returned "nothing left to work" instead. Deleted; the executor's own terminal
judgement, from the artifact's `Status` line, is the only one.

Two more defects sat on the same path. The wake messages tell the user to type `next chapter /saga`
and `retry /saga`, and `saveSagaGoal` made those words the goal; the planner then planned "retry"
for the rest of the run. Now a saga in flight keeps its goal and the text is a note, shown to the
planner and the worker beside the goal it must not replace. And a finished `SAGA.md` was reused
verbatim, so a new `/saga` in that repository was told the old goal was met; now it is archived as
`SAGA.<started>.md` and the request starts a new saga, with no subcommand and nothing deleted.

Smaller, from the same review: the wake budget was built from three of the artifact's four limit
lines and blocked a five-strike saga at three; the verifier threw away a failed `git commit` when
the user cancelled at the same moment. Both fixed with the test that shows the old behaviour.

Each fix was reintroduced by mutation and its test failed; each file came back byte-identical. The
REPL-level test is the one to keep: chapter 1 done, `continue /saga`, and the assertion that the
scripted provider received exactly one request — the planner — is the difference between a saga
and a chapter.

## F2 — delegated execution says what it does, and does what it says (complete 2026-09-02)

Three places described a child's network access — the briefing, the status line, and the vendor's
argv — and nothing made them agree. `run.go` declared network for every child; Codex expressed
"disabled" by saying nothing, which left the user's own config in charge; and the plan doc promised a
policy the code did not have. Now the decision is made once, in the engine, per task, from three
inputs: the `subagent_network` policy, the task's kind, and whether the vendor's child has a switch.
`auto` gives the network to research and withholds it from everything else; Codex is told both ways;
Claude, which has no switch, is declared enabled rather than pretended disabled, and under the strict
policy is refused. Everything downstream renders from that one answer.

The reviewer's third finding was reconsidered rather than fixed as filed. The one-shot path now
scrubs the vendor's own API key, and the reviewer read that as a regression for a Codex user who
authenticates with `OPENAI_API_KEY`. It is — and it is right. A subscription child that receives the
API key bills the API instead of the plan, which is the spend rule violated sideways, and the
`ProcessOptions` comment had already recorded that environment is not a thing a caller may hand a
provider child. What the scrub did need was the shapes it missed: `AWS_SECRET_ACCESS_KEY`,
`GITHUB_PAT`, and `OPENAI_API_KEY_BACKUP` all passed through a suffix-only list. An allowlist was
considered and rejected: a coding child runs the repository's build tools, which read whatever the
user's shell exported, and a list that had to know them all would break the tools first.

## F3 — Fable is a model the harness can select and route below (complete 2026-09-02)

The model the program is named after could not be selected through the plan selector: the catalog
had sonnet on Pro and opus on Max and nothing else, so `claude-fable` resolved as "not a plan model"
and `claude-haiku` — the rung every trivial task should run on — likewise. Both rows exist now, and
both are backed by a live check rather than a memory of the vendor's docs: an invented model came
back `unrecognized_model` at zero cost, and `haiku` and `fable` each answered a one-turn call on this
machine's login. The CLI's own help lists the five effort levels and names `fable` as an alias.

One correction to the plan, recorded rather than smoothed over. F3.3 set out to build plan-native
downward routing because the build log said, on 08-30, that it was not built. STIGI built it on
08-30 and 08-31. On the roster path `bindLevel` always binds, so the gateway slots the old note
worried about are unreachable for a plan session; F3.3 became a Fable-specific test of what exists.
A guard test that asserted the catalogue did *not* know haiku — the premise for bypassing it — was
rewritten to assert the property that actually matters: every ladder rung opens through the
connector manifest, never nil-and-nil.

The one behaviour change beyond the catalog: at the top rung with nothing signed in, the agent lane
used to say nothing, on the grounds that the user had just chosen Fable and there was nothing to add.
There was: a sign-in would let trivial work run on Haiku, and a saving nobody mentions is one nobody
takes. It says that now, and only that.

## Discover, don't burn — owner decision recorded (2026-09-02)

F3 added two Claude rows to the plan catalog by hand after a live check, and the owner's response
was the right one: that is a table, and tables rot. "Do not burn model names before knowing what's
available … tomorrow claude or codex will update his model names and kolk will stop working
correctly" — and, when the first draft named two vendors, "this should be like these for EVERY
vendor." The proof arrived within the hour: `codex debug models` is an official, zero-cost catalog,
and it does not contain `gpt-5.6-pro`, which kolk has burned into both `codexRungs` and the plan
catalog since 08-30, while it does contain `gpt-5.5`, `gpt-5.2`, and an `ultra` effort kolk refuses.
Claude Code, by contrast, has no listing at all: a valid name can only be confirmed by spending a
turn (`--max-turns 0` still spends one), an invalid one fails locally for free, and an
unreachable-API probe retries with backoff for minutes. So "mapping" means two different things —
a listing where the vendor offers one, seed-and-verify where it does not — and the port has to
carry both under one status vocabulary. Recorded as F4 of `FABLE_OPTIMIZATION.md`, ahead of the
efficiency and cleanup phases, because a model command that lies is worse than a slow one. The
2026-09-02 Codex catalog is checked in as a fixture.

## F4.1–F4.2 — the discovery port, and Codex answers it (2026-09-02)

The port is small on purpose: `Discover(ctx) (VendorCatalog, error)`, four statuses, and a rule that
"cannot list" is an answer with a reason rather than an absence. What makes it a contract is the
registry test — every plan kolk can name must resolve to a lister — and what makes it the owner's
rule is that the gateway preview serves every vendor without a catalog by prefix, so a new API-key
connector gets a row the day it is added and never a hand-typed table.

Codex answered on the first try: `codex --version`, `codex debug models`, eight rows, fifty
milliseconds, through the same scrubbed child path a subagent uses. The checked-in fixture is the
`--bundled` catalog; the live one differed already — `gpt-5.4` and its mini are hidden in the binary
and listed by the service — which is the small daily drift the owner was describing, observed the
same afternoon.

One thing worth keeping from the mutations: a reverse `sed` that matched `return nil` restored two
lines instead of one and quietly broke the registry's default case. The cli suite caught it inside
the same run. A mutation script is code too; anchor its reversals on a marker, not on a phrase the
file may contain twice.

## F4.3 — the first prompt is the verification (2026-09-02)

The owner's correction turned Claude's missing catalog from a problem into a shape: the gateway
knows the exact names, the CLI takes a family alias, so a Claude row is a family — `opus` with
`anthropic/claude-opus-5`, `4.8`, `4.5` behind it, the biggest context among them, the CLI's five
efforts — shown before any prompt and marked `unverified`, because the gateway knowing a name is
not the same as this login being able to use it. The first prompt settles that: the stream-json
`init` frame reports the model the vendor actually resolved, and the verifying backend — which
already existed to confirm the connector on its first answered turn — now records that id and
promotes the row. A refusal in the vendor's own words retires the row; anything else teaches the
catalog nothing, because a network error is not information about a model.

Two small honesties from the mutations. `Replace` had to carry `verified` forward across a
re-discovery — a vendor listing a model again does not un-prove a turn — and keep a dropped row as
`gone`, so the person who configured it is told rather than left with a name that silently stopped
resolving. And one planned mutation did not go red: the explicit skip of `:batch`/`-fast` variants
was redundant with the family pattern, so it was deleted; a guard nothing can prove is not a guard.

## F4.4 — every start, every login (2026-09-02)

The mapping now runs where the owner said it should. Startup asks every signed-in vendor behind
the prompt, on the same background lane the gateway catalog refreshes on, so a slow vendor never
holds the first prompt; a login asks that vendor in front of the user and says, in one line, what
it found and how — `codex 0.149.1: 5 models listed by codex debug models: …` — or why it could not.
`kolk models --refresh` asks everyone.

Three rules came out of writing the tests. A vendor whose version changed is forgotten before its
fresh rows land: a model proved under one CLI is not proved under the next, and carrying the proof
forward would be the burned-in claim wearing a timestamp. A vendor that will not answer keeps its
last catalog and is reported, never blanked — yesterday's list with a warning beats no list. And a
recorded-but-disabled connector is not asked; the mutation for that guard only went red once the
fixture contained one, which is the difference between a test that passes and a test that proves.

## F4.5 — the seed ranks, the vendor decides (2026-09-02)

The question this leaf had to answer was what to do with a ladder. The engine's ceiling needs an
order — strongest first — and a vendor catalog gives names, not an order kolk can trust for
routing: Codex's `priority` puts `gpt-5.2` below `gpt-5.4-mini`, which says where the picker lists
them, not which is cheaper to run a commit on. So the seed ladder keeps the ranking and the vendor
catalog decides availability: a rung the vendor no longer lists is not offered however long the
ladder has named it, a model the vendor lists and the seed never heard of is selectable but not
descended to, and a vendor that has not been asked answers from its seed as before. `gpt-5.6-pro`
stops being a rung the day the vendor stops listing it; `gpt-5.5` and `ultra` arrive without a code
change; neither becomes a routing decision kolk cannot defend.

Then the ratchet earned its keep. With every surface reading the derived catalog, four exported
seed-only functions had no production caller left, and the dead-export test named all four in the
same run. They were deleted, not allowlisted: a seed row presented as resolvable is the burned-in
claim this whole phase exists to end, and an entry point that only tests can reach is how it would
have crept back.

## F4.6 — the surfaces say how they know (2026-09-02)

The catalog was right by F4.5; F4.6 is about what a person sees of it. Two decisions shaped the
rendering. Vendor models get their own section rather than joining the gateway list, because one is
a subscription already paid for and the other bills per token, and a list that blurs that costs
someone money. And a status is printed only when it changes what someone would do: `unverified`
says the gateway published this name and no turn has proved this login can use it, `gone` says the
vendor dropped it — while `listed` and `verified` stay quiet, because a column that says "fine" on
every row is decoration people learn to skip.

Every list of vendor rows now carries its provenance: which command or preview produced it, which
vendor version answered, and how long ago. That line is the difference between a catalog and a
claim, and it is the one thing a stale list cannot fake.

One mutation in this leaf would not apply — the sed pattern fought the escaped format string it was
aimed at — and the script reported that rather than passing. It was re-anchored on a plain line.
A mutation that silently fails to mutate is worse than no mutation, because it reports a guard as
proved.

## F4.7 — running it (2026-09-02)

A fresh clone passed the gate, which proves the tree; then the binary was run against the real
installed CLIs in an isolated config, which proved the feature. `kolk models --refresh` asked
codex 0.149.1 and previewed claude from the gateway, and the two things the owner asked for both
happened in the output: `gpt-5.6-pro`, a rung kolk has carried in its own source since 08-30, was
refused by name — *"codex 0.149.1 does not list gpt-5.6-pro"* — and `gpt-5.5`, which kolk's source
has never heard of, appeared on both Codex tiers with the vendor's efforts and context. The live
catalog also lists two models the bundled fixture written that morning hides. One afternoon, and
the vendor had already moved.

The run found what the unit tests had not: `--refresh` rendered the vendor sections before running
discovery, so it showed the old list and then said it had fetched a new one. That is the whole
phase's failure mode in miniature — a display that is confidently out of date — and it survived
six mutations and a full gate because nothing had driven the real command end to end.

Its test then had to be written twice. The first version called the two functions in the right
order itself and passed; mutating the fix left it passing, because it was testing the functions and
not the command. The second drives `runModels` over an httptest gateway, and the mutation fails as
it must. Both facts are in the dossier: a test that cannot fail is worse than no test, because the
report says "proved".

## `kolk version` became a prompt, and sent 74 turns (2026-09-02)

Closing the outside-session surface removed fifteen verbs. Dispatch treats an unrecognised first
word as a prompt — deliberately, so `kolk fix the failing test` works — and nothing was taught that
the removed names were different. So `kolk version` stopped printing a version and started sending
the word "version" to a model.

That would have been a slow annoyance if a person typed it. What made it expensive is that a gate
does: `scripts/check-budgets.sh` measures cold start by running `kolk version` twenty times, and
`make check` runs that gate. Against this machine's real configuration — the budget script does not
isolate HOME, on purpose, because it is measuring the real binary — each run opened a session on the
signed-in Codex subscription and took a turn. `~/.local/share/kolk/stats.jsonl` records 74 calls to
gpt-5.6-sol in code mode between 16:43 and 16:48. They carry no dollar cost because the connector is
a subscription; what they consumed was plan quota.

Nothing caught it for three `make check` runs, because a failing turn is fast and the budget only
fails when the measurement is slow. The run that finally failed did so at 31,679 ms against a 30 ms
budget — the gate reporting a number thirty-thousand times too large, which is a strange way to be
told you are spending someone's subscription.

Two fixes, and the first is the one that matters. A retired verb is now refused by name, for free,
with the spelling that works and a note that quoting still makes it a prompt: `kolk "version"` is one
argument and never equals a verb, so the design that makes `kolk fix the failing test` work is
untouched. The budget script now measures `kolk help`, which exists and is in the closed set, so it
cannot quietly stop existing again.

What this says about the removal: deleting a published command is not finished when the code
compiles and the tests pass. Every place that *types* the old name has to be found too — and a
script that types it twenty times an hour is worse than a user who types it once, because nobody
reads its output.

## Three decisions and the caller the note named (2026-09-03)

The owner answered the three questions F7 left. V34.3a keeps the code and changes the plan: the
session hold is advisory, so a platform without file locks still runs sessions, and two sessions on
one directory get a warning from `kolk sessions` rather than a refusal; the leaf is reworded and
ticked. `SagaRunner.Run`, the continuous loop nothing in the product called, is deleted; the four
tests that used it to prove the cost limit, the doom loop and the planner's memory now drive repeated
wakes, which is the only way the product ever runs a saga anyway.

The third was a question rather than a decision: which call had asked the Claude child for a gateway
model. One more Max wake with the new note active answered it in one line, before any chapter ran —
it was the saga planner. `AgentPlanner` deliberately runs on the fast lane, because choosing the next
chapter is a cheap judgement, and `FastLaneModel` returned the best discovered free gateway model
whenever the session model was not free, without asking whether the session's backend could run it.
On a plan session that backend is the vendor's child, which runs its own rungs and nothing else; it
answered as Fable, and the catalog recorded a Cohere id as a verified Claude model.

The fix is where the fast lane is chosen: on a session with a plan connector it is the roster's
cheapest signed-in rung, or the session model when nothing cheaper is. The first attempt keyed on the
ladder instead of the connector and broke two tests for the same Claude model reached through the
gateway — correctly, because that session's backend is the gateway and can run the free pick. The
connector the session already records is the honest discriminator.

## Mode survives a resume (2026-09-02)

Plan 06 §3 has said for a long time that `session.Mode` is written on switch and restored on resume.
It was not: the session file carried model and effort and nothing about mode, so every `kolk -r`
reopened in code, and the F7.2 transcript re-issued `/mode agent` on all seven wakes. The owner
asked for it to be built, and it is one field with one writer: `Agent.SetMode` records the mode the
moment it changes, for every surface that can change it, startup records what the run actually
runs in, and resume applies the same precedence effort uses — flag, then session, then the default.
Along the way, `newAgent` turned out to hand the engine the raw `--mode` flag rather than the mode it
had resolved, which the new resume test caught before the mutation did.

## v1.3.0 — the release that the closed surface had quietly broken (2026-09-02)

The v1.2.33 tag never became a release. Its verify job built all four archives, extracted the host
one, and ran `kolk version` on it — the verb the outside-session surface had just retired — and got
the retirement notice and exit 1. The public installer does the same thing to decide whether an
existing install is older than the latest, so every `curl | bash` after 1.2.33 would have found the
new binary "invalid" too. The local gates could not see either: the release verifier's fixture is a
fake `kolk` that answered `version`, and the installer test's fake answered anything.

The build identity line — `kolk v1.3.0 (commit, date) go1.27 linux/amd64`, the same formatter the
old verb used — is now a line of `kolk help`, which is in the closed set and cannot quietly stop
existing. The installer, the release rehearsal, and the release verifier read it there, and the
installer still tries `kolk version` afterwards for a binary older than 1.2.33. The fake `kolk` in
the verifier's fixture answers `help` now, so the gate matches the product.

The same commit's CI failed twice on `TempDir RemoveAll cleanup: directory not empty`, six minutes
after the identical code had passed: F4's start-time discovery keeps writing into the app's dirs in
the background, and a test that returns first races the cleanup. The test fixture joins that work
before the directory goes.

The capabilities page taught seven verbs that no longer exist outside a session and said nothing
about what F4–F7 built. It now says `/key`, `/update`, `/doctor` and the rest, states the closed
surface as a capability, and carries three new cards — every vendor mapped before it is offered, the
selected model as a ceiling with a printed lane, delegated children declaring their network — each
pinned by the site gate so the page cannot drift from the binary again.

## F7.3–F7.4 — a reviewer who had built none of it, and the leaves it earned (2026-09-02)

The reviewer was a fresh agent in its own worktree with one brief: restate each of F1–F3's
invariants, rerun the named tests, break a guard per phase and prove it went red, then look where the
tests do not. Fifteen mutations went red. Two did not go the way the record claimed, and one
invariant turned out not to hold at all.

The one that did not hold is the interesting one. F3.2 says `-e max` on Fable reaches the vendor
as `--effort max`, and its test proves that — at the adapter. Above the adapter sits `EffortForPlan`,
which folds Claude's `xhigh` into `max` so a downgrade is never reported between two spellings of the
same thing, and returns the first offered level at that rank. When F4 taught kolk to discover the
Claude catalog, every family row began to offer both `xhigh` and `max`, in that order, and from that
day `max` went to the vendor as `xhigh`. Nothing red, nothing logged; a later phase quietly moved a
dial the earlier phase had proven. An exact spelling now wins before any folding, and the test that
pins it offers both spellings.

The smaller two: `_ACCESS_KEY` was on the denylist but no sentinel pinned it — the one sentinel
with that suffix also contains `SECRET`, so dropping the suffix changed nothing the test could see,
and the F2.5 dossier's claim that it would was false. `MINIO_ACCESS_KEY` pins it now. And npm's
`//registry.npmjs.org/:_authToken` was not scrubbed, because `_AUTHTOKEN` is not `_TOKEN`; it is a
suffix now, with its own sentinel.

F7.4 then judged the seven V34 leaves the program named. One earned its tick: durable chapter
state, on F1's tests plus the live run's habit of committing `SAGA.md` inside every chapter commit.
Five stay open with the reason written beside each. One of those is worth the owner's eye:
V34.3a says lock acquisition errors are fatal, and `run.go` says, in a comment, that the session
hold is deliberately not fatal so a platform without file locks still runs sessions. Both cannot be
right; a tick would only have hidden the disagreement.

## F7.2 — the first live Fable saga, and what it took to get there (2026-09-02)

The plan's line for this point reads like a demo script: install, sign in, pick Fable, switch to
agent mode, run a saga, reset it. The first attempt died on the saga's first command, and that is
the point of running things for real.

Two things were wrong. Claude Code 2.1.258 answers a Bash command it will not run with a
`system/permission_denied` frame whose `message` is a plain string, and kolk's adapter had typed
that field as an object — so one denial ended the turn with a Go struct dump for an error. And the
denial itself should never have happened under `-P full-auto`: plan 04 §4.2 maps kolk's full-auto
onto the vendor's `bypassPermissions`, and nothing had ever built the mapping, so the child ran
`acceptEdits` with nobody there to approve, could edit files, and could run nothing. Both are fixed
in `8bb243f0`, both anchored — one by a real capture of the vendor's frames, the other by tests at
all three layers — and both guards go red under mutation.

The whole thing is captured verbatim in
[`docs/transcripts/f72-fable-saga-2026-09-02.txt`](transcripts/f72-fable-saga-2026-09-02.txt): a pty
with `TERM=dumb`, one line sent per prompt, colour stripped, nothing else touched. The decisive
exchanges, quoted from it.

The session opens, names the loss, and shows where it stands:

```text
full-auto on claude: the vendor child runs with bypassPermissions — kolk's hardline blocklist does not apply inside it, and the child keeps this mode until the session ends.
kolk — mode: code · effort: medium · model: claude-fable  (full-auto)
session: s_01M1HYTEP4PA4TPD084TN1P8YK
Type your request, or /help for commands. Ctrl+C interrupts a turn, /exit quits.

❯
```

`/mode agent` restarts the child and prints the lane:

```text
=== 21:01:26 send: /mode agent ===
/mode agent
mode: agent
claude restarted in agent mode (claude-fable (Claude Max, via the claude CLI))
agent lane: claude-fable → claude-opus → claude-sonnet → claude-haiku

❯
```

The saga's first wake, planning and running chapter 1 — with real commands this time:

```text
=== 21:01:26 send: Build a tiny Go CLI called tinytodo with add, list and done subcommands that store tasks in tasks.json, with unit tests and a README. /saga ===
Build a tiny Go CLI called tinytodo with add, list and done subcommands that store tasks in tasks.json, with unit tests and a README. /saga
chapter 1: Initialize the Go module and add a main.go with a subcommand dispatcher that prints usage for add, list and done.
◆ planning (claude-fable)…
◆ single-step task, running directly
kolk-agent I'll check the repo state, then init the module and write main.go.
· Bash: {"command":"ls -la && cat SAGA.md && go version","description":"Inspect repo and Go version"}
  → ok total 8

· Bash: {"command":"go mod init tinytodo 2>&1 && cat > main.go <<'EOF'\n// Command tinytodo is a tiny task m…
  → ok go: creating new go.mod: module tinytodo
Chapter 1 is done and verified.

**What changed**
- `go.mod` created with module name tinytodo, Go 1.27.
- `main.go` added with a `run(args) int` dispatcher. It handles add, list and done by printing their usage lines, prints full usage on help, and prints usage plus exits 1 on no command or an unknown command.

**Verification**
- `gofmt -l .` reported nothing, `go vet ./...` passed, `go build` succeeded.
- Smoke test: no args and an unknown command both exit 1 with usage. Each of add, list and done prints its usage line and exits 0.

The dispatcher is a plain function returning an exit code, so later chapters can unit test it without spawning a process. Next chapter should add the task model and tasks.json load and save.
  [agent · claude-fable-5-1 · 1150 tok · $0.1400 · 18013ms]
saga "Build a tiny Go CLI called tinytodo with add, list and done subcommands that store tasks in tasks.json, with unit tests and a README.": wake complete at chapter 1. Include /saga in your next request for the next chapter.

❯
```

The planner chose six chapters where the plan's author had imagined three; the artifact reached
`completed` on the seventh wake, which is what the reset rule in plan 10 waits for:

```text
saga "Build a tiny Go CLI called tinytodo with add, list and done subcommands that store tasks in tasks.json, with unit tests and a README.": every acceptance criterion is met. SAGA.md is finished; the next /saga request archives it and starts a new saga.

❯
```

And the reset — a new goal on a finished artifact archives it and starts over:

```text
saga "Build a tiny Go CLI called tinytodo with add, list and done subcommands that store tasks in tasks.json, with unit tests and a README." was completed; archived to SAGA.20260902-180126.md. starting a new saga "Add a --json flag to tinytodo list that prints the tasks as a JSON array, with a unit test.".
chapter 1: Add a --json flag to the list subcommand that prints the tasks as an indented JSON array, with a unit test.
◆ planning (claude-fable)…
◆ single-step task, running directly
kolk-agent I'll extend listTasks with a JSON mode, wire a --json flag in main.go, add tests, and update the README.
· Bash: {"command":"grep -n \"listTasks\\|list \" main.go commands.go commands_test.go README.md","descripti…
  → ok main.go:15:  list         list all tasks

· Bash: {"command":"python3 - <<'EOF'\nimport re\n# commands.go\np='commands.go'; s=open(p).read()\ns=s.repl…
  → ok commands_test.go
gofmt flagged commands_test.go (import ordering). Fixing that and rerunning.
· Bash: {"command":"gofmt -w commands_test.go && gofmt -l . && go vet ./... && go test ./... 2>&1 | tail -2 …
  → ok ok  	tinytodo	(cached)
Chapter 1 is done and verified.

**What changed**
- `commands.go` gives `listTasks` an asJSON parameter. When set, it writes the tasks as an indented JSON array with a trailing newline, and an empty store prints an empty array.
- `main.go` accepts an optional --json argument on list. Any other argument prints the list usage line and exits 1. The top-level usage text now shows the flag.
- `commands_test.go` adds tests that JSON output is an indented array that round-trips back to the same tasks, that an empty store yields an empty array, and that run rejects unknown list arguments. Existing list tests pass false for the new parameter.
- `README.md` documents the flag in the usage block and list section with example output.

**Verification**
- gofmt reports nothing after formatting the test file, `go vet` passes, and all tests pass under `go test ./...`.
- Smoke test in a temp dir: list --json printed an empty array before any adds, then the full indented array after adding and completing tasks. Plain list still prints text lines, and an unknown flag exits 1 with usage.
  [agent · claude-fable-5-1 · 2582 tok · $0.3357 · 36211ms]
saga "Add a --json flag to tinytodo list that prints the tasks as a JSON array, with a unit test.": wake complete at chapter 1. Include /saga in your next request for the next chapter.

❯
```

Two more things surfaced on the way and are recorded in the F7 dossier rather than fixed here in
full: a gateway model id turned up in the *claude* catalog as verified, with Fable's exact id beside
it, because the persistent child answers whatever it is asked on the model it was spawned with —
the recorder now refuses to verify a model the vendor does not list, and says so once, but which
call asked for that model is still open. And a resumed session restores model and effort but not
mode, so `/mode agent` had to be repeated every wake.

## F6 — four rules that had more than one implementation (2026-09-02)

Nothing here was broken, which is the point: these are the places where the next break would have
come from. Four checks on a directory, written three times with three wordings. Two REPLs, each with
a branch that called the shared prompt boundary and another that inlined what the boundary does,
under a copy of the same error block. A saga loop with two bodies that had already drifted. And a
subagent port that could not carry the execution envelope, which `openSubagentBackend` silently
preferred whenever a host set it — so reaching for the simpler name got you a child with no
workspace confinement and no network declaration, with nothing to tell you.

That last one is the reason a shim was the wrong answer. The plan allowed one; a port that cannot
carry the envelope cannot be confined, so keeping it under any name keeps the hole. It went, and
every subagent test grew a workspace as a result — which is the product's own invariant arriving in
the tests, not an accommodation.

The extraction paid for itself immediately. Folding the two saga loops into one `step` lost a
distinction the copies had held by accident: a *planner* that fails has produced no chapter, so
counting it as a chapter failure and continuing asks a broken planner the same question until the
doom threshold stops it. `TestAPlannerThatFailsStopsTheRun` went red on the first run with the
message "a failing planner reported success", which is a test earning its keep years after it was
written.

One thing worth saying about the vendor rule. `validateClaudeExecutionOptions` was a free function
named for one vendor, called from Claude's three constructors and nowhere else — which is exactly how
Codex ended up without the invariant. It is a table keyed on the envelope's provider now: Claude has
no network switch and is refused a network-disabled envelope, Codex has one, and the next provider is
a row rather than a call site someone has to remember.

## F5 — the work a turn was doing twice (2026-09-02)

Measure first was the rule, and it paid: the guess would have been "argv building is cheap, leave
it", and the measurement said 48 of the 54 allocations in building one Codex turn's argv were
re-validating an envelope the constructor had already validated — every turn, for every subagent.
The fix is a single unexported bool. It is trustworthy *because* it is unexported: an envelope built
outside the package cannot claim to be validated, so the check still runs for everyone who has not
earned the right to skip it. Six allocations remain, against fifty-four.

The Claude finding was plainer. `StreamChatObserved` built a full one-shot invocation at the top of
the function and then, on the persistent path — which is every ordinary session — returned without
ever using it. Fifty-three allocations and a full envelope validation per turn, for an argv nobody
ran. It is built below that return now.

Both tests prove the absence behaviourally rather than by counting: delete the workspace after
construction, and a turn that re-validates fails while a turn that trusts the constructor does not.
That is a better assertion than a benchmark threshold, which would be a number nobody could defend
on a different machine.

One thing was got wrong on the way. Teaching `executionOptionsEmpty` about the new field, I also
added `Efforts` to it — and an efforts-only envelope then looked *delegated*, which made the
session's own Codex invocation start overriding the user's `~/.codex/config.toml` network setting.
F2's test caught it in the same run, which is the argument for having written that test at all.

## TUI progress-log observability — C5 queued 2026-09-01

The requested Codex/Claude-style work log is recorded as a dedicated future checkpoint. It will
consume the existing lifecycle stream and render concise, ordered, bounded summaries for commands,
file edits, verification, checkpoint transitions, and subagents. Ephemeral spinner/status state will
remain separate from persistent completed entries, with stable semantic colors, narrow/no-color
fallbacks, ANSI/control-byte sanitization, secret redaction, and reload reconstruction. The
checkpoint explicitly covers concurrent ordering, start/finish replacement, aggregation, cancellation
and failure, `agent [i/n] — model — effort — summary`, and race/mutation tests. It does not change
execution scheduling or SAGA semantics and remains queued behind the inline SAGA routing/lifecycle
checkpoints. No implementation or provider turn was performed for C5.

## The sandbox has a switch before it has an enforcer — V34.1e.0 closed 2026-09-05

Plan 13 accepted OS-level sandboxing into v1 and left the mechanism to "select and prove under
V34.1e", which was referenced eight times in the ledger and defined nowhere. §7.2 now decides it —
Seatbelt on macOS, Landlock on Linux, one policy, fail closed — and this leaf ships the part every
enforcer will read: the policy type, the switch, and the refusal.

The refusal is a `Result`, not an error, and that was the one design choice worth arguing about. In
this tree `Run`'s error means "the turn was cancelled" and nothing else; a non-zero exit is a
successful Run with a failing Result, because the model should see a failure and react to it. A
sandbox that cannot be established is the same kind of fact: "I would not run this, here is what is
missing, here is the switch" belongs in the conversation, not in an abort. So the command does not
run, the exit code is -1, and the Failure text names the reason and `/sandbox off` verbatim.

The owner changed the default mid-leaf, from on to off, and the plan was amended in the same session
rather than left to drift: the sandbox is opt-in, `/sandbox on` turns it on for the session, and the
one rule that survives is that the state is always explicit — no `auto`, because a sandbox that
downgrades itself is one nobody notices. Default-off weakens the "yolo inside a sandbox" pairing, and
the mitigation is deliberately small: choosing `/full-auto` prints one suggestion, once, and never
throws the switch itself. It also does not mean offline. The sandbox confines writes; network stays
allowed for the user's own commands, so `go test` still fetches.

Red first, and the red was clean: three packages failed to build on exactly the missing symbols and
nothing else. Thirteen tests then went green, `internal/shell` under the race detector because the
mechanism probe is a package variable that tests override.

Two things were got right by the tree rather than by me. `NetworkDeny` was written and then removed
— nothing reads it until the network leaf, and an exported constant with no user is the kind of
promise `arch`'s dead-export check exists to refuse; it arrives with V34.1e.3. And `cmd_sandbox.go`
called `os.UserHomeDir` to build the credential denylist, and `arch` failed the build: home-directory
lookups have exactly one owner, `internal/paths`, so that "the engine touches no OS" is a property
and not a habit. `paths.UserHomeDir()` is the seam, and the fix was one line. The rule caught it in
under a second, which is the whole argument for having rules that are data enforced by Go instead of
conventions enforced by review.

Nothing public changes. The README's "no general execution sandbox", the capabilities rows and the
comparison cells all still say so, because nothing enforces anything yet; they flip in V34.1e.6, in
one commit, with the site pins that guard them. `/sandbox` does appear in `kolk help` and the README's
command list, and turning it on tells you truthfully that it cannot yet be established here.

## The sandbox refuses for real on macOS — V34.1e.1 closed 2026-09-05

Seatbelt, through `/usr/bin/sandbox-exec -p` with a profile rendered from the policy. The leaf was
built the way the contract asks and it mattered here more than usual, because the red had to be the
*right* red. A sandboxed command can be stopped two ways: kolk declining to run it at all, or the
kernel refusing it while it runs. Only the second is a sandbox. So the escape tests assert that the
command ran -- a real exit code -- and that the output says `Operation not permitted`. Before the
enforcer existed every one of them failed with "kolk declined to run the command instead of the
sandbox refusing it", which is exactly the failure that proves the test can tell the two apart.

Two properties of Seatbelt shaped the generator. Rules match on the real path, so every path is
resolved through its symlinks first: `/tmp` is `/private/tmp`, `t.TempDir()` is under
`/private/var/folders`, and a profile that names the unresolved path matches nothing and refuses
everything. And the last matching rule wins, so the credential denylist is written after the broad
allows -- which is what keeps `~/.ssh` refused when the root has been widened to the whole home
directory, the case test 4 exists for.

Test 8 changed the plan. `go test` on a trivial fixture inside the root failed, because Go writes its
build cache to `~/Library/Caches/go-build`, its module cache to `~/go/pkg/mod`, and its scratch to
`$TMPDIR`, none of which are the root. A sandbox that breaks `go test` is a sandbox nobody turns on.
Plan 13 §7.2 said writes are root and temp only; it now also says the toolchain caches, and the
policy carries them as `Writable` -- the user cache dir through a new `paths.UserCacheDir()` seam,
since `internal/paths` is the one owner of that lookup, plus `GOPATH` and `GOMODCACHE`, honouring the
environment when set and Go's defaults when not, because that is what the toolchain will do. `Run`
also sets the child's `TMPDIR` to the policy's temp. The profile goes inline rather than through the
0600 file the plan first described: it holds only paths, and a file would need a lifetime, a cleanup
and a race with the process that reads it.

The cross-package test seam the previous record worried about turned out to be unnecessary. The
`tools` test asked for a sandbox that could not be established by naming a root that does not exist,
which no enforcer can resolve. On macOS that refusal comes from the generator; elsewhere from the
probe; the model reads the same sentence either way.

Nothing public flips. On this machine `/sandbox on` now confines writes for real, and the README still
says there is no general execution sandbox. That is an under-claim, and it is deliberate: the
statements change together in V34.1e.6, with the site pins that guard them, once Linux is real too.

## Linux gets its child before it gets its rules — V34.1e.2a closed 2026-09-05

Leaf 2 was subdivided before a line of it was written, for a reason the machine made plain: there is
no Linux kernel here. No colima or lima instance, docker's daemon down, and Landlock is a kernel
feature. A leaf whose red cannot be observed is not a leaf, so 2a is everything that can be proven on
this Mac and cross-compiled for the rest, 2b is the ruleset and the escape tests, and 2c is running
them on a real kernel — which CI's ubuntu runner provides on a pull request, so no VM is needed.

`x/sys` v0.47 turned out to ship Landlock's constants and attr structs and none of the syscall
wrappers, so the calls are raw `unix.Syscall` on the sysnums, which are the same on every Linux
architecture. The probe asks the kernel its ABI and reports `landlock vN`; `ENOSYS` and `EOPNOTSUPP`
refuse with the kernel floor named.

Go has no pre-exec hook, so the confined child is kolk itself, re-executed in front of the command.
The plan said an internal `landlock-exec` verb. That would have been a fifth outside-session command
on a surface the same plan, the README, the site guard and two tests insist has exactly four. The
entry is gated on an environment variable instead, checked at the top of `main` before an app is
built, with the policy as JSON in a second variable — paths and a network word, nothing secret. The
child strips both before it execs, or a `kolk` run inside the sandbox would think it was the child.

The one decision worth defending is that the linux child *refuses*. It has no rules yet; the honest
options were to refuse, or to exec the command unconfined and call it progress. A child that quietly
runs unconfined is the exact silent downgrade §7.2 exists to forbid, so `applyLandlock` returns an
error and the exit code is 125 until 2b gives it a body.

Two gates earned their keep. The third-party allow-list refused `x/sys` in `internal/shell` until it
was added with a reason — which is the point of a budget rather than a convention — and the rot test
that deletes unused allowances accepted it, because the scanner parses linux-tagged files on any
host. Red was two packages failing to build on exactly the missing names; green on darwin under the
race detector, the Seatbelt tests intact, and the tree building for linux/amd64, linux/arm64 and
windows. That is as far as a Mac can take Landlock; the next step needs a kernel, and CI has one.

## Landlock confines for real, and the kernel taught two things a Mac could not — V34.1e.2b/2c closed 2026-09-05

The leaf ran on a pull request because there is no Linux kernel on the development machine and CI
has one. A tests-only commit went first, and ubuntu reported exactly the red the tests were built to
show: every escape test failed with "the confined child refused before running the command", which is
the child declining for want of rules — not a kernel refusal, not a compile error. Then the ruleset,
and it took two attempts, because Landlock differs from Seatbelt in two ways that only a running
kernel exposes.

The first is that there is no deny. Seatbelt lets a later `(deny …)` win over an earlier allow, so
"everything under the home except `.ssh`" is one line. Landlock can only grant, and a rule on a
directory grants everything beneath it. The read grant already coped: walk the tree, grant siblings
whole, descend only along the ancestors of a denied path. The write grant did not — it was one rule
on the root — and test 4, which widens the root to the whole home, showed `~/.ssh` writable. The walk
is now `grantTree` and serves both access sets. Tests 4 and 9 pin the two halves.

The second is what "re-execute yourself" means under `go test`. The confined child is kolk itself,
and in a test the running program is the test binary. `internal/shell` had the `TestMain` that hands
the child entry `os.Args` before the framework sees them; `internal/tools` did not. Its refusal test
asked for a sandbox with a root that does not exist. On darwin the profile generator refused in the
parent. On linux the parent only encoded the policy, forked, and the child — the `tools` test binary —
ignored the environment it did not know to check and ran the whole suite again, which ran the test
again, twelve levels deep, until timeouts stopped it. Two fixes, both kept: `prepareSandbox`
validates root and temp in the parent exactly as Seatbelt does, so a doomed policy never forks; and
`internal/tools` intercepts the re-exec too, because a future test there could sandbox a real
command and the recursion would be back.

Lint added a third lesson, cheaper than the other two. golangci on darwin does not analyse
linux-tagged files, so four findings — `%v` on an `errno` where `%w` belongs, two capitalised error
strings, two unchecked `unix.Close` — reached CI first. `GOOS=linux golangci-lint run` from the Mac
finds all four; it is in the loop now, before the push rather than after.

Landlock is real on Linux and Seatbelt is real on macOS. Nothing public says so yet. That flips in
V34.1e.6, together, with the pins that guard each statement.

## The network deny is enforced, or refused — V34.1e.3 closed 2026-09-05

One word in the policy, one line in each enforcer, and a refusal where the kernel cannot keep the
promise. Red first on both runners: a TCP connect under `network = deny` printed `connected`, and a
simulated Landlock ABI 3 asked for a deny handed back no error. Then the two lines.

Seatbelt's is `(deny network*)`. A probe earlier settled two things the test needed to know: a shell
still starts under it, and `curl` fails silently — exit 7, not a word — so the escape test connects
through bash's `/dev/tcp`, which prints the kernel's own phrase. The test opens a real loopback
listener first, and a control test makes the same connect under `allow` and requires `connected`,
because a refusal against a closed port would prove nothing.

Landlock's line is `Access_net` on the ruleset. Its network rules are allow-only like its filesystem
rules, so handling connect and bind and then adding no port rule denies every TCP connect and bind —
which is precisely the policy. It exists only from ABI 4, Linux 6.7. Below that the parent refuses in
`prepareSandbox`, naming the floor and the two ways out, before any child is forked: the plan's rule
that a deny the kernel cannot enforce is refused, never approximated. The ABI probe went behind a
variable so a Mac can stand in for a kernel it does not have, and the refusal has a test.

Guard rails failed on the red commit, and correctly: `NetworkDeny` was reachable only from tests,
which is the exact reason it was deferred out of leaf 0. The green commit reads it in both enforcers
and the gate passed. What this leaf does not do is decide *when* the user's own commands get a deny.
§7.1 governs delegated children; in-process subagents inherit the parent's policy. Giving a task
kind a deny is a design question for the owner, and it is written down as one rather than slipped in.

## The sandbox says what it is doing — V34.1e.4 closed 2026-09-05

Three places the user already looks, each told the truth in one line. A sandboxed command that fails
with the kernel's own phrase — `Operation not permitted`, `Permission denied` — gets exactly one
bounded line under its exit error: what is confined, whether the network is allowed, and the switch.
It does not say the sandbox caused the failure, because it does not know that; the phrase is a strong
hint and an overstated line is worse than none. Success, an ordinary non-zero exit, kolk's own
refusal and a timeout get nothing.

`/doctor` reports what would enforce a policy here — Seatbelt, Landlock with its ABI, or why nothing
can — and whether a network deny could be kept on this machine, so a pasted report explains why
`/sandbox on` did or did not take. The status line carries a `sandbox` row: `off` is a word there,
never a blank, since the one rule that survived the switch to opt-in is that the state is always
visible. The row also has a third value the plan never named: `on, unenforced`, for a policy set
where nothing can enforce it. In that state every command refuses, and the user should see why
before the first refusal rather than after.

The leaf is platform-neutral, so it closed on main without a PR: the linux-tagged files were vetted
and linted from the Mac with `GOOS=linux`, as leaf 2 taught. Red was observed the ordinary way — a
test asked the status for a field it did not have. Public claims are still unchanged; V34.1e.5
measures the wrapper's cost, and only V34.1e.6 flips anything.

## The wrapper costs two to seven milliseconds — V34.1e.5 closed 2026-09-05

A sandbox that made every `ls` feel slow would be a sandbox people switch off, so the leaf was a
measurement first. The shell package now times a bare `true` against a sandboxed one, p50 of
twenty-one runs, and holds the difference to the same two lines the binary's cold start is held to:
twenty milliseconds earns a warning, thirty fails the test. The budgets script lifts the line into
its log beside cold start from the one verbose run it already makes, and treats a missing line as an
error rather than silence.

The numbers: about six milliseconds on this Mac, where `sandbox-exec` compiles a profile per command,
and two on the ubuntu runner, where kolk re-executes itself and installs a Landlock ruleset before
handing over to the shell. The plan had guessed ten to thirty. It is corrected to what was measured.

The second half was a property, not a number. The cancel ladder kills a process group so that
`npm test &` does not outlive a cancelled turn; the question was whether a wrapper in front of the
shell breaks that. It does not, and the reason is worth pinning: both enforcers exec the command in
place, so the wrapper *becomes* the shell and the group leader. Two twins of the grandchild test run
under a policy — a timeout and a cancellation — and both pass. To be sure they could fail, Setpgid
was switched off for a moment: both reported a grandchild surviving through the wrapper, which is the
exact regression a future forking wrapper would introduce. Switched back, both pass. Nothing public
has changed yet. V34.1e.6 is the flip, and it needs the owner.

## The flip — V34.1e.6 closed 2026-09-05

Six leaves said nothing in public. This one says it, everywhere at once, and only what the tests
can defend. The README's "no general execution sandbox" became: opt-in, off by default, `/sandbox
on`; Seatbelt on macOS, Landlock on Linux 5.13 or newer; writes confined to the project, temp and the
toolchain caches; credentials unreadable even inside a widened root; a network deny the kernel cannot
enforce refused rather than approximated. The capabilities card moved from "Designed, not shipped" to
"Available now" and carries the escape-test count and the measured cost. `llms.txt` follows.

The comparison pages were the delicate part, because a flipped claim about kolk changes a sentence
about someone else. Codex sandboxes by default with three modes; kolk sandboxes on request with one,
so the card now names that residual gap instead of a capability gap. Claude Code's sandboxed Bash tool
was verified today from its own documentation — Seatbelt on macOS, bubblewrap on Linux and WSL2,
network isolation by allowed domain — and the card says both sandbox the bash tool and that Claude
Code's network rules are finer. Its default state could not be read from the page and is not claimed.

The site guard learned an inverse pin. `contains` proves the new sentence is present; `not_contains`
proves the old one is gone, in every page that carried it, so a stale copy nobody re-read cannot
outlive the flip. V34.1e is closed: one policy, two enforcers, fail closed, off until asked.
