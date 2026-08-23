package selfupdate

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/atomicfile"
)

func releaseServer(t *testing.T, latest string, archive []byte, manifest func(string, []byte) string) (*httptest.Server, *[]string) {
	t.Helper()
	var base string
	requests := &[]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*requests = append(*requests, r.Method+" "+r.URL.Path)
		prefix := "/releases/download/v" + latest + "/"
		switch {
		case r.URL.Path == "/releases/latest":
			http.Redirect(w, r, base+"/releases/tag/v"+latest, http.StatusFound)
		case r.URL.Path == "/releases/tag/v"+latest:
			w.WriteHeader(http.StatusOK)
		case r.URL.Path == prefix+"checksums.txt":
			archiveName := "kolk_" + latest + "_darwin_arm64.tar.gz"
			_, _ = io.WriteString(w, manifest(archiveName, archive))
		case strings.HasPrefix(r.URL.Path, prefix+"kolk_"):
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	base = srv.URL
	return srv, requests
}

func validManifest(name string, archive []byte) string { return checksumRow(name, archive) }

func testUpdater(srv *httptest.Server, current, goos, goarch string, executable func() (string, error)) updater {
	return updater{
		client:         srv.Client(),
		releasesURL:    srv.URL + "/releases",
		currentVersion: current,
		goos:           goos,
		goarch:         goarch,
		executable:     executable,
		replace:        atomicfile.Write,
	}
}

func TestUpdaterPreflightRejectsBeforeAnyEffect(t *testing.T) {
	for _, tc := range []struct {
		name, current, goos, goarch string
	}{
		{"development build", "dev", "darwin", "arm64"},
		{"prerelease build", "1.2.3-dev.1", "linux", "amd64"},
		{"unsupported OS", "1.2.3", "windows", "amd64"},
		{"unsupported architecture", "1.2.3", "linux", "386"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			network, executableCalls, writes := 0, 0, 0
			u := updater{
				client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
					network++
					return nil, errors.New("network must not run")
				})},
				releasesURL: officialReleasesURL, currentVersion: tc.current,
				goos: tc.goos, goarch: tc.goarch,
				executable: func() (string, error) { executableCalls++; return "", errors.New("must not run") },
				replace:    func(string, []byte, os.FileMode) error { writes++; return errors.New("must not run") },
			}
			if _, err := u.run(context.Background()); err == nil {
				t.Fatal("invalid preflight succeeded")
			}
			if network != 0 || executableCalls != 0 || writes != 0 {
				t.Fatalf("effects = network %d, executable %d, writes %d", network, executableCalls, writes)
			}
		})
	}
}

func TestUpdaterSkipsArtifactForSameOrNewerBuild(t *testing.T) {
	for _, current := range []string{"1.2.3", "2.0.0"} {
		t.Run(current, func(t *testing.T) {
			srv, requests := releaseServer(t, "1.2.3", nil, validManifest)
			defer srv.Close()
			executableCalls := 0
			u := testUpdater(srv, current, "darwin", "arm64", func() (string, error) {
				executableCalls++
				return "", errors.New("must not run")
			})

			result, err := u.run(context.Background())
			if err != nil || result.Updated || result.Current != current || result.Latest != "1.2.3" {
				t.Fatalf("result = (%+v, %v)", result, err)
			}
			if executableCalls != 0 || len(*requests) != 2 {
				t.Fatalf("executable calls = %d, requests = %v", executableCalls, *requests)
			}
		})
	}
}

func TestUpdaterAtomicallyReplacesResolvedExecutable(t *testing.T) {
	archive := validReleaseArchive(t)
	srv, _ := releaseServer(t, "1.2.3", archive, validManifest)
	defer srv.Close()
	dir := t.TempDir()
	target := filepath.Join(dir, "kolk-real")
	link := filepath.Join(dir, "kolk")
	if err := os.WriteFile(target, []byte("old executable"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Base(target), link); err != nil {
		t.Fatal(err)
	}
	u := testUpdater(srv, "1.0.0", "darwin", "arm64", func() (string, error) { return link, nil })

	result, err := u.run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	resolved, _ := filepath.EvalSymlinks(link)
	if !result.Updated || result.Current != "1.0.0" || result.Latest != "1.2.3" || result.Path != resolved {
		t.Fatalf("result = %+v, resolved path %q", result, resolved)
	}
	if body, _ := os.ReadFile(target); string(body) != "verified executable" {
		t.Fatalf("installed bytes = %q", body)
	}
	info, err := os.Stat(target)
	if err != nil || info.Mode().Perm() != 0o755 {
		t.Fatalf("installed mode = %v, err %v", info.Mode(), err)
	}
	if linkInfo, err := os.Lstat(link); err != nil || linkInfo.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("launch symlink was replaced: mode %v, err %v", linkInfo.Mode(), err)
	}
}

