package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/bus"
	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/enginetest"
	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/tui"
	"github.com/onembyte/kolkrabbi/internal/xid"
	"github.com/onembyte/kolkrabbi/protocol"
)

func TestStreamJSONIsPureFromResumeStartup(t *testing.T) {
	a, out, errOut := newTestApp(t, "")
	t.Setenv("OPENROUTER_API_KEY", "sk-or-v1-testkey123")
	srv := enginetest.New(enginetest.Step{Text: "answer"})
	defer srv.Close()
	err := a.runDefault(context.Background(), []string{"-p", "hello", "--resume", "--output-format", "stream-json", "--base-url", srv.URL, "--model", "mock/model", "--permission", "full-auto"})
	if err != nil {
		t.Fatalf("run: %v stderr=%s", err, errOut.String())
	}
	count := 0
	if err := protocol.DecodeStream(strings.NewReader(out.String()), protocol.StreamNDJSON, func(protocol.Envelope) error { count++; return nil }); err != nil {
		t.Fatalf("non-NDJSON startup: %v stdout=%q", err, out.String())
	}
	if count == 0 {
		t.Fatal("empty stream")
	}
	if !strings.Contains(errOut.String(), "no previous session") {
		t.Errorf("startup notice lost: %s", errOut.String())
	}
}

type surfaceFailSession struct{ *enginetest.FakeSession }

func (*surfaceFailSession) SaveRecovery(string) error {
	return errors.New("write /saved/session.resume.json.gz: no space left on device")
}

type surfaceErrorBackend struct{ calls int }

func (b *surfaceErrorBackend) StreamChat(context.Context, string, []provider.Message, []provider.Tool, func(string)) (provider.Message, provider.Meta, error) {
	b.calls++
	return provider.Message{}, provider.Meta{}, errors.New("provider stopped")
}
func surfaceAgent(t *testing.T, out io.Writer) (*engine.Agent, *surfaceErrorBackend) {
	t.Helper()
	b, err := bus.New(xid.New(xid.Session), bus.Options{})
	if err != nil {
		t.Fatal(err)
	}
	backend := &surfaceErrorBackend{}
	ag := engine.New(engine.Options{Backend: backend, Model: "mock/model", Mode: engine.ModeCode, Effort: engine.EffortMedium, Permission: engine.PermissionFullAuto, Sess: &surfaceFailSession{enginetest.NewFakeSession("surface", "mock/model")}, Root: t.TempDir(), Out: out, Bus: b})
	t.Cleanup(func() { _ = ag.Close(); _ = b.Close() })
	return ag, backend
}
func TestRecoveryFailureTravelsThroughStreamTurn(t *testing.T) {
	var out bytes.Buffer
	ag, _ := surfaceAgent(t, io.Discard)
	err := streamTurn(context.Background(), ag, "do something", &out)
	var lost *engine.RecoverySaveError
	if !errors.As(err, &lost) {
		t.Fatalf("lost typed failure: %v", err)
	}
	notices := 0
	terminal := 0
	if err := protocol.DecodeStream(bytes.NewReader(out.Bytes()), protocol.StreamNDJSON, func(env protocol.Envelope) error {
		if env.Type == protocol.EventRecoveryFailed {
			notices++
			var data protocol.RecoveryFailedData
			if err := json.Unmarshal(env.Data, &data); err != nil {
				return err
			}
			if data.Reason != "error" || data.Durable || !strings.Contains(data.Message, "/saved/session.resume.json.gz") {
				t.Errorf("bad recovery frame: %+v", data)
			}
		}
		if env.Type == protocol.EventTurnFinished {
			terminal++
		}
		return nil
	}); err != nil {
		t.Fatalf("invalid stdout: %v %q", err, out.String())
	}
	if notices != 1 || terminal != 1 {
		t.Fatalf("notices=%d terminal=%d", notices, terminal)
	}
}
func TestRecoveryFailureTravelsThroughRealTUIWriter(t *testing.T) {
	screen := tui.NewRuntime(tui.RuntimeOptions{Output: io.Discard})
	ag, _ := surfaceAgent(t, screen)
	if err := ag.RunTurn(context.Background(), "do something"); err == nil {
		t.Fatal("missing failure")
	}
	screen.SetStatus(tui.Status{Model: "another model", Lifecycle: "ready"})
	if got := screen.Snapshot().Status.RecoveryWarning; !strings.Contains(got, "restart recovery is not guaranteed") {
		t.Fatalf("TUI warning lost after turn: %q", got)
	}
}
func TestStreamTurnRefusesUnavailableJournal(t *testing.T) {
	for _, expired := range []bool{false, true} {
		t.Run(map[bool]string{false: "absent", true: "expired cursor"}[expired], func(t *testing.T) {
			ag, backend := surfaceAgent(t, io.Discard)
			if expired {
				journal, err := bus.New(xid.New(xid.Session), bus.Options{MaxEvents: 1})
				if err != nil {
					t.Fatal(err)
				}
				ag.Bus = journal
				t.Cleanup(func() { _ = journal.Close() })
				for range 2 {
					if _, err := journal.Publish(bus.Event{Turn: xid.New(xid.Turn), Type: protocol.EventMessageDelta, Data: json.RawMessage(`{"text":"old"}`)}); err != nil {
						t.Fatal(err)
					}
				}
			} else {
				_ = ag.Bus.Close()
				ag.Bus = nil
			}
			var out bytes.Buffer
			if err := streamTurn(context.Background(), ag, "work", &out); err == nil || backend.calls != 0 || out.Len() != 0 {
				t.Fatalf("admitted unobservable work: %v calls=%d output=%q", err, backend.calls, out.String())
			}
		})
	}
}
func TestStreamJSONAgentLaneUsesStderr(t *testing.T) {
	a, out, errOut := newTestApp(t, "")
	t.Setenv("OPENROUTER_API_KEY", "sk-or-v1-testkey123")
	dirs, err := a.resolve()
	if err != nil {
		t.Fatal(err)
	}
	store := provider.VendorCatalogs{Vendors: map[string]provider.VendorCatalog{"codex": {Vendor: "codex", Models: []provider.DiscoveredModel{{ID: "mock/model", Rank: 1, Status: provider.StatusListed}}}}}
	if err := provider.SaveVendorCatalogs(dirs.VendorCatalogFile(), store); err != nil {
		t.Fatal(err)
	}
	srv := enginetest.New(enginetest.Step{Text: `[{"title":"first","kind":"explain"},{"title":"second","kind":"explain"}]`}, enginetest.Step{Text: "first done"}, enginetest.Step{Text: "second done"}, enginetest.Step{Text: "answer"})
	defer srv.Close()
	err = a.runDefault(context.Background(), []string{"-p", "two things", "--mode", "agent", "--output-format", "stream-json", "--base-url", srv.URL, "--model", "mock/model", "--permission", "full-auto"})
	if err != nil {
		t.Fatalf("run: %v stderr=%s", err, errOut.String())
	}
	if err := protocol.DecodeStream(strings.NewReader(out.String()), protocol.StreamNDJSON, func(protocol.Envelope) error { return nil }); err != nil {
		t.Fatalf("agent lane corrupted stream: %v", err)
	}
	if !strings.Contains(errOut.String(), "agent lane:") {
		t.Fatalf("lane missing: %s", errOut.String())
	}
}

