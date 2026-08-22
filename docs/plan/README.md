# docs/plan — hardened plan items

One file per item of `PLAN.md`, named `NN-slug.md` (e.g. `10-saga-loop.md`). A `/loop` hardening
session writes the file, then ticks the item in `PLAN.md` and adds a one-line decision summary.

## Template

```markdown
# NN. <Item title>

Status: hardened on YYYY-MM-DD · supersedes: — · PLAN.md item NN

## Decision (the short version)
One paragraph a new contributor can act on.

## Spec
The concrete design: commands/flags, config keys, data formats, state machines, UX transcripts.
Prefer tables and examples over prose. Mark anything "v0.x / later".

## Rationale
Why this and not the alternatives; what constraint or evidence drove it.

## Alternatives rejected
- Option — why not (1 line each).

## Risks & open questions
- Risk → mitigation. Anything still undecided goes here, not back into PLAN.md.

## Sources
- URLs / docs/research files used, with dates.
```

## Rules of thumb
- Resolve every "Decide" bullet of the PLAN.md item; if one truly can't be decided yet, say why
  and what would unblock it.
- Cite sources for external facts (vendor policies, API limits, library status) with dates — they
  change.
- Keep the prototype in mind: say what changes in `internal/…` and what is kept.
- A hardened item should let someone start implementing without asking questions.