func TestUpdaterFailuresPreserveExecutable(t *testing.T) {
	archive := validReleaseArchive(t)
	for _, tc := range []struct {
		name     string
		manifest func(string, []byte) string
		execPath func(string) string
		replace  func(string, []byte, os.FileMode) error
	}{
		{
			name: "checksum mismatch",
			manifest: func(name string, _ []byte) string {
				return strings.Repeat("0", 64) + "  " + name + "\n"
			},
		},
		{
			name: "executable resolution",
			execPath: func(dir string) string {
				return filepath.Join(dir, "missing-kolk")
			},
		},
		{
			name: "replacement",
			replace: func(string, []byte, os.FileMode) error {
				return errors.New("write refused")
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			manifest := tc.manifest
			if manifest == nil {
				manifest = validManifest
			}
			srv, _ := releaseServer(t, "1.2.3", archive, manifest)
			defer srv.Close()
			dir := t.TempDir()
			path := filepath.Join(dir, "kolk")
			if err := os.WriteFile(path, []byte("old executable"), 0o700); err != nil {
				t.Fatal(err)
			}
			execPath := path
			if tc.execPath != nil {
				execPath = tc.execPath(dir)
			}
			u := testUpdater(srv, "1.0.0", "darwin", "arm64", func() (string, error) { return execPath, nil })
			if tc.replace != nil {
				u.replace = tc.replace
			}

			if _, err := u.run(context.Background()); err == nil {
				t.Fatal("failed update succeeded")
			}
			body, err := os.ReadFile(path)
			if err != nil || string(body) != "old executable" {
				t.Fatalf("old executable = %q, err %v", body, err)
			}
			info, _ := os.Stat(path)
			if info.Mode().Perm() != 0o700 {
				t.Fatalf("old mode = %o", info.Mode().Perm())
			}
		})
	}
}

func TestUpdaterNetworkStageFailuresPreserveExecutable(t *testing.T) {
	archive := validReleaseArchive(t)
	for _, stage := range []string{"discovery", "archive"} {
		t.Run(stage, func(t *testing.T) {
			var base string
			name := "kolk_1.2.3_darwin_arm64.tar.gz"
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/releases/latest" && stage == "discovery":
					http.Error(w, "unavailable", http.StatusServiceUnavailable)
				case r.URL.Path == "/releases/latest":
					http.Redirect(w, r, base+"/releases/tag/v1.2.3", http.StatusFound)
				case r.URL.Path == "/releases/tag/v1.2.3":
					w.WriteHeader(http.StatusOK)
				case strings.HasSuffix(r.URL.Path, "/checksums.txt"):
					_, _ = io.WriteString(w, checksumRow(name, archive))
				case strings.HasSuffix(r.URL.Path, "/"+name):
					http.Error(w, "archive unavailable", http.StatusServiceUnavailable)
				default:
					http.NotFound(w, r)
				}
			}))
			defer srv.Close()
			base = srv.URL
			path := filepath.Join(t.TempDir(), "kolk")
			if err := os.WriteFile(path, []byte("old executable"), 0o700); err != nil {
				t.Fatal(err)
			}
			u := testUpdater(srv, "1.0.0", "darwin", "arm64", func() (string, error) { return path, nil })

			if _, err := u.run(context.Background()); err == nil {
				t.Fatalf("%s failure succeeded", stage)
			}
			body, err := os.ReadFile(path)
			if err != nil || string(body) != "old executable" {
				t.Fatalf("%s failure changed bytes to %q, err %v", stage, body, err)
			}
			info, _ := os.Stat(path)
			if info.Mode().Perm() != 0o700 {
				t.Fatalf("%s failure changed mode to %o", stage, info.Mode().Perm())
			}
		})
	}
}

func TestUpdaterTreatsCommittedDurabilityErrorAsUpdatedWarning(t *testing.T) {
	archive := validReleaseArchive(t)
	srv, _ := releaseServer(t, "1.2.3", archive, validManifest)
	defer srv.Close()
	path := filepath.Join(t.TempDir(), "kolk")
	if err := os.WriteFile(path, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	u := testUpdater(srv, "1.0.0", "darwin", "arm64", func() (string, error) { return path, nil })
	u.replace = func(path string, body []byte, perm os.FileMode) error {
		if err := os.WriteFile(path, body, perm); err != nil {
			return err
		}
		return &atomicfile.DurabilityError{Path: path, Err: errors.New("directory sync refused")}
	}

	result, err := u.run(context.Background())
	if err != nil || !result.Updated || !strings.Contains(result.Warning, "directory sync refused") {
		t.Fatalf("committed result = (%+v, %v)", result, err)
	}
}

func TestResolveExecutableRejectsNonRegularAndResolvesSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real")
	link := filepath.Join(dir, "link")
	if err := os.WriteFile(target, []byte("x"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Base(target), link); err != nil {
		t.Fatal(err)
	}
	got, err := resolveExecutable(link)
	want, wantErr := filepath.EvalSymlinks(target)
	if err != nil || wantErr != nil || got != want {
		t.Fatalf("resolveExecutable = (%q, %v), want (%q, %v)", got, err, want, wantErr)
	}
	if _, err := resolveExecutable(dir); err == nil {
		t.Fatal("directory was accepted as an executable")
	}
}

func TestProductionUpdaterHasBoundedOfficialDependencies(t *testing.T) {
	u := productionUpdater()
	if u.releasesURL != officialReleasesURL || u.currentVersion == "" ||
		u.goos != runtime.GOOS || u.goarch != runtime.GOARCH || u.client.Timeout <= 0 ||
		u.executable == nil || u.replace == nil {
		t.Fatalf("production updater is incomplete: %+v", u)
	}
}

func TestUpdaterCancellationPreservesExecutable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kolk")
	if err := os.WriteFile(path, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	u := updater{
		client: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return nil, req.Context().Err()
		})},
		releasesURL: officialReleasesURL, currentVersion: "1.0.0", goos: "darwin", goarch: "arm64",
		executable: func() (string, error) { return path, nil }, replace: atomicfile.Write,
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := u.run(ctx); err == nil {
		t.Fatal("cancelled update succeeded")
	}
	if body, _ := os.ReadFile(path); string(body) != "old" {
		t.Fatalf("cancelled update changed executable to %q", body)
	}
}
