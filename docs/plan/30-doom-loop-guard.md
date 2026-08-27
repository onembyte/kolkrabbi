# 30. The doom-loop guard

Status: hardened on 2026-08-27 · supersedes: — · PLAN.md item 30

## Decision (the short version)

The item says "nothing at the *turn* level". That is very nearly true, and the exception matters
enough to record: `RunTurn` already ends a turn after **two consecutive empty completions**, which is
a loop guard — it catches a model that has stopped producing anything. What is missing is the loop
where the model produces plenty and none of it changes anything.

Four guards exist today, and the gap between them is precise:

| Guard | Where | Catches |
| --- | --- | --- |
| `MaxRoundsFor` | per turn | anything, eventually — 4/12/24/50 rounds by effort in code mode |
| Two empty completions | per turn | a model that has stopped answering |
| `StopDoomLoop`, threshold 3 | per saga | three chapters that failed in a row |
| 429 rotation | per request | a rate-limited free model |

`MaxRoundsFor` is a **ceiling, not a detector**, and the distinction is the whole item: at max effort
in code mode, a model repeating one useless call is stopped on round 51, having been paid for fifty
times. The saga's detector is the right shape but the wrong altitude — it counts failed *chapters*,
so a chapter that spends its entire budget spinning inside one turn never registers as a failure to
count.

**The rule adopted here: three consecutive tool calls with identical arguments *and* identical
results are not a fourth attempt, they are a loop.** Both halves are required, and that pair is the
answer to the item's hardest question.

## Spec

### 1. The trigger

A call is a repeat of its predecessor when **both**:

- the tool name and the **canonical** arguments match — the JSON re-serialized with sorted keys and
  no insignificant whitespace, because providers re-serialize the same call differently and a
  formatting difference is not a different intention; and
- the **result is byte-identical** to the previous result.

Three in a row trips the guard. The counter resets on any call that differs in either half.

**Only canonical re-serialization is normalized. Nothing else.** Not trimmed paths, not lower-cased
strings, not "similar" arguments. An edit whose `old_string` differs by one space is a different
edit, and merging it with its neighbour would make the guard fire on work that is progressing.
Over-normalizing turns a safety device into a source of false stops, which is how safety devices get
switched off.

**Fixed at three, and it does not scale with effort.** Effort buys more work, not more permission to
repeat the same work — a threshold that rose with effort would mean the larger the budget, the longer
kolk is willing to burn it achieving nothing, which is backwards. Three also matches the saga's
`DefaultDoomThreshold`, so the vocabulary and the configuration knob are one thing rather than two.

### 2. Why identical *results* is the load-bearing half

The item asks whether a repeat that succeeded should count, and answers with the right example:
re-reading one file three times is waste; re-running a failing test three times is the job.

Success is the wrong discriminator. A failing test that fails *differently* each run is progress — the
error is moving. A read that succeeds identically three times is waste even though every call
returned `ok`. **What separates progress from repetition is whether anything changed**, and the
observable form of "anything changed" is the result bytes.

This also disposes of the false positive the naive rule would produce. A model that runs a test,
edits a file, and runs the test again has not made three consecutive identical calls — the edit sits
between them. Only a model that runs the same command three times *with nothing in between* trips
the guard, and that model is not testing anything.

### 3. What happens on the third call

The call is **not executed**. What happens next depends on who is there to ask, which is item 13's
tier model applied unchanged:

| Tier | Response |
| --- | --- |
| `/ask`, `/auto-approve` | the user is asked: run it again, or stop the turn. Answering "run it" resets the counter |
| `/full-auto` | the turn ends with a doom-loop stop, logged with the tool, the arguments and the count |
| a subagent | the call is auto-denied and the tool returns an error naming the loop; a **second** trigger ends the child's turn |

**Why `/full-auto` aborts rather than asks.** Full-auto's contract is that it does not stop to ask —
there is nobody there. But "proceed anyway" is exactly the behaviour the guard exists to prevent, and
a guard that yields in the mode with the largest budget is decoration. Aborting is consistent with
the tier's other promise: it never asks, and it always says what it did. Item 13 established the same
shape for path confinement, where full-auto proceeds *and logs the reason* — here the safe action is
to stop, and it is logged the same way.

**Why a subagent gets a denial rather than a kill.** Item 13 auto-denies in children anything the
tier would ask about, and this is that. A denial with a reason gives the child one chance to do
something else, which is the outcome we actually want; a kill discards work that may be fine. The
second trigger ends the turn because a child that loops, is told it is looping, and loops again is
not going to recover.

**An injected system notice was considered and rejected as the primary response.** Telling the model
"you appear to be looping" costs a round trip, is advice a looping model is by construction bad at
taking, and leaves the call executed. It is worth including as the *text of the denial* — where it
arrives as a tool result the model must react to — not as a substitute for not running the call.

### 4. Scope, and what it refuses to be

**The counter resets per turn.** A session-scoped counter would flag a user who legitimately asks for
the same thing in two different turns, and the loop this item is about lives inside one turn by
definition.

**No "always allow this tool".** The item asks whether to offer it; the answer is no. "Always allow"
here would mean "always allow me to spend your budget achieving nothing", and the rule it would write
is tool-wide — disabling the guard for every future loop in the session in order to get past one
call. The escape is the one-time "run it again", which resets the counter and costs the user one
decision rather than the guard.

**The guard is not a permission rule and must not be expressible as one.** `allow bash(*)` does not
disable it. Item 13's rules answer "is this dangerous?"; this answers "is this futile?" — a different
question, from a different budget, and collapsing the two would let a reasonable permission rule
silently remove a spending guard.

**It never inspects semantics.** No similarity scoring, no embedding distance, no "these two edits
look alike". Byte equality of a canonical form is a rule a person can check by eye, and every
softening of it trades a rare catch for a class of false stops that nobody can predict.

## Build leaves

- **L30.1 the detector** — canonical `(tool, arguments)` plus result bytes, three in a row, reset on
  any difference and at turn start.
- **L30.2 the tier responses** — ask, full-auto abort with a logged reason, subagent auto-deny then
  stop on the second trigger.
- **L30.3 the stop reason plumbed to the surface** — the saga already has `StopDoomLoop`; a turn-level
  stop needs to say the same word in the same place.
- **L30.4 a test that the ceiling is no longer the detector** — a fixture repeating one call must stop
  at three rounds, not at `MaxRoundsFor`.

## Open questions

- Whether the threshold should be configurable per session, as the saga's already is. Probably yes,
  by the same key, once there is one detector rather than two.
