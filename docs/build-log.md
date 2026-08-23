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
