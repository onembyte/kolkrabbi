package serve

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"path/filepath"

	"github.com/onembyte/kolkrabbi/internal/devices"
)

type recordingStarter struct {
	prompts []string
	err     error
}

func (s *recordingStarter) StartTurn(prompt string) error {
	s.prompts = append(s.prompts, prompt)
	return s.err
}

func turnMux(t *testing.T, starter TurnStarter, store *devices.Store) http.Handler {
	t.Helper()
	handler, err := Mux(Options{Bus: testBus(t), Token: "operator-token", Turns: starter, Devices: store})
	if err != nil {
		t.Fatal(err)
	}
	return handler
}

func postTurn(t *testing.T, handler http.Handler, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/v1/turns", bytes.NewBufferString(body))
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

// Every route but the two that say nothing needs a credential (I26.2).
func TestStartingATurnNeedsACredential(t *testing.T) {
	handler := turnMux(t, &recordingStarter{}, nil)
	if got := postTurn(t, handler, "", `{"prompt":"go"}`).Code; got != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 for an unauthenticated turn", got)
	}
}

// Steering is the higher tier. A device paired on a train can watch; it cannot
// ask the session to do things (I26.6).
func TestAReadOnlyDeviceCannotStartATurn(t *testing.T) {
	store, err := devices.Load(filepath.Join(t.TempDir(), "devices.json"))
	if err != nil {
		t.Fatal(err)
	}
	_, readToken, err := store.Add("phone", devices.TierRead)
	if err != nil {
		t.Fatal(err)
	}
	_, steerToken, err := store.Add("laptop", devices.TierSteer)
	if err != nil {
		t.Fatal(err)
	}
	starter := &recordingStarter{}
	handler := turnMux(t, starter, store)

	if got := postTurn(t, handler, readToken, `{"prompt":"go"}`).Code; got != http.StatusForbidden {
		t.Errorf("status = %d, want 403 for a read-only device", got)
	}
	if len(starter.prompts) != 0 {
		t.Fatalf("a read-only device started a turn: %v", starter.prompts)
	}
	if got := postTurn(t, handler, steerToken, `{"prompt":"go"}`).Code; got != http.StatusAccepted {
		t.Errorf("status = %d, want 202 for a steering device", got)
	}
}

// The protocol's rules are the server's rules: an empty or oversized prompt is
// refused here for the same reason it is refused there.
func TestTheServerAppliesTheCommandsOwnRules(t *testing.T) {
	starter := &recordingStarter{}
	handler := turnMux(t, starter, nil)

	for name, body := range map[string]string{
		"empty":      `{"prompt":""}`,
		"whitespace": `{"prompt":"   "}`,
		"missing":    `{}`,
		"oversized":  `{"prompt":"` + strings.Repeat("x", 40*1024) + `"}`,
		"malformed":  `{"prompt":`,
	} {
		t.Run(name, func(t *testing.T) {
			if got := postTurn(t, handler, "operator-token", body).Code; got != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", got)
			}
		})
	}
	if len(starter.prompts) != 0 {
		t.Errorf("an invalid command reached the session: %v", starter.prompts)
	}
}

// kolk serve owns no agent. Saying so plainly is better than pretending, and
// far better than inventing a supervisor two items have refused.
func TestATurnIsRefusedWhenNothingIsAttached(t *testing.T) {
	handler := turnMux(t, nil, nil)
	response := postTurn(t, handler, "operator-token", `{"prompt":"go"}`)
	if response.Code != http.StatusNotImplemented {
		t.Errorf("status = %d, want 501 when no session is attached", response.Code)
	}
	if !strings.Contains(response.Body.String(), "session") {
		t.Errorf("the refusal does not say what is missing: %s", response.Body.String())
	}
}

func TestAnAcceptedTurnReachesTheSession(t *testing.T) {
	starter := &recordingStarter{}
	handler := turnMux(t, starter, nil)
	if got := postTurn(t, handler, "operator-token", `{"prompt":"run the tests"}`).Code; got != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", got)
	}
	if len(starter.prompts) != 1 || starter.prompts[0] != "run the tests" {
		t.Errorf("the session received %v, want one prompt \"run the tests\"", starter.prompts)
	}
}
