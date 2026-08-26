package local

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sha256Of(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func fetcherFor(body []byte, calls *int) Fetcher {
	return func(context.Context, string) (io.ReadCloser, error) {
		if calls != nil {
			*calls++
		}
		return io.NopCloser(strings.NewReader(string(body))), nil
	}
}

func TestInstallRuntimePlacesVerifiedBytes(t *testing.T) {
	body := []byte("#!/bin/sh\necho managed sidecar\n")
	dest := filepath.Join(t.TempDir(), "runtime", SidecarName)
	release := RuntimeRelease{
		Version: "0.1.2", URL: "https://example.invalid/ollama",
		SHA256: sha256Of(body), Size: uint64(len(body)),
	}

	if err := InstallRuntime(context.Background(), release, dest, fetcherFor(body, nil)); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(dest)
	if err != nil || string(got) != string(body) {
		t.Fatalf("installed bytes = %q, err = %v", got, err)
	}
	info, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("mode = %v, want an executable the owner can run", info.Mode().Perm())
	}
}

func TestInstallRuntimeRefusesBytesThatDoNotMatch(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "runtime", SidecarName)
	release := RuntimeRelease{
		Version: "0.1.2", URL: "https://example.invalid/ollama",
		SHA256: sha256Of([]byte("what was promised")),
	}

	err := InstallRuntime(context.Background(), release, dest, fetcherFor([]byte("what arrived"), nil))
	if err == nil {
		t.Fatal("bytes that do not match the pinned checksum must never be installed")
	}
	if !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("error = %v, want the mismatch named", err)
	}
	if _, statErr := os.Stat(dest); statErr == nil {
		t.Fatal("a rejected download was left on disk")
	}
}

func TestInstallRuntimeRefusesToRunWithoutAPinnedChecksum(t *testing.T) {
	calls := 0
	dest := filepath.Join(t.TempDir(), "runtime", SidecarName)
	release := RuntimeRelease{Version: "0.1.2", URL: "https://example.invalid/ollama"}

	err := InstallRuntime(context.Background(), release, dest, fetcherFor([]byte("anything"), &calls))
	if err == nil {
		t.Fatal("an unpinned release must be refused")
	}
	// Nothing is fetched: there would be no way to judge what came back.
	if calls != 0 {
		t.Fatalf("fetched %d times without a checksum to verify against", calls)
	}
}

func TestInstallRuntimeRefusesMoreBytesThanPromised(t *testing.T) {
	body := []byte(strings.Repeat("x", 4096))
	dest := filepath.Join(t.TempDir(), "runtime", SidecarName)
	release := RuntimeRelease{
		Version: "0.1.2", URL: "https://example.invalid/ollama",
		SHA256: sha256Of(body), Size: 16,
	}

	err := InstallRuntime(context.Background(), release, dest, fetcherFor(body, nil))
	if err == nil {
		t.Fatal("a download larger than its declared size must be stopped, not buffered")
	}
	if _, statErr := os.Stat(dest); statErr == nil {
		t.Fatal("an oversized download was left on disk")
	}
}

func TestInstallRuntimeIsIdempotentForTheSameVersion(t *testing.T) {
	body := []byte("#!/bin/sh\nexit 0\n")
	dest := filepath.Join(t.TempDir(), "runtime", SidecarName)
	release := RuntimeRelease{
		Version: "0.1.2", URL: "https://example.invalid/ollama", SHA256: sha256Of(body),
	}
	calls := 0

	for range 3 {
		if err := InstallRuntime(context.Background(), release, dest, fetcherFor(body, &calls)); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatalf("downloaded %d times, want an install that is already correct to be left alone", calls)
	}
}

func TestInstallRuntimeReplacesADifferentBinary(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "runtime", SidecarName)
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte("an older runtime"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := []byte("the pinned runtime")
	calls := 0

	err := InstallRuntime(context.Background(), RuntimeRelease{
		Version: "0.2.0", URL: "https://example.invalid/ollama", SHA256: sha256Of(body),
	}, dest, fetcherFor(body, &calls))
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("fetched %d times, want the wrong binary replaced exactly once", calls)
	}
	got, _ := os.ReadFile(dest)
	if string(got) != string(body) {
		t.Fatalf("installed = %q", got)
	}
}

func TestInstallRuntimeSurfacesAFetchFailure(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "runtime", SidecarName)
	err := InstallRuntime(context.Background(), RuntimeRelease{
		Version: "0.1.2", URL: "https://example.invalid/ollama", SHA256: sha256Of([]byte("x")),
	}, dest, func(context.Context, string) (io.ReadCloser, error) {
		return nil, errors.New("network is unreachable")
	})
	if err == nil || !strings.Contains(err.Error(), "unreachable") {
		t.Fatalf("error = %v, want the fetch failure preserved", err)
	}
}

// A half-filled pin is the dangerous state: a version and URL with no checksum
// reads as "configured" while verifying nothing. Either all three are present
// or this build installs no runtime.
func TestPinnedRuntimeIsCompleteOrAbsent(t *testing.T) {
	release, ok := PinnedRuntime()
	if !ok {
		if pinnedRuntime.SHA256 != "" {
			t.Fatal("a checksum is pinned but the release is not usable")
		}
		return
	}
	if release.Version == "" || release.URL == "" || release.SHA256 == "" {
		t.Fatalf("pinned release is incomplete: %+v", release)
	}
	if len(release.SHA256) != 64 {
		t.Fatalf("pinned checksum %q is not a sha-256 digest", release.SHA256)
	}
	if !strings.HasPrefix(release.URL, "https://") {
		t.Fatalf("pinned url %q must be https", release.URL)
	}
}
