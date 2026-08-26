package cli

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestParseFlags(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want options
	}{
		{"empty", nil, options{}},
		{"bare prompt", []string{"fix", "the", "test"},
			options{prompt: "fix the test", rest: []string{"fix", "the", "test"}}},
		{"print flag", []string{"-p", "hello"}, options{prompt: "hello"}},
		{"long print", []string{"--print", "hello"}, options{prompt: "hello"}},
		{"short model", []string{"-m", "x/y"}, options{model: "x/y"}},
		{"long model", []string{"--model", "x/y"}, options{model: "x/y"}},
		{"equals form", []string{"--model=x/y"}, options{model: "x/y"}},
		{"booleans", []string{"-y", "-r"}, options{yolo: true, resume: true}},
		{"long booleans", []string{"--yolo", "--resume"}, options{yolo: true, resume: true}},
		{"mixed", []string{"-e", "ultra", "--mode", "code", "ship", "it"},
			options{effort: "ultra", mode: "code", prompt: "ship it", rest: []string{"ship", "it"}}},
		{"chat mode", []string{"--mode", "chat"}, options{mode: "chat"}},
		{"agent mode", []string{"--mode", "agent"}, options{mode: "agent"}},
		{"flag wins over positional", []string{"-p", "flagged", "positional"},
			options{prompt: "flagged", rest: []string{"positional"}}},
		// The only way to write a prompt that starts with a dash.
		{"double dash ends flags", []string{"--", "-m", "is", "not", "a", "flag"},
			options{prompt: "-m is not a flag", rest: []string{"-m", "is", "not", "a", "flag"}}},
		{"value may start with a dash via equals", []string{"--base-url=--weird"},
			options{baseURL: "--weird"}},
		{"session id", []string{"-s", "s_123"}, options{session: "s_123"}},
		{"trailing base url slash is kept here", []string{"--base-url", "http://x/"},
			options{baseURL: "http://x/"}}, // trimming is config.ResolveBaseURL's job
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := parseFlags(c.args)
			if err != nil {
				t.Fatalf("parseFlags(%q) = error %v", c.args, err)
			}
			if !reflect.DeepEqual(*got, c.want) {
				t.Errorf("parseFlags(%q)\n got %+v\nwant %+v", c.args, *got, c.want)
			}
		})
	}
}

func TestParseFlagsRejects(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"unknown short", []string{"-x"}},
		{"unknown long", []string{"--nope"}},
		{"typo'd flag is not silently a prompt", []string{"--mdoel", "gpt-4"}},
		{"missing value short", []string{"-m"}},
		{"missing value long", []string{"--model"}},
		{"value given to a boolean", []string{"--yolo=true"}},
		{"unknown flag in equals form", []string{"--nope=1"}},
		{"unknown mode", []string{"--mode", "delegate"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := parseFlags(c.args)
			if err == nil {
				t.Fatalf("parseFlags(%q) accepted a bad command line", c.args)
			}
			var ue *UsageError
			if !errors.As(err, &ue) {
				t.Errorf("parseFlags(%q) = %T, want *UsageError (exit 2)", c.args, err)
			}
		})
	}
}

func TestFlagTableIsWellFormed(t *testing.T) {
	longs, shorts := map[string]bool{}, map[string]bool{}
	for _, f := range flagTable {
		if f.long == "" || f.summary == "" || f.set == nil {
			t.Errorf("flag %+v is missing a long name, summary or setter", f)
		}
		if longs[f.long] {
			t.Errorf("duplicate long flag --%s", f.long)
		}
		longs[f.long] = true
		if f.short != "" {
			if shorts[f.short] {
				t.Errorf("duplicate short flag -%s", f.short)
			}
			shorts[f.short] = true
			if len(f.short) != 1 {
				t.Errorf("short flag %q must be one letter", f.short)
			}
		}
	}
}

// A flag must never accidentally collide with a command name, unless it is an
// intentional flag twin (model, mode, effort) defined in docs/plan/09-command-surface.md §1.2.
func TestFlagsAndCommandsDoNotCollide(t *testing.T) {
	twinFlags := map[string]bool{
		"model":  true,
		"mode":   true,
		"effort": true,
	}
	for _, f := range flagTable {
		if twinFlags[f.long] {
			continue
		}
		if c := lookupCommand(f.long); c != nil {
			t.Errorf("--%s collides with the %q command", f.long, c.name)
		}
	}
}

func TestExitCode(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, ExitOK},
		{"plain", errors.New("boom"), ExitError},
		{"usage", usagef("bad"), ExitUsage},
		{"budget", &BudgetError{Msg: "spent"}, ExitBudget},
		{"guided", guided("no key", "do this"), ExitError},
		{"canceled", context.Canceled, ExitInterrupt},
		{"wrapped canceled", errors.Join(errors.New("turn failed"), context.Canceled), ExitInterrupt},
		{"wrapped usage", errors.Join(errors.New("outer"), usagef("inner")), ExitUsage},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := exitCode(c.err); got != c.want {
				t.Errorf("exitCode(%v) = %d, want %d", c.err, got, c.want)
			}
		})
	}
}
