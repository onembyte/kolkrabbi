package engine

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/enginetest"
	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/protocol"
)

// A rotation publishes the limit that caused it, once, with the action it
// took; a stop publishes once with "stop". A surface reading the bus learns
// the same facts as the transcript, in a shape it can act on.
func TestEveryLimitDecisionIsPublishedOnce(t *testing.T) {
	b := newTestBus(t)
	srv := enginetest.New(
		enginetest.Step{StatusCode: http.StatusTooManyRequests, ErrorBody: `{"error":{"message":"rate limited"}}`},
		enginetest.Step{Text: "answer"},
	)
	defer srv.Close()
	a := New(Options{
		Client: provider.NewCompatibleClient(srv.URL), Bus: b, Mode: ModeCode, Model: "vendor/one:free",
		FreeModels: []string{"vendor/one:free", "vendor/two:free"},
		Permission: PermissionFullAuto, Out: io.Discard, Sess: enginetest.NewFakeSession("s_test", "vendor/one:free"),
	})
	if err := a.RunTurn(context.Background(), "hello"); err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	var got []protocol.ProviderLimitData
	for _, env := range bReplay(t, b) {
		if env.Type != protocol.EventProviderLimit {
			continue
		}
		var data protocol.ProviderLimitData
		if err := json.Unmarshal(env.Data, &data); err != nil {
			t.Fatal(err)
		}
		got = append(got, data)
	}
	if len(got) != 1 || got[0].Action != "rotate" || got[0].Kind != "endpoint_capacity" || got[0].Model != "vendor/one:free" {
		t.Fatalf("limit events = %+v, want one rotate of vendor/one:free for endpoint capacity", got)
	}
}

// A run that stops on a limit says so once, and never twice.
func TestAStopOnALimitIsPublishedOnce(t *testing.T) {
	b := newTestBus(t)
	srv := enginetest.New(enginetest.Step{StatusCode: http.StatusPaymentRequired, ErrorBody: `{"error":{"message":"insufficient credits"}}`})
	defer srv.Close()
	a := New(Options{
		Client: provider.NewCompatibleClient(srv.URL), Bus: b, Mode: ModeCode, Model: "vendor/paid",
		OnSubscriptionLimit: OnLimitStop,
		Permission:          PermissionFullAuto, Out: io.Discard, Sess: enginetest.NewFakeSession("s_test", "vendor/paid"),
	})
	if err := a.RunTurn(context.Background(), "hello"); err == nil {
		t.Fatal("a 402 did not stop the run")
	}
	stops := 0
	for _, env := range bReplay(t, b) {
		if env.Type == protocol.EventProviderLimit {
			var data protocol.ProviderLimitData
			_ = json.Unmarshal(env.Data, &data)
			if data.Action == "stop" && data.Kind == "account_quota" {
				stops++
			}
		}
	}
	if stops != 1 {
		t.Fatalf("stop events = %d, want exactly one", stops)
	}
}
