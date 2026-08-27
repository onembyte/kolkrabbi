# Security policy

## Reporting a vulnerability

Report privately through GitHub's
[**Report a vulnerability**](https://github.com/onembyte/kolkrabbi/security/advisories/new) form.
Please do **not** open a public issue for anything exploitable.

Include what you did, what happened, and the version (`kolk version`). A proof of concept helps.
This is a solo project, so expect a first response in days rather than hours.

## What kolk touches on your machine

Knowing this makes a report easier to judge:

- **Credentials.** `kolk key` stores provider API keys in a `0600` file under your user data
  directory. Keys are never written to sessions, stats, logs or the event bus, and the
  `Authorization` header is constructed only inside the transport. See
  [`docs/plan/05-auth-keys-secrets.md`](docs/plan/05-auth-keys-secrets.md).
- **Subscription backends.** When you use a vendor CLI backend, kolk spawns *your own*
  already-logged-in binary and never reads, stores or proxies its credentials.
  See [`docs/plan/04-subscription-backends.md`](docs/plan/04-subscription-backends.md).
- **Shell and file tools.** The agent can read and write files and run commands, gated by the
  permission tier. `full-auto` removes those prompts — treat it as you would `curl | sh`.
- **Network.** kolk talks to the model provider you configured. Telemetry is not collected; the
  local dashboard never leaves loopback.

## Scope

In scope: credential disclosure, permission-gate bypass, code execution through untrusted model
or tool output, the release/installer supply chain.

Out of scope: a model producing a bad suggestion, and anything requiring an attacker who already
has your user account.

## Verifying a download

Releases ship `checksums.txt`; `site/install.sh` verifies the SHA-256 before installing.
