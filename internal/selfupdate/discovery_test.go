package selfupdate

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOfficialReleaseOriginCannotDrift(t *testing.T) {
	const want = "https://github.com/onembyte/kolkrabbi/releases"
	if officialReleasesURL != want {
		t.Fatalf("official release origin = %q, want %q", officialReleasesURL, want)
	}
}

func TestDiscoverLatestStableReleaseFromExactRedirect(t *testing.T) {
	var base string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			t.Errorf("method = %s, want HEAD", r.Method)
		}
		switch r.URL.Path {
		case "/releases/latest":
			http.Redirect(w, r, base+"/releases/tag/v1.12.3", http.StatusFound)
		case "/releases/tag/v1.12.3":
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	base = srv.URL

	got, err := discoverLatest(context.Background(), srv.Client(), base+"/releases")
	if err != nil || got.String() != "1.12.3" {
		t.Fatalf("discoverLatest = (%q, %v), want 1.12.3", got, err)
	}
}

func TestDiscoverLatestRejectsUnexpectedDestinations(t *testing.T) {
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer other.Close()

	for _, destination := range []string{
		"/releases/latest",
		"/releases/tag/v1.2.3-beta",
		"/releases/tag/v1.2.3/extra",
		"/other/tag/v1.2.3",
		"/releases/tag/v01.2.3",
		"/releases/tag/v1.2.3?asset=other",
		other.URL + "/releases/tag/v1.2.3",
	} {
		t.Run(strings.ReplaceAll(destination, "/", "_"), func(t *testing.T) {
			var base string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/releases/latest" && destination != "/releases/latest" {
					target := destination
					if strings.HasPrefix(target, "/") {
						target = base + target
					}
					http.Redirect(w, r, target, http.StatusFound)
					return
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer srv.Close()
			base = srv.URL

			if _, err := discoverLatest(context.Background(), srv.Client(), base+"/releases"); err == nil {
				t.Fatalf("destination %q was accepted", destination)
			}
		})
	}
}

func TestDiscoverLatestRejectsHTTPFailureAndHonorsCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "no release", http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	if _, err := discoverLatest(context.Background(), srv.Client(), srv.URL+"/releases"); err == nil ||
		!strings.Contains(err.Error(), "503") {
		t.Fatalf("HTTP failure = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := discoverLatest(ctx, srv.Client(), srv.URL+"/releases"); err == nil {
		t.Fatal("cancelled discovery succeeded")
	}
}
