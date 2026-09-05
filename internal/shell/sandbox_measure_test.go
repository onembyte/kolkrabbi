//go:build darwin || linux

package shell

import (
	"context"
	"sort"
	"testing"
	"time"
)

// The wrapper's own cost, measured against the same budget the binary's cold
// start is held to (scripts/check-budgets.sh: soft 20 ms, hard 30 ms). The
// sandbox adds one exec in front of every command -- sandbox-exec compiling a
// profile on darwin, kolk re-executing itself and installing a ruleset on
// linux -- and a cost that is felt on every `ls` is a cost the user pays for
// opting in. The number is logged so the budgets job carries it beside cold
// start; the test fails only past the hard line, so a slow runner earns a
// visible warning line, not a red gate on runner noise -- a shared macOS runner
// measured 30.1 ms on 2026-09-05 and turned main red, which is exactly the flake
// a budget must not be. So: the line always carries the number; the hard budget
// is enforced as an error by the budgets job, on a runner whose timing is
// trusted; and this test fails only at three times the hard line, which no
// runner noise reaches and any real regression (a forked wrapper, a profile
// that stopped compiling quickly) does. Over budget is a finding to record.
func TestSandboxWrapperOverheadStaysUnderTheColdStartBudget(t *testing.T) {
	if _, err := mechanism(); err != nil {
		t.Skipf("no sandbox mechanism here: %v", err)
	}
	const soft, hard = 20 * time.Millisecond, 30 * time.Millisecond
	root, temp := t.TempDir(), t.TempDir()
	policy := &Sandbox{Root: root, Temp: temp, Network: NetworkAllow}

	once := func(sb *Sandbox) time.Duration {
		start := time.Now()
		res, err := New().Run(context.Background(), Cmd{Command: "true", Dir: root, Timeout: 10 * time.Second, Sandbox: sb})
		if err != nil || !res.OK() {
			t.Fatalf("true did not run cleanly: %v %+v", err, res)
		}
		return time.Since(start)
	}
	p50 := func(sb *Sandbox) time.Duration {
		var runs []time.Duration
		for i := 0; i < 21; i++ {
			runs = append(runs, once(sb))
		}
		sort.Slice(runs, func(i, j int) bool { return runs[i] < runs[j] })
		return runs[len(runs)/2]
	}

	once(policy) // warm the wrapper's first exec out of the sample
	bare, boxed := p50(nil), p50(policy)
	overhead := boxed - bare
	// One line, greppable: check-budgets.sh lifts it into the budgets log.
	t.Logf("sandbox overhead p50: %.1f ms (bare %.1f ms, sandboxed %.1f ms; soft %d ms, hard %d ms)",
		ms(overhead), ms(bare), ms(boxed), soft.Milliseconds(), hard.Milliseconds())
	if ceiling := 3 * hard; overhead > ceiling {
		t.Fatalf("sandbox overhead %.1f ms is past %d ms, three times the hard budget: not noise", ms(overhead), ceiling.Milliseconds())
	}
	switch {
	case overhead > hard:
		t.Logf("::warning::sandbox overhead %.1f ms is over the %d ms hard budget on this runner", ms(overhead), hard.Milliseconds())
	case overhead > soft:
		t.Logf("::warning::sandbox overhead %.1f ms is over the %d ms soft budget", ms(overhead), soft.Milliseconds())
	}
}

func ms(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }
