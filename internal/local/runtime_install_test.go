package local

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func runtimeInstallFixture(t *testing.T, platform string) (*RuntimeInstaller, *atomic.Int32, []byte) {
	t.Helper()
	spec := runtimePlatforms[platform]
	data := runtimeCompressed(t, spec.Format, runtimeTar(t,
		archiveMember{name: spec.Binary, body: "fixture executable", mode: 0o755},
		archiveMember{name: "lib/ollama/libfixture.so", body: "companion library", mode: 0o644},
		archiveMember{name: "NOTICE", body: "fixture license", mode: 0o644}))
	r := fixtureRuntimeRelease(platform)
	r.Assets[0].Size = int64(len(data))
	r.Assets[0].Digest = fmt.Sprintf("sha256:%x", sha256.Sum256(data))
	metadata, _ := json.Marshal(r)
	calls := &atomic.Int32{}
	i := &RuntimeInstaller{Dir: filepath.Join(t.TempDir(), "runtimes"), platform: platform,
		freeSpace: func(string) (uint64, bool) { return 32 << 30, true },
		client: &http.Client{Transport: runtimeTransport(func(req *http.Request) (*http.Response, error) {
			calls.Add(1)
			if req.URL.String() == runtimeReleaseURL {
				return runtimeResponse(200, metadata), nil
			}
			if req.URL.String() != r.Assets[0].URL {
				t.Errorf("unexpected download %s", req.URL)
			}
			return runtimeResponse(200, data), nil
		})},
	}
	return i, calls, data
}

func assertNoInstallStage(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".install-") {
			t.Errorf("leaked attempt %s", entry.Name())
		}
	}
}

