package provider_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

// A canary in the shape of an OpenRouter key, long enough for the scrubber's
// inference rule and never registered with it: a compatible endpoint registers
// nothing, so only the shape can save the user. Assembled at runtime rather
// than written as one literal, because GitHub's push protection reads a
// literal of this shape as a live key and refuses the push -- rightly, since
// it cannot tell a canary from a leak.
var canaryKey = "sk-or-v1-" + strings.Repeat("0123456789abcdef", 4)

// A token in the URL's username slot is how people smuggle credentials into a
// base URL. net/http strips the *password* from a transport error's URL and
// leaves the username, so the token comes back verbatim in the error -- which
// the CLI prints and the session keeps. Every error the client returns has to
// pass the scrubber first.
func TestTransportErrorsNeverEchoACredentialFromTheURL(t *testing.T) {
	client := provider.NewCompatibleClient("http://" + canaryKey + "@127.0.0.1:1")
	_, _, err := client.StreamChat(context.Background(), "m", nil, nil, func(string) {})
	if err == nil {
		t.Fatal("a connection to a closed port succeeded")
	}
	if strings.Contains(err.Error(), canaryKey) {
		t.Fatalf("StreamChat error echoes the credential: %v", err)
	}
	if _, err = client.ListModels(context.Background()); err == nil {
		t.Fatal("a connection to a closed port succeeded")
	} else if strings.Contains(err.Error(), canaryKey) {
		t.Fatalf("ListModels error echoes the credential: %v", err)
	}
}

// The server's own error text inside the stream is the server's to write, and
// a misconfigured proxy will happily quote the key it rejected.
func TestStreamErrorChunksAreScrubbed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(`data: {"error":{"message":"invalid key ` + canaryKey + `"}}` + "\n\n"))
	}))
	defer srv.Close()
	client := provider.NewCompatibleClient(srv.URL)
	_, _, err := client.StreamChat(context.Background(), "m", nil, nil, func(string) {})
	if err == nil {
		t.Fatal("an error chunk did not become an error")
	}
	if strings.Contains(err.Error(), canaryKey) {
		t.Fatalf("stream error echoes the credential: %v", err)
	}
	if !strings.Contains(err.Error(), "invalid key") {
		t.Fatalf("the scrub destroyed the message, not just the secret: %v", err)
	}
}
