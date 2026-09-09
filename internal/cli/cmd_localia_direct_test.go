package cli

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/config"
	"github.com/onembyte/kolkrabbi/internal/local"
)

// `direct` takes the user's own runner command, runs it, then uses what it
// served: the command's last argument is the model, and the runner that
// lists it is where the session is pointed.
func TestLocaliaDirectRunsTheCommandThenUsesTheModel(t *testing.T) {
	a, out, _ := newTestApp(t, "")
	var ran []string
	a.runRunner = func(_ context.Context, w io.Writer, args []string) error {
		ran = args
		_, _ = io.WriteString(w, "pulled it\n")
		return nil
	}
	stubIdentify(a, map[string]local.Runtime{
		"127.0.0.1:12434": {Kind: local.KindOpenAI, Base: "http://127.0.0.1:12434/engines/v1",
			Models: []string{"hf.co/orcarouter/Qwen3.8-27B-Uncensored"}},
	})

	err := a.runLocalia(context.Background(), []string{"direct", "--yes", "docker", "model", "run", "hf.co/orcarouter/Qwen3.8-27B-Uncensored"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(ran, " ") != "docker model run hf.co/orcarouter/Qwen3.8-27B-Uncensored" {
		t.Fatalf("ran %q", ran)
	}
	got := out.String()
	for _, want := range []string{"pulled it", "hf.co/orcarouter/Qwen3.8-27B-Uncensored"} {
		if !strings.Contains(got, want) {
			t.Errorf("direct did not report %q:\n%s", want, got)
		}
	}
	// The runner it found is remembered, so the next session reaches it
	// without running the command again.
	saved := endpointsOf(t, a)
	if len(saved) != 1 || saved[0].Kind != local.KindOpenAI || saved[0].Base != "http://127.0.0.1:12434/engines/v1" {
		t.Fatalf("saved = %+v", saved)
	}
	if !strings.Contains(got, saved[0].Name+"/hf.co/orcarouter/Qwen3.8-27B-Uncensored") {
		t.Errorf("direct did not say the id to use:\n%s", got)
	}
}

// A command that fails is reported and nothing is switched or saved: the
// model it was going to serve does not exist.
func TestLocaliaDirectReportsAFailedCommandAndSavesNothing(t *testing.T) {
	a, _, _ := newTestApp(t, "")
	a.runRunner = func(context.Context, io.Writer, []string) error { return errors.New("exit status 1") }
	stubIdentify(a, map[string]local.Runtime{})
	if err := a.runLocalia(context.Background(), []string{"direct", "--yes", "docker", "model", "run", "x"}); err == nil {
		t.Fatal("a failed runner command was reported as success")
	}
	if got := endpointsOf(t, a); len(got) != 0 {
		t.Fatalf("a failed command still saved %+v", got)
	}
}

// A command that ran but served nothing kolk can find says so plainly
// rather than pointing the session at a model that is not there.
func TestLocaliaDirectSaysWhenNoRunnerServesTheModel(t *testing.T) {
	a, _, _ := newTestApp(t, "")
	a.runRunner = func(context.Context, io.Writer, []string) error { return nil }
	stubIdentify(a, map[string]local.Runtime{
		"127.0.0.1:12434": {Kind: local.KindOpenAI, Base: "http://127.0.0.1:12434/engines/v1", Models: []string{"something-else"}},
	})
	err := a.runLocalia(context.Background(), []string{"direct", "--yes", "docker", "model", "run", "missing-model"})
	if err == nil || !strings.Contains(err.Error(), "missing-model") {
		t.Fatalf("err = %v, want one naming the model that was not served", err)
	}
}

// An Ollama that serves the model keeps the name it has always had, rather
// than gaining a second record for the same server.
func TestLocaliaDirectUsesTheOllamaNameWhenOllamaServesIt(t *testing.T) {
	a, out, _ := newTestApp(t, "")
	a.runRunner = func(context.Context, io.Writer, []string) error { return nil }
	stubIdentify(a, map[string]local.Runtime{
		"127.0.0.1:11434": {Kind: local.KindOllama, Base: "http://127.0.0.1:11434/v1", Models: []string{"qwen3:8b"}},
	})
	if err := a.runLocalia(context.Background(), []string{"direct", "--yes", "ollama", "run", "qwen3:8b"}); err != nil {
		t.Fatal(err)
	}
	if got := endpointsOf(t, a); len(got) != 0 {
		t.Fatalf("this machine's Ollama gained a record it does not need: %+v", got)
	}
	if !strings.Contains(out.String(), local.SidecarName+"/qwen3:8b") {
		t.Fatalf("direct did not name the ollama id:\n%s", out.String())
	}
}

// Without --yes and with no way to ask, the command is not run.
func TestLocaliaDirectAsksBeforeRunning(t *testing.T) {
	a, _, _ := newTestApp(t, "")
	a.terminalOwned = func() bool { return true }
	a.runRunner = func(context.Context, io.Writer, []string) error {
		t.Fatal("the command ran without being approved")
		return nil
	}
	if err := a.runLocalia(context.Background(), []string{"direct", "docker", "model", "run", "x"}); err == nil {
		t.Fatal("an unapproved command was accepted")
	}
	_ = config.Endpoint{}
}
