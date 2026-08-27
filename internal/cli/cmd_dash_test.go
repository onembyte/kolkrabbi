package cli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestDashRefusesANonLoopbackAddress(t *testing.T) {
	isolateConnectorState(t)
	for _, addr := range []string{"0.0.0.0:8080", "192.168.1.10:8080", ":8080", "example.com:80"} {
		a, _, errOut := newTestApp(t, "")
		if code := a.main(context.Background(), []string{"dash", "--addr", addr}); code == ExitOK {
			t.Fatalf("%s was accepted", addr)
		}
		// The refusal has to explain itself, or it reads as an arbitrary limit
		// someone will look for a flag to defeat.
		if !strings.Contains(errOut.String(), "loopback") {
			t.Fatalf("%s refused without a reason: %q", addr, errOut.String())
		}
	}
}

func TestDashAcceptsLoopbackSpellings(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:0", "localhost:0", "[::1]:0"} {
		if !dashAddrIsLoopback(addr) {
			t.Fatalf("%s should be loopback", addr)
		}
	}
}

func TestDashServesThePageFromTheUsageLog(t *testing.T) {
	dirs := isolateConnectorState(t)
	if err := dirs.EnsureData(); err != nil {
		t.Fatal(err)
	}
	body := `{"kind":"call","time":"2026-08-26T10:00:00Z","turn":"t1","model":"vendor/model","mode":"code","effort":"high","prompt_tokens":100,"cost":0.25}` + "\n"
	if err := os.WriteFile(dirs.StatsFile(), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	a, _, _ := newTestApp(t, "")

	server := httptest.NewServer(a.dashHandler(dirs.Data))
	defer server.Close()
	response, err := http.Get(server.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()

	page := make([]byte, 64*1024)
	n, _ := response.Body.Read(page)
	rendered := string(page[:n])
	if !strings.Contains(rendered, "vendor/model") || !strings.Contains(rendered, "$0.25") {
		t.Fatalf("page = %q", rendered)
	}
	if got := response.Header.Get("Content-Security-Policy"); !strings.Contains(got, "default-src 'none'") {
		t.Fatalf("csp = %q, want a page that is not allowed to load anything", got)
	}
	if got := response.Header.Get("Cache-Control"); got != "no-store" {
		t.Fatalf("cache-control = %q, want usage data left out of caches", got)
	}
}

func TestDashUnknownPathIsNotFound(t *testing.T) {
	dirs := isolateConnectorState(t)
	a, _, _ := newTestApp(t, "")
	server := httptest.NewServer(a.dashHandler(dirs.Data))
	defer server.Close()

	response, err := http.Get(server.URL + "/../etc/passwd")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for anything but the one page", response.StatusCode)
	}
}

func TestDashOnAnEmptyMachineStillServes(t *testing.T) {
	dirs := isolateConnectorState(t)
	a, _, _ := newTestApp(t, "")
	server := httptest.NewServer(a.dashHandler(dirs.Data))
	defer server.Close()

	response, err := http.Get(server.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d on a machine with no usage yet", response.StatusCode)
	}
}
