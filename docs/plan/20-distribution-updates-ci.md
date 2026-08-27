# 20. Distribution, updates & CI

Status: hardened on 2026-08-27 · supersedes: — · PLAN.md item 20

## Decision (the short version)

Almost all of this item was built while other items were being hardened, so most of what follows is
a record of what exists rather than a design for what to write. The pipeline that is in the repo
today is: a tag pushes → the release workflow reruns the whole gate → GoReleaser builds four
archives → Cosign signs the checksum manifest keylessly → the release publishes → a verifier
downloads what was published and checks it from the outside.

Three questions were still open. They are answered here, and two of the answers are refusals:

1. **Homebrew, scoop, winget, AUR — not yet, and not because they are hard.** A package that lags
   the release is worse than no package.
2. **macOS notarization — not for the CLI.** `curl` does not quarantine what it downloads, so
   Gatekeeper never sees the binary the install script places. It becomes a real requirement the day
   item 19 ships something with an icon.
3. **A version check that runs on its own — never.** `kolk` learns that a new release exists at
   exactly one moment: when a person types `kolk update`.

The one thing that was genuinely missing is now built: a **weekly live smoke test** against the free
model the offline catalogue promises. Everything else in kolk's test suite runs offline by
construction, which is a good property and also a blind spot — a week of drift in OpenRouter's API
would have been discovered by a user, not by us.

## Spec

### 1. Install paths

| Path | State | Notes |
| --- | --- | --- |
| `curl \| sh` (`site/install.sh`) | built | detects OS/arch, refuses anything but macOS/Linux on amd64/arm64, follows the `releases/latest` redirect, verifies SHA-256 against `checksums.txt`, rejects archives containing unexpected paths, and installs to the first writable directory on `PATH` (or `KOLK_INSTALL_DIR`). 72 contract checks. |
| GitHub Releases | built | four archives — darwin/linux × amd64/arm64 — plus `checksums.txt`, signed with keyless Cosign. |
| `go install` | built | supported and defended: a guardrail fails CI if a `replace` directive ever appears in the root `go.mod`, because `go install …@latest` hard-refuses such a module. |
| Homebrew tap | **refused for now** | see below. |
| scoop / winget | **refused** | Windows is advisory until migration step 13; shipping a package for a platform that is not yet supported is a promise we cannot keep. |
| AUR | **refused for now** | an AUR package needs a maintainer who watches AUR. |

**Why no Homebrew tap yet.** It costs a second repository, a token with write access to it, and a
standing obligation. GoReleaser's `brews:` block makes the mechanical part a ten-line change, which
is exactly why it can wait: nothing is learned by doing it early, and a stale tap teaches users that
`brew upgrade` does not get them the current kolk. Revisit when install friction — not
discoverability — is what people complain about.

**Why no notarization.** The install script fetches a tarball with `curl`, which does not set
`com.apple.quarantine`; Gatekeeper therefore never evaluates the binary and notarization would
change nothing for the supported path. It is not free either: it needs a paid Apple Developer
account and a signing identity in CI. The one case it would help is a user who downloads the archive
from the Releases page **in a browser**, which does quarantine it — that user needs
`xattr -d com.apple.quarantine kolk` today, and the README says so. When item 19 ships a `.app` or a
`.pkg`, notarization stops being optional and is specified there.

### 2. Updates

`kolk update` downloads the current release for the running platform, fetches `checksums.txt`,
verifies the archive's SHA-256, and replaces the running executable atomically. It is available as a
command and from inside a session.

**Nothing checks for updates on its own.** There is no background poll, no startup nudge, no
"a new version is available" line, and no opt-in flag to turn one on. This is a deliberate refusal
rather than an unbuilt feature: a version check is a phone-home by another name, it fires on a
schedule the user did not choose, and it leaks *when someone is working* to a third party. `kolk`
contacts a release server only when a person asks it to.

