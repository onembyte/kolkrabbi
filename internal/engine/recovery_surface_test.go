package engine

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/enginetest"
)

type recoveryNoticeWriter struct {
	bytes.Buffer
	notices []string
}

func (w *recoveryNoticeWriter) RecoveryWarning(message string) {
	w.notices = append(w.notices, message)
}

func TestRecoverySaveFailuresReachEverySurface(t *testing.T) {
	for _, reason := range []string{"pause", "limit", "error", "resume"} {
		t.Run(reason, func(t *testing.T) {
			out := &recoveryNoticeWriter{}
			b := newTestBus(t)
			a := New(Options{Sess: &reviewSession{FakeSession: enginetest.NewFakeSession("surface", "model"), fail: map[string]error{reason: errors.New("write /sessions/recovery: token=sk-ant-api03-abcdefghijklmnopqrstuvwxyz1234567890")}}, Out: out, Bus: b})
			defer a.Close()
			for range 2 {
				if err := a.saveRecovery(reason); err == nil {
					t.Fatal("expected save failure")
				}
			}
			events := bReplay(t, b)
			n := 0
			for _, e := range events {
				if string(e.Type) != "recovery.failed" {
					continue
				}
				n++
				var data struct {
					Code, Reason, Message string
					Durable               bool
				}
				if err := json.Unmarshal(e.Data, &data); err != nil {
					t.Fatal(err)
				}
				if data.Code != "recovery_save_failed" || data.Reason != reason || data.Durable || !strings.Contains(data.Message, "restart recovery is not guaranteed") {
					t.Errorf("wrong notice: %+v", data)
				}
			}
			if n != 2 || len(out.notices) != 2 {
				t.Errorf("events=%d sticky notices=%d, want 2 each", n, len(out.notices))
			}
			if strings.Count(out.String(), "restart recovery is not guaranteed") != 2 {
				t.Errorf("plain warning missing: %s", out.String())
			}
			if strings.Contains(out.String(), "abcdefghijklmnopqrstuvwxyz1234567890") {
				t.Error("plain output leaked a credential")
			}
		})
	}
}
func TestMissingRecoveryStorageStillNotifies(t *testing.T) {
	out := &recoveryNoticeWriter{}
	b := newTestBus(t)
	a := New(Options{Out: out, Bus: b})
	defer a.Close()
	if err := a.saveRecovery("error"); err == nil {
		t.Fatal("missing storage accepted")
	}
	if len(out.notices) != 1 || !strings.Contains(out.String(), "session storage is unavailable") {
		t.Fatal("missing storage was silent")
	}
}

func TestDurableRecoveryMirrorFailureIsStillVisible(t *testing.T) {
	out := &recoveryNoticeWriter{}
	b := newTestBus(t)
	sess := &reviewSession{FakeSession: enginetest.NewFakeSession("mirror", "model"), fail: map[string]error{"pause": durableMirrorError{}}}
	a := New(Options{Sess: sess, Out: out, Bus: b})
	defer a.Close()
	if err := a.saveRecovery("pause"); err != nil {
		t.Fatalf("durable mirror warning failed recovery: %v", err)
	}
	found := false
	for _, env := range bReplay(t, b) {
		if string(env.Type) != "recovery.failed" {
			continue
		}
		found = true
		var data struct {
			Durable bool
			Reason  string
		}
		if err := json.Unmarshal(env.Data, &data); err != nil {
			t.Fatal(err)
		}
		if !data.Durable || data.Reason != "pause" {
			t.Fatalf("lost durability fact: %+v", data)
		}
	}
	if !found || len(out.notices) != 1 || strings.Contains(out.String(), "restart recovery is not guaranteed") {
		t.Fatalf("mirror notice missing/wrong: %q", out.String())
	}
}
