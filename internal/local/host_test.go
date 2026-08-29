package local

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// ollamaLike answers the two requests discovery makes, the way a real server
// does: "Ollama is running" at the root, and a version object.
func ollamaLike(t *testing.T, version string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			_, _ = w.Write([]byte("Ollama is running"))
		case "/api/version":
			_, _ = w.Write([]byte(`{"version":"` + version + `"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func addrOf(server *httptest.Server) string { return strings.TrimPrefix(server.URL, "http://") }

func found(path string) func(string) (string, error) {
	return func(string) (string, error) { return path, nil }
}

func notFound(string) (string, error) { return "", &exec.Error{Name: "ollama", Err: exec.ErrNotFound} }

func TestDiscoverHostAdoptsARunningServer(t *testing.T) {
	server := ollamaLike(t, "0.33.1")
	host := DiscoverHost(context.Background(), HostDiscovery{Addr: addrOf(server), LookPath: found("/usr/bin/ollama")})
	if host.State != HostRunning {
		t.Fatalf("state = %v, want running", host.State)
	}
	if host.Version != "0.33.1" || host.Addr != addrOf(server) {
		t.Errorf("host = %+v, want the version and address it answered from", host)
	}
}

// The guard that matters. Something else may be listening on 11434 — a dev
// server, a proxy, an old experiment. Adopting it because it answered 200 would
// send every local turn to a stranger.
func TestDiscoverHostDoesNotAdoptAStranger(t *testing.T) {
	// A plausible stranger: plenty of services answer /api/version with a
	// version object. Only the root's "Ollama is running" tells them apart.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/version" {
			_, _ = w.Write([]byte(`{"version":"1.0.0"}`))
			return
		}
		_, _ = w.Write([]byte("hello from something else"))
	}))
	t.Cleanup(server.Close)
	host := DiscoverHost(context.Background(), HostDiscovery{Addr: addrOf(server), LookPath: found("/usr/bin/ollama")})
	if host.State == HostRunning {
		t.Fatal("a server that is not Ollama was adopted")
	}
	if host.State != HostInstalled {
		t.Fatalf("state = %v, want installed (the binary is there, nothing of ours is running)", host.State)
	}
}

func TestDiscoverHostFindsTheBinaryWhenNothingListens(t *testing.T) {
	host := DiscoverHost(context.Background(), HostDiscovery{Addr: "127.0.0.1:1", LookPath: found("/opt/ollama")})
	if host.State != HostInstalled || host.Binary != "/opt/ollama" {
		t.Fatalf("host = %+v, want installed at /opt/ollama", host)
	}
}

func TestDiscoverHostReportsAbsentWithAnInstallLine(t *testing.T) {
	host := DiscoverHost(context.Background(), HostDiscovery{Addr: "127.0.0.1:1", LookPath: notFound})
	if host.State != HostAbsent {
		t.Fatalf("state = %v, want absent", host.State)
	}
	hint := host.InstallHint()
	if !strings.Contains(hint, "ollama.com") {
		t.Errorf("install hint %q does not point at the vendor", hint)
	}
}

// Never OLLAMA_HOST. It may name another machine, or ollama.com itself, and a
// probe that followed it would adopt a server that is not on this box.
func TestDiscoverHostIgnoresOLLAMAHOST(t *testing.T) {
	server := ollamaLike(t, "9.9.9")
	t.Setenv("OLLAMA_HOST", server.URL)
	host := DiscoverHost(context.Background(), HostDiscovery{Addr: "127.0.0.1:1", LookPath: notFound})
	if host.State == HostRunning {
		t.Fatal("discovery followed OLLAMA_HOST to a server it was not told about")
	}
}

// A port that accepts and then says nothing must not hang startup.
func TestDiscoverHostIsBounded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(2 * time.Second)
	}))
	t.Cleanup(server.Close)
	start := time.Now()
	host := DiscoverHost(context.Background(), HostDiscovery{Addr: addrOf(server), LookPath: notFound})
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("discovery took %v against a silent port; it must give up in well under a second", elapsed)
	}
	if host.State == HostRunning {
		t.Fatal("a silent port was adopted")
	}
}