**What the two fast paths verify, and what they do not.** Both `install.sh` and `kolk update` verify
SHA-256 against `checksums.txt`. That catches a truncated or corrupted download and a single swapped
artifact — but the manifest is fetched from the same origin as the archive, so it is not evidence
against a compromised release origin. The signature is what would be, and neither fast path checks
it: verifying a Sigstore bundle means either shelling out to `cosign` (which users do not have) or
taking on the Sigstore module tree (which breaks the two-module gate that keeps this project
auditable).

The resolution is to be honest about it rather than to pretend the checksum is a signature.
`scripts/verify-release.sh` verifies the Cosign signature against the workflow identity for a given
tag, CI runs it against every published release, and anyone who wants signature-level assurance can
run it by hand. Revisit if signature verification ever becomes cheap in the standard library.

### 3. CI

| Job | What it guards |
| --- | --- |
| `test` (ubuntu + macos) | the suite, with `-race`. Windows is advisory until migration step 13. |
| `guardrails` | arch layering, purity, build tags, platforms, the mode surface, the site, the installer, the spec guard, `go.mod` tidiness and the no-`replace` rule, and the four workflow/release contracts. |
| `lint` | golangci-lint. |
| `budgets` | binary size (20 MB hard / 12 MB soft), cold-start p50 (30 ms hard / 20 ms soft), the test-count floor, and the two-module ceiling. These fail; they never warn. |
| `release` (tag only) | reruns `make check`, validates the tag, rehearses all four archives, publishes, then verifies what was published. |
| `smoke` (weekly) | one real turn against a real provider. New in this item. |

### 4. The weekly live smoke test

`.github/workflows/smoke.yml` runs Mondays at 07:00 UTC and on demand. It drives
`scripts/smoke.sh --real` — the same script a developer runs locally — against
`meta-llama/llama-3.3-70b-instruct:free`.

Four properties make it safe to have a live key in a workflow at all, and
`scripts/test-smoke-workflow.sh` (18 checks, in `make check`) fails the build if any of them erodes:

- **It never runs on a push.** Schedule and `workflow_dispatch` only. Quota is not spent per commit.
- **It is opt-in.** With no `OPENROUTER_API_KEY` secret configured the run emits a notice and
  succeeds. A repository without the secret is not a repository with a red build.
- **It cannot run from a fork.** `github.repository == 'onembyte/kolkrabbi'` gates the job, so a fork
  that inherits the file inherits nothing that runs.
- **It holds the key as tightly as it can.** Default `permissions: {}`, `contents: read` on the job,
  no `id-token`/`contents: write`, the secret referenced exactly once through an `env` mapping, and
  both actions pinned by digest — the same pins the release workflow uses, because this job and that
  one are the two that would matter if an action were backdoored.

**Why a free model, and why that one.** `smoke.sh --real` defaults to `openrouter/auto`, which bills.
The workflow pins the free model that `internal/provider/catalog.go` seeds as the offline fallback,
so the weekly run exercises exactly the model a keyless user meets first — and it supports tool
calling, which the smoke script needs. Because the id is spelled in YAML, nothing but a check stops
it drifting: the contract test extracts the `--model` argument, fails if it is not a `:free` id, and
fails if it is not present in the fallback catalogue.

## Build leaves

- **L20.1** — the weekly live smoke workflow, its 18-check contract (including the model-drift
  ratchet against the fallback catalogue), and its wiring into `make check` and the CI guardrails
  job. *Built.*
- **L20.2** — the README's install section, which documented only `go build` and documented it
  wrongly (`go build -o kolk .` — the root has no main package; it is `./cmd/kolk`). It now covers
  the install script, `go install`, source builds, `kolk update`, what the checksum does and does
  not prove, and the browser-download quarantine wrinkle. *Built.*
- Everything else in this item was already built: installer, releases, signing, updater, matrix,
  lint, budgets, release workflow and public verifier.

## Open questions

- A Homebrew tap, when install friction is the complaint.
- Notarization and a signing identity, when item 19 ships a bundled app.
- Signature verification in the fast paths, if it ever stops costing a dependency tree.
