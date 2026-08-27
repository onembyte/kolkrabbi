package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/enginetest"
	"github.com/onembyte/kolkrabbi/internal/paths"
	"github.com/onembyte/kolkrabbi/protocol"
)

func TestSingleShotStreamJSONOutputsNDJSONEnvelopes(t *testing.T) {
	srv := enginetest.New(enginetest.Step{Text: "Streamed JSON response"})
	defer srv.Close()

	home := t.TempDir()
	t.Setenv("HOME", home)
	blockProviderAccess(t)
	t.Setenv("OPENROUTER_API_KEY", "sk-or-v1-testkey123")
	t.Setenv(paths.EnvConfigDir, filepath.Join(home, "config"))
	t.Setenv(paths.EnvDataDir, filepath.Join(home, "data"))
	t.Setenv(paths.EnvCacheDir, filepath.Join(home, "cache"))

	d, err := paths.Resolve()
	if err != nil {
		t.Fatalf("paths.Resolve: %v", err)
	}
	if err := d.EnsureConfig(); err != nil {
		t.Fatalf("EnsureConfig: %v", err)
	}

	var stdout, stderr bytes.Buffer
	app := &app{
		stdout: &stdout,
		stderr: &stderr,
		in:     nil,
	}

	args := []string{
		"-p", "echo something",
		"--output-format", "stream-json",
		"--base-url", srv.URL,
		"--model", "mock/model",
		"--permission", "full-auto",
	}

	if err := app.runDefault(context.Background(), args); err != nil {
		t.Fatalf("runDefault: %v (stderr: %s)", err, stderr.String())
	}

	output := stdout.String()
	if output == "" {
		t.Fatalf("expected NDJSON output on stdout, got empty (stderr: %s)", stderr.String())
	}

	var decoded []protocol.Envelope
	err = protocol.DecodeStream(strings.NewReader(output), protocol.StreamNDJSON, func(env protocol.Envelope) error {
		decoded = append(decoded, env)
		return nil
	})
	if err != nil {
		t.Fatalf("DecodeStream failed: %v (raw stdout: %q)", err, output)
	}

	if len(decoded) == 0 {
		t.Fatal("decoded 0 envelopes from stream-json output")
	}

	hasTurnStarted := false
	hasDelta := false
	hasTurnFinished := false
	for _, env := range decoded {
		if env.Type == protocol.EventTurnStarted {
			hasTurnStarted = true
		}
		if env.Type == protocol.EventMessageDelta {
			hasDelta = true
		}
		if env.Type == protocol.EventTurnFinished {
			hasTurnFinished = true
		}
	}

	if !hasTurnStarted || !hasDelta || !hasTurnFinished {
		t.Fatalf("missing expected event types in stream-json output: %+v", decoded)
	}

	// Verify spill file was created
	files, err := os.ReadDir(d.Sessions())
	if err != nil {
		t.Fatalf("ReadDir sessions: %v", err)
	}
	foundSpill := false
	for _, f := range files {
		if strings.HasSuffix(f.Name(), ".events.ndjson") {
			foundSpill = true
			content, _ := os.ReadFile(filepath.Join(d.Sessions(), f.Name()))
			if len(content) == 0 {
				t.Errorf("spill file %s is empty", f.Name())
			}
		}
	}
	if !foundSpill {
		t.Fatal("expected .events.ndjson spill file in sessions directory")
	}
}
