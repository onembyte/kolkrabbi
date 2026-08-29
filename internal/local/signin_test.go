package local

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func meServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/me" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server
}

// E6. The server says whether it is signed in, without kolk ever holding the
// credential: the key is the server's own, in its home directory.
func TestSignInReadsThePlanFromASignedInServer(t *testing.T) {
	server := meServer(t, http.StatusOK, `{"id":"u1","email":"x@example.invalid","name":"x","plan":"pro"}`)
	state := SignIn(context.Background(), addrOf(server))
	if !state.Known || !state.SignedIn || state.Plan != "pro" {
		t.Fatalf("state = %+v, want known, signed in, plan pro", state)
	}
}

func TestSignInCapturesTheURLFromASignedOutServer(t *testing.T) {
	server := meServer(t, http.StatusUnauthorized, `{"error":"unauthorized","signin_url":"https://ollama.com/connect?name=box&key=abc"}`)
	state := SignIn(context.Background(), addrOf(server))
	if !state.Known || state.SignedIn {
		t.Fatalf("state = %+v, want known and signed out", state)
	}
	if state.SignInURL != "https://ollama.com/connect?name=box&key=abc" {
		t.Fatalf("signin_url = %q", state.SignInURL)
	}
}

// Unreachable is not "signed out". A verifier that read a dead server as a
// revoked sign-in would un-verify a connector every time the machine slept.
func TestSignInIsUnknownWhenNothingAnswers(t *testing.T) {
	state := SignIn(context.Background(), "127.0.0.1:1")
	if state.Known {
		t.Fatalf("state = %+v, want unknown", state)
	}
}