func TestRuntimeInstallerPublishesAndReusesWithoutNetwork(t *testing.T) {
	for platform := range runtimePlatforms {
		t.Run(platform, func(t *testing.T) {
			i, calls, _ := runtimeInstallFixture(t, platform)
			var stages []string
			i.Progress = func(p RuntimeProgress) { stages = append(stages, p.Stage) }
			if got, err := i.Find(); err != nil || got.State != HostAbsent {
				t.Fatalf("absent Find: %+v, %v", got, err)
			}
			if _, err := os.Stat(i.Dir); !os.IsNotExist(err) {
				t.Fatal("read-only discovery created storage")
			}
			host, err := i.Ensure(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if !host.Managed || host.State != HostInstalled || !filepath.IsAbs(host.Binary) || host.Version != "v99.12.3" {
				t.Fatalf("host=%+v", host)
			}
			if calls.Load() != 2 {
				t.Fatalf("requests=%d", calls.Load())
			}
			i.client = &http.Client{Transport: runtimeTransport(func(*http.Request) (*http.Response, error) {
				t.Error("reuse touched network")
				return nil, fmt.Errorf("offline")
			})}
			for n := 0; n < 3; n++ {
				got, err := i.Ensure(context.Background())
				if err != nil || got != host {
					t.Fatalf("reuse=%+v, %v", got, err)
				}
			}
			found, err := i.Find()
			if err != nil || found != host {
				t.Fatalf("Find=%+v, %v", found, err)
			}
			if !strings.Contains(strings.Join(stages, ","), "checking,downloading") || stages[len(stages)-1] != "ready" {
				t.Fatalf("stages=%v", stages)
			}
			assertNoInstallStage(t, i.Dir)
			// Companion libraries and licenses live in the same immutable tree.
			base := filepath.Join(i.Dir, strings.Split(strings.TrimPrefix(host.Binary, i.Dir+string(filepath.Separator)), string(filepath.Separator))[0])
			if b, err := os.ReadFile(filepath.Join(base, "lib/ollama/libfixture.so")); err != nil || string(b) != "companion library" {
				t.Fatalf("library=%q, %v", b, err)
			}
		})
	}
}

func TestRuntimeInstallerFailureDoesNotPublishOrRemoveOtherData(t *testing.T) {
	for _, failure := range []string{"checksum", "short", "long", "wrong size", "bad archive", "missing executable", "not executable", "disk before download", "disk during extraction", "cancel download", "cancel extraction"} {
		t.Run(failure, func(t *testing.T) {
			i, _, data := runtimeInstallFixture(t, "linux/amd64")
			if err := os.MkdirAll(i.Dir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(i.Dir, "unrelated-cache"), []byte("keep"), 0o600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			transport := i.client.Transport
			i.client.Transport = runtimeTransport(func(req *http.Request) (*http.Response, error) {
				if req.URL.String() == runtimeReleaseURL {
					if failure == "bad archive" || failure == "missing executable" || failure == "not executable" {
						if failure == "bad archive" {
							data = []byte("not an archive")
						} else {
							m := archiveMember{name: "bin/ollama", body: "fixture", mode: 0o600}
							if failure == "missing executable" {
								m.name = "other"
								m.mode = 0o700
							}
							data = runtimeCompressed(t, "tar.zst", runtimeTar(t, m))
						}
						r := fixtureRuntimeRelease("linux/amd64")
						r.Assets[0].Size = int64(len(data))
						r.Assets[0].Digest = fmt.Sprintf("sha256:%x", sha256.Sum256(data))
						metadata, _ := json.Marshal(r)
						return runtimeResponse(200, metadata), nil
					}
					return transport.RoundTrip(req)
				}
				body := bytes.Clone(data)
				switch failure {
				case "checksum":
					body[0] ^= 1
				case "short":
					body = body[:len(body)-1]
				case "long":
					body = append(body, 'x')
				case "cancel download":
					cancel()
				}
				resp := runtimeResponse(200, body)
				if failure == "short" || failure == "long" {
					resp.ContentLength = -1
				}
				if failure == "wrong size" {
					resp.ContentLength++
				}
				return resp, nil
			})
			if failure == "disk before download" {
				i.freeSpace = func(string) (uint64, bool) { return 0, true }
			}
			if failure == "disk during extraction" {
				var n int
				i.freeSpace = func(string) (uint64, bool) {
					n++
					if n > 1 {
						return 0, true
					}
					return 32 << 30, true
				}
			}
			if failure == "cancel extraction" {
				i.Progress = func(p RuntimeProgress) {
					if p.Stage == "extracting" {
						cancel()
					}
				}
			}
			if _, err := i.Ensure(ctx); err == nil {
				t.Fatal("failure succeeded")
			} else if strings.HasPrefix(failure, "cancel") && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancel=%v", err)
			}
			if _, err := os.Stat(filepath.Join(i.Dir, i.currentRecordName())); !os.IsNotExist(err) {
				t.Fatal("failed attempt published")
			}
			assertNoInstallStage(t, i.Dir)
			if b, err := os.ReadFile(filepath.Join(i.Dir, "unrelated-cache")); err != nil || string(b) != "keep" {
				t.Fatalf("unrelated data changed: %q, %v", b, err)
			}
		})
	}
}

func TestRuntimeInstallerSerializesConcurrentProjects(t *testing.T) {
	i, calls, _ := runtimeInstallFixture(t, "linux/amd64")
	var wg sync.WaitGroup
	results := make(chan Host, 8)
	for n := 0; n < 8; n++ {
		wg.Go(func() {
			copy := *i
			host, err := copy.Ensure(context.Background())
			if err != nil {
				t.Error(err)
				return
			}
			results <- host
		})
	}
	wg.Wait()
	close(results)
	var first Host
	for host := range results {
		if first.Binary == "" {
			first = host
		}
		if host != first {
			t.Errorf("different installation: %+v", host)
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("concurrent setup downloaded %d requests", calls.Load())
	}
	assertNoInstallStage(t, i.Dir)
}

func TestRuntimeInstallerRefusesTamperedCompletionRecords(t *testing.T) {
	for _, damage := range []string{"outside binary", "outside digest", "foreign platform", "missing completion", "missing binary", "symlink record", "symlink directory"} {
		t.Run(damage, func(t *testing.T) {
			i, _, _ := runtimeInstallFixture(t, "linux/amd64")
			host, err := i.Ensure(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			pointer := filepath.Join(i.Dir, i.currentRecordName())
			data, err := os.ReadFile(pointer)
			if err != nil {
				t.Fatal(err)
			}
			var record runtimeInstallation
			if err := json.Unmarshal(data, &record); err != nil {
				t.Fatal(err)
			}
			bundle := filepath.Join(i.Dir, record.directory())
			switch damage {
			case "outside binary":
				record.Binary = "../../escape"
			case "outside digest":
				record.Digest = "../../escape"
			case "foreign platform":
				record.Platform = "darwin/arm64"
			case "missing completion":
				if err := os.Remove(filepath.Join(bundle, ".kolk-runtime.json")); err != nil {
					t.Fatal(err)
				}
			case "missing binary":
				if err := os.Remove(host.Binary); err != nil {
					t.Fatal(err)
				}
			case "symlink record":
				other := filepath.Join(i.Dir, "other.json")
				if err := os.Rename(pointer, other); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(other, pointer); err != nil {
					t.Fatal(err)
				}
			case "symlink directory":
				other := bundle + "-other"
				if err := os.Rename(bundle, other); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(other, bundle); err != nil {
					t.Fatal(err)
				}
			}
			if strings.HasPrefix(damage, "outside") || damage == "foreign platform" {
				data, _ = json.Marshal(record)
				if err := os.WriteFile(pointer, data, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := i.Find(); err == nil {
				t.Fatal("inconsistent installation reused")
			}
		})
	}
}
