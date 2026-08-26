# 26. Remote access — steer a session from another device

Status: hardened on 2026-08-26 · supersedes: — · PLAN.md item 26

## Decision (the short version)

Kolkrabbi is pinned to the terminal that started it. Unpinning it is worth doing, and the way it is
usually done — a relay service, native apps, an account system — is a second product. This item is
the version that fits a 7.9 MB static binary under a two-module dependency gate.

Four decisions carry the whole design:

1. **Kolkrabbi never runs a relay.** Reaching a machine over the internet is Tailscale's problem or
   SSH's, and both are already installed on the machines this matters for. Kolkrabbi's job is to
   *notice* it is reachable and print the right URL, not to route traffic. A relay would mean
   servers, uptime, and an account system, which is where "open source coding agent" quietly becomes
   "SaaS with a CLI".
2. **Tokens are per device, not per machine.** One shared secret means losing a phone costs everyone
   else their access. Each paired device gets its own token with a label and a last-seen time, and
   revoking one leaves the rest alone.
3. **Pairing is a short-lived code, not a QR.** A QR encoder is Reed-Solomon error correction we
   would have to write and test — the budget cannot buy a module for it, and 400 lines of maths is a
   poor trade for saving someone typing six digits. Deferred with that reason, not "later".
4. **Reading and steering are different permissions.** A device that can watch a session is not
   automatically a device that can answer its permission prompts. This falls out of item 13 rather
   than being invented here: a remote approval is still an approval, and the floor still holds.

## What exists today

`kolk serve` has an SSE event stream, a permission-resolve endpoint, bearer auth over every route
except `/` and `/v1/health`, and — until I26.1 — a hole where `--addr :8080` bound every interface
with no token. `kolk dash` is loopback-only, unauthenticated, and read-only.

Nothing generates a token, stores one, or knows what a device is. `--token` takes a string the user
invented, which in practice means an empty string.

## Spec

### 1. The token store

`<data>/devices.json`, 0600, one record per paired device:

```json
{"devices":[{"id":"dev_...","label":"Pixel 9","hash":"...","created":"...","lastSeen":"..."}]}
```

**The token itself is never stored** — only a hash of it, the way a password file works. A device
that loses its token pairs again; a device file that leaks does not hand over access. This costs
nothing and is the difference between a stolen file being an inconvenience and being a compromise.

It lives in Data rather than Config for the reason `CredentialsFile` already documents: a token is
state, not a setting, and someone who symlinks their config directory into a public dotfiles
repository must not thereby publish it.

Revocation is deleting a record. `kolk devices` lists them, `kolk devices revoke <id>` removes one,
and both are also slash commands so a running session can revoke a device without stopping.

### 2. Pairing

Pairing is **armed deliberately and briefly**. It is not a route that is always live.

| | Rule |
|---|---|
| Arming | `kolk pair` (or `/pair`) prints a six-digit code and the URL to visit. |
| Lifetime | Two minutes, or until used once. Whichever comes first. |
| Attempts | Five wrong codes disarms pairing entirely and says so. |
| Comparison | Constant time. A six-digit code is small enough that timing matters. |
| Result | A new device record and its token, returned once and never again. |
| Route | `POST /v1/pair` is open **only while armed**, and 404s otherwise. |

That last row is the awkward one and it is deliberate. I26.2 ratcheted the open-route set to exactly
`/` and `/v1/health`, with a test that fails if anyone widens it. Pairing has to be reachable without
a credential — that is what pairing *is* — so it does not join that map. It is a route that exists
only while a human has armed it, which is a different and much smaller claim than "open".

Five attempts, two minutes, six digits: an attacker gets five guesses at one of a million codes
inside a window a person is watching. The cap is what makes the short code safe, not the code's
length.

### 3. Being reachable

`kolk serve` reports how it can be reached, because the common failure is not insecurity but
confusion — someone binds `0.0.0.0`, gets a token, and still cannot work out what URL to open.

- Loopback: say so, and say that nothing else can reach it.
- A Tailscale address (`100.64.0.0/10`, or an interface named `tailscale*`): print that URL first.
  Detection is `net.Interfaces()`, not a module.
- Any other non-loopback bind: print the addresses, and say plainly that anything on that network can
  reach the port.
- SSH: documented, not implemented. `ssh -L 8080:127.0.0.1:8080 host` is one line and needs nothing
  from Kolkrabbi.

### 4. What a remote device may do

Two tiers, because "can watch" and "can act" are different risks:

| | Read | Steer |
|---|---|---|
| Event stream | yes | yes |
| Session list, transcript, cost | yes | yes |
| Answer a permission prompt | **no** | yes |
| Send a turn, interrupt | **no** | yes |
| Change model, permission tier | **no** | yes |
| Anything the floor refuses | no | **no** |

A device is paired into one tier and can be moved between them. The default is **read**, because the
first thing anyone does with a new capability is try it, and the safe default for "I just paired my
phone on a train" is that it cannot approve a `rm -rf`.

The floor from item 13 is unchanged and unreachable from here. A remote approval is an approval: it
goes through the same `Judge`, and no tier or device grants what the hardline refuses.

### 5. The client

A server-rendered page from the existing `dash`, installable as a PWA, no JavaScript framework. The
constraint that made `dash` server-rendered with no `<script>` still applies; steering needs form
posts and an event stream, which is not a framework's worth of work.

**Native mobile apps are out.** Two app stores, two release trains, two languages, and a review
process between a fix and its users. Recorded as a refusal, not a deferral.

## Build leaves

- **I26.1 the bind floor** — done: a wildcard address is not loopback, refused before the socket opens.
- **I26.2 the protected surface, ratcheted** — done: the open-route set is two, and widening it fails.
- **I26.3 the device store** — per-device tokens, hashed at rest, list and revoke.
- **I26.4 pairing** — armed briefly, six digits, single use, attempt-capped, 404 when not armed.
- **I26.5 reachability** — `kolk serve` says how to reach it, Tailscale first.
- **I26.6 read and steer tiers** — a device's tier decides which endpoints answer it.
- **I26.7 the remote client** — the dash page, authenticated, able to steer.

## Open questions

- **Should pairing require the terminal to confirm the device after the code is right?** It closes
  the window further at the cost of needing the laptop at hand, which is the thing pairing exists to
  avoid. Leaning no; revisit if the attempt cap proves too weak.
- **Does a revoked device's live SSE stream get cut immediately?** Cheap to do at the next ping, and
  "revoked but still watching until it reconnects" is a surprising thing to have to explain.
