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
