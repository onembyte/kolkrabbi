package local

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// modelList serves an OpenAI-compatible model list at one base path and
// nothing anywhere else, which is how every runtime but Ollama answers.
func modelList(t *testing.T, base string, ids ...string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != base+"/models" {
			http.NotFound(w, r)
			return
		}
		data := make([]map[string]string, 0, len(ids))
		for _, id := range ids {
			data = append(data, map[string]string{"id": id})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	t.Cleanup(server.Close)
	return server
}

// The probe says what is actually at an address rather than assuming one
// runtime: an Ollama by its own handshake, anything else by the model list
// it serves, at the root, under /v1, or under /engines/v1 where Docker's
// model runner puts it.
func TestIdentifyTellsTheRuntimesApart(t *testing.T) {
	ollama := ollamaLike(t, "0.5.7")
	found, err := Identify(context.Background(), strings.TrimPrefix(ollama.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	if found.Kind != KindOllama || found.Base != ollama.URL+"/v1" || found.Version != "0.5.7" {
		t.Fatalf("ollama identified as %+v", found)
	}

	for _, tc := range []struct {
		name string
		base string
	}{
		{"at the root", ""},
		{"under /v1", "/v1"},
		{"docker model runner", "/engines/v1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := modelList(t, tc.base, "qwen3-27b", "llama3")
			found, err := Identify(context.Background(), strings.TrimPrefix(server.URL, "http://"))
			if err != nil {
				t.Fatal(err)
			}
			if found.Kind != KindOpenAI || found.Base != server.URL+tc.base {
				t.Fatalf("identified as %+v, want an OpenAI-compatible runtime at %q", found, server.URL+tc.base)
			}
			if strings.Join(found.Models, ",") != "qwen3-27b,llama3" {
				t.Fatalf("models = %q", found.Models)
			}
		})
	}
}

// An address where nothing answers is refused, and says what was tried
// rather than leaving the user to guess.
func TestIdentifyRefusesWhatItCannotReach(t *testing.T) {
	quiet := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, nil)
	}))
	defer quiet.Close()
	_, err := Identify(context.Background(), strings.TrimPrefix(quiet.URL, "http://"))
	if err == nil {
		t.Fatal("a server that serves nothing was accepted")
	}
	for _, want := range []string{"/v1/models", "/engines/v1/models"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not say it tried %q: %v", want, err)
		}
	}
	if _, err := Identify(context.Background(), "127.0.0.1:1"); err == nil {
		t.Fatal("an address with nothing listening was accepted")
	}
}

// A runtime that demands a key is not what this is for, and says so: no
// key of the user's is ever sent to a local endpoint.
func TestIdentifyRefusesAnEndpointThatWantsAKey(t *testing.T) {
	guarded := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer guarded.Close()
	_, err := Identify(context.Background(), strings.TrimPrefix(guarded.URL, "http://"))
	if err == nil || !strings.Contains(err.Error(), "key") {
		t.Fatalf("a guarded endpoint = %v, want a refusal that names the key", err)
	}
}

// A slow endpoint does not hold the session: the probe has a deadline, and
// a cancelled context stops it at once.
func TestIdentifyKeepsItsDeadline(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer slow.Close()
	start := time.Now()
	if _, err := Identify(context.Background(), strings.TrimPrefix(slow.URL, "http://")); err == nil {
		t.Fatal("a server that never answers was accepted")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("the probe took %s; it has no useful deadline", elapsed)
	}
}

// An address may be written the way a person writes one: bare host:port, a
// scheme, or a scheme with the base path the runtime serves at.
func TestIdentifyAcceptsTheWaysAnAddressIsWritten(t *testing.T) {
	server := modelList(t, "/engines/v1", "qwen3")
	host := strings.TrimPrefix(server.URL, "http://")
	for _, written := range []string{host, "http://" + host, server.URL + "/engines/v1", "http://" + host + "/engines/v1/"} {
		found, err := Identify(context.Background(), written)
		if err != nil {
			t.Fatalf("%q: %v", written, err)
		}
		if found.Base != server.URL+"/engines/v1" {
			t.Fatalf("%q identified base %q", written, found.Base)
		}
	}
}

// An endpoint's name is its model-id prefix, so it has to be one plain word
// and cannot be one of the names the feature reserves for itself.
func TestEndpointNamesAreOneWordAndNotReserved(t *testing.T) {
	for _, good := range []string{"shop", "rig", "gpu-2", "a"} {
		if err := ValidEndpointName(good); err != nil {
			t.Errorf("%q refused: %v", good, err)
		}
	}
	for _, bad := range []string{"", "Shop", "two words", "has/slash", "ollama", "here", strings.Repeat("x", 40)} {
		if err := ValidEndpointName(bad); err == nil {
			t.Errorf("%q was accepted as a name", bad)
		}
	}
}
