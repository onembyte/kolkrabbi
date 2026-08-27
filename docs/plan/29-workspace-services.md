# 29. Workspace services — what the code you just wrote is doing

Status: hardened on 2026-08-27 · supersedes: — · PLAN.md item 29

## Decision (the short version)

Three things were on the table and they are wildly unequal. One is cheap and useful, one is a product,
and one has never been shown to change a decision.

1. **Port discovery ships.** A task starts a dev server; the port it chose is the one fact the user
   needs and the only one they cannot easily get. It costs a `/proc` read on Linux and a `lsof`
   fallback elsewhere.
2. **Supervision is refused.** Restart, stop, logs, health — that is a process manager, and a process
   manager is a product with its own failure modes, its own state to lose, and its own reasons to be
   running when Kolkrabbi is not.
3. **Resource telemetry is refused**, having failed the only test that mattered: nobody could name a
   decision it would change.

## Spec

### 1. Port discovery — the whole feature

When a `bash` tool call leaves something listening that was not listening before, say so:

```
  ◆ listening on http://127.0.0.1:5173 (started by this command)
```

| | Rule |
|---|---|
| When | After a `bash` call returns, compared against a snapshot taken before it. |
| How | `/proc/net/tcp` and `tcp6` on Linux, `lsof -iTCP -sTCP:LISTEN` elsewhere, both parsed as text. No module. |
| What | The address and port, and the URL when the port is loopback. Nothing about the process. |
| Cost | Two reads of a small file per bash call, and only when the call had a chance to start something. |
| Failure | Silent. A machine where this cannot be read still runs the agent; it just does not get the line. |

**Only loopback ports get a URL.** A service bound to `0.0.0.0` gets its port stated and no link,
because printing `http://192.168.1.5:5173` invites a click that publishes what the user may not have
meant to publish — the same reasoning as I26.5's reachability rules, applied to somebody else's
server.

**Nothing is opened, probed or requested.** Discovery reads a table the kernel already keeps. An HTTP
request to find out what a port is would be the agent making a network call nobody asked for.

### 2. Supervision — refused

`internal/shell.StartManagedProcess` exists and the saga's sidecar work uses it, so the mechanism is
there. That is not the objection.

The objection is what supervision *becomes*. "Restart it" needs to know how it was started; "show me
the logs" needs somewhere to have kept them; "is it healthy?" needs a definition of healthy per
service; and all three need to survive Kolkrabbi exiting, which means a daemon — the thing item 27
just refused for sessions, with better reasons available there than here.

A user who wants a supervised dev server has `systemd`, `foreman`, `docker compose`, or a second
terminal. Kolkrabbi telling them the port is the part none of those do.

### 3. Resource telemetry — refused, on the test it failed

"What does a session cost in CPU and memory" is interesting and was in the plan as a *maybe*. It is
cut, on a specific test: **name a decision it changes.** Cost per session already exists and changes
whether to keep going. Context usage already exists and changes whether to compact. CPU and memory
change nothing a user would do differently — and a number nobody acts on is a number that makes a
dashboard look busy while teaching its reader to ignore panels.

If it returns, it returns with a decision attached.

## Build leaves

- **I29.1 listening-port discovery** — a bash call that starts a listener says so, loopback links only.

## Open questions

- **Does the port line belong in the transcript or the status line?** The transcript is where it
  happened; the status line is where it still is. A dev server started ten turns ago is more useful in
  the second place, and needs the first mention either way.
- **Should a port that stops listening be noticed?** Symmetrical and tidy, and it means keeping the
  snapshot alive across turns rather than across one call — which is the first step towards the
  supervision this item just refused.