// Stall stdout until the error boundary. The bounded subscription overflows
// during the token burst, before the recovery notice exists.
type slowRecoveryOutput struct {
	bytes.Buffer
	started chan struct{}
	release <-chan struct{}
	once    sync.Once
}

func (w *slowRecoveryOutput) Write(p []byte) (int, error) {
	w.once.Do(func() { close(w.started); <-w.release })
	return w.Buffer.Write(p)
}

type burstFailureBackend struct{ started <-chan struct{} }

func (b burstFailureBackend) StreamChat(ctx context.Context, _ string, _ []provider.Message, _ []provider.Tool, onToken func(string)) (provider.Message, provider.Meta, error) {
	select {
	case <-b.started:
	case <-ctx.Done():
		return provider.Message{}, provider.Meta{}, ctx.Err()
	}
	for range 64 {
		onToken("a token ")
	}
	return provider.Message{}, provider.Meta{}, errors.New("provider failed after burst")
}

type releasingRecoverySession struct {
	*enginetest.FakeSession
	release chan struct{}
	once    sync.Once
}

func (s *releasingRecoverySession) SaveRecovery(string) error {
	s.once.Do(func() { close(s.release) })
	return errors.New("snapshot disk full")
}
func TestSlowStreamStillDeliversRecoveryFailure(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{})
	output := &slowRecoveryOutput{started: started, release: release}
	journal, err := bus.New(xid.New(xid.Session), bus.Options{SubscriberBuffer: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = journal.Close() }()
	ag := engine.New(engine.Options{Backend: burstFailureBackend{started: started}, Model: "mock/model", Mode: engine.ModeCode, Effort: engine.EffortMedium, Permission: engine.PermissionFullAuto, Sess: &releasingRecoverySession{FakeSession: enginetest.NewFakeSession("surface", "mock/model"), release: release}, Root: t.TempDir(), Bus: journal, Out: io.Discard})
	defer ag.Close()
	if err := streamTurn(context.Background(), ag, "work", output); err == nil {
		t.Fatal("expected failed save")
	}
	recovery, terminal := 0, 0
	var last uint64
	if err := protocol.DecodeStream(bytes.NewReader(output.Bytes()), protocol.StreamNDJSON, func(env protocol.Envelope) error {
		if env.Seq != last+1 {
			t.Errorf("lost/duplicated sequence: previous=%d got=%d", last, env.Seq)
		}
		last = env.Seq
		if env.Type == protocol.EventRecoveryFailed {
			recovery++
		}
		if env.Type == protocol.EventTurnFinished {
			terminal++
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if recovery != 1 || terminal != 1 {
		t.Fatalf("slow stdout lost notices: recovery=%d terminal=%d", recovery, terminal)
	}
}
