package local

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"testing/fstest"
	"time"
)

// companionHardware is the machine detection reads for each official companion.
func companionHardware(name string) fstest.MapFS {
	switch name {
	case "rocm":
		root := fstest.MapFS{}
		drmCard(root, "card0", "0x1002")
		return root
	case "jetpack5":
		return fstest.MapFS{"etc/nv_tegra_release": &fstest.MapFile{Data: []byte("# R35 (release), REVISION: 5.0\n")}}
	case "jetpack6":
		return fstest.MapFS{"etc/nv_tegra_release": &fstest.MapFile{Data: []byte("# R36 (release), REVISION: 4.3\n")}}
	}
	return fstest.MapFS{}
}

type companionFixture struct {
	installer *RuntimeInstaller
	calls     *atomic.Int32
	downloads *[]string
	companion []byte
	standard  int64
}

// companionInstallFixture serves one release with the standard bundle and the
// named companion, whose members are given, on hardware that needs it.
func companionInstallFixture(t *testing.T, platform, name string, members ...archiveMember) companionFixture {
	t.Helper()
	i, calls, standard := runtimeInstallFixture(t, platform)
	spec := runtimePlatforms[platform]
	extra := runtimeCompanions[platform][name]
	companion := runtimeCompressed(t, extra.Format, runtimeTar(t, members...))
	r := fixtureRuntimeRelease(platform)
	r.Assets[0].Size, r.Assets[0].Digest = int64(len(standard)), fmt.Sprintf("sha256:%x", sha256.Sum256(standard))
	r.Assets = append(r.Assets, runtimeAsset{Name: extra.Asset, Size: int64(len(companion)),
		Digest: fmt.Sprintf("sha256:%x", sha256.Sum256(companion)),
		URL:    "https://github.com/ollama/ollama/releases/download/v99.12.3/" + extra.Asset})
	metadata, _ := json.Marshal(r)
	var downloads []string
	i.client = &http.Client{Transport: runtimeTransport(func(req *http.Request) (*http.Response, error) {
		calls.Add(1)
		switch req.URL.String() {
		case runtimeReleaseURL:
			return runtimeResponse(200, metadata), nil
		case r.Assets[0].URL:
			downloads = append(downloads, spec.Asset)
			return runtimeResponse(200, standard), nil
		case r.Assets[1].URL:
			downloads = append(downloads, extra.Asset)
			return runtimeResponse(200, companion), nil
		}
		t.Errorf("unexpected download %s", req.URL)
		return runtimeResponse(404, nil), nil
	})}
	i.Hardware = companionHardware(name)
	return companionFixture{installer: i, calls: calls, downloads: &downloads, companion: companion, standard: int64(len(standard))}
}

func companionMembers(name string) []archiveMember {
	dir := "lib/ollama/" + name + "_fixture/"
	return []archiveMember{
		{name: dir, kind: tar.TypeDir, mode: 0o755},
		{name: dir + "libggml-gpu.so.1", body: "companion accelerator library", mode: 0o644},
		{name: dir + "libggml-gpu.so", kind: tar.TypeSymlink, target: "libggml-gpu.so.1", mode: 0o777},
	}
}

func installedTree(t *testing.T, i *RuntimeInstaller, host Host) string {
	t.Helper()
	rel := strings.TrimPrefix(host.Binary, i.Dir+string(filepath.Separator))
	return filepath.Join(i.Dir, strings.Split(rel, string(filepath.Separator))[0])
}

// The companion is verified like the standard bundle, extracted beside it in
// private staging, and published as one new immutable tree whose identity
// names both. A standard-only tree of the same release is a different
// directory, so a runtime already running from it is never modified.
func TestRuntimeInstallerAddsTheCompanionToANewTree(t *testing.T) {
	for platform, companions := range runtimeCompanions {
		for name := range companions {
			t.Run(platform+"/"+name, func(t *testing.T) {
				f := companionInstallFixture(t, platform, name, companionMembers(name)...)
				var bundles []string
				f.installer.Progress = func(p RuntimeProgress) {
					if p.Stage == "downloading" && p.Completed == 0 {
						bundles = append(bundles, p.Bundle)
					}
				}
				host, err := f.installer.Ensure(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				if got := strings.Join(*f.downloads, ","); got != runtimePlatforms[platform].Asset+","+runtimeCompanions[platform][name].Asset {
					t.Fatalf("downloads = %s; want the standard bundle, then its companion", got)
				}
				if strings.Join(bundles, ",") != ","+name {
					t.Errorf("download progress bundles = %q; want the standard bundle, then %s", bundles, name)
				}
				tree := installedTree(t, f.installer, host)
				if !strings.Contains(filepath.Base(tree), name) {
					t.Errorf("tree %s does not carry the companion in its identity", tree)
				}
				dir := filepath.Join(tree, "lib/ollama", name+"_fixture")
				if b, err := os.ReadFile(filepath.Join(dir, "libggml-gpu.so")); err != nil || string(b) != "companion accelerator library" {
					t.Fatalf("companion library through its link = %q, %v", b, err)
				}
				if b, err := os.ReadFile(filepath.Join(tree, "lib/ollama/libfixture.so")); err != nil || string(b) != "companion library" {
					t.Fatalf("the standard bundle's library was lost: %q, %v", b, err)
				}
				record, err := os.ReadFile(filepath.Join(tree, ".kolk-runtime.json"))
				if err != nil || !strings.Contains(string(record), `"companion":"`+name+`"`) {
					t.Fatalf("installation record = %s, %v; want the companion recorded", record, err)
				}
				assertNoInstallStage(t, f.installer.Dir)
				f.installer.client = &http.Client{Transport: runtimeTransport(func(*http.Request) (*http.Response, error) {
					t.Error("reuse touched network")
					return nil, fmt.Errorf("offline")
				})}
				if again, err := f.installer.Ensure(context.Background()); err != nil || again != host {
					t.Fatalf("offline reuse = %+v, %v; want %+v", again, err, host)
				}
			})
		}
	}
}

// A companion adds its own directory; it never replaces a file of the
// standard bundle. On a first install an overlapping or tampered companion is
// dropped and the standard runtime installed; too little room refuses both.
func TestRuntimeInstallerRefusesAnUntrustedCompanion(t *testing.T) {
	const platform = "linux/amd64"
	spec := runtimePlatforms[platform]
	cases := map[string]func(*companionFixture){
		"overlap": nil,
		"tampered": func(f *companionFixture) {
			base := f.installer.client.Transport
			f.installer.client = &http.Client{Transport: runtimeTransport(func(req *http.Request) (*http.Response, error) {
				if strings.HasSuffix(req.URL.Path, runtimeCompanions[platform]["rocm"].Asset) {
					bad := append([]byte(nil), f.companion...)
					bad[len(bad)-1] ^= 0xff
					return runtimeResponse(200, bad), nil
				}
				return base.RoundTrip(req)
			})}
		},
		"no room for both": func(f *companionFixture) {
			// Room for the standard bundle alone, one byte short of both.
			free := uint64(64<<20 + f.standard + int64(len(f.companion)) - 1)
			f.installer.freeSpace = func(string) (uint64, bool) { return free, true }
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			members := companionMembers("rocm")
			if name == "overlap" {
				members = append(members, archiveMember{name: spec.Binary, body: "a companion replacing the runtime", mode: 0o755})
			}
			f := companionInstallFixture(t, platform, "rocm", members...)
			if mutate != nil {
				mutate(&f)
			}
			var notes []string
			f.installer.Progress = func(p RuntimeProgress) {
				if p.Stage == "note" {
					notes = append(notes, p.Note)
				}
			}
			host, err := f.installer.Ensure(context.Background())
			if name == "no room for both" {
				// Room is the machine's to fix, not the bundle's: refused whole.
				if err == nil || len(*f.downloads) != 0 {
					t.Fatalf("no room: err=%v downloads=%v; want a refusal before downloading", err, *f.downloads)
				}
				return
			}
			// A first install is not held hostage by its accelerator bundle:
			// the standard runtime is installed, and the note says why the
			// bundle is missing (V43.4c.3 F11; the b2 contract refused both).
			if err != nil {
				t.Fatalf("a bad companion blocked the standard runtime: %v", err)
			}
			tree := installedTree(t, f.installer, host)
			if strings.Contains(filepath.Base(tree), "rocm") {
				t.Fatalf("an untrusted companion was installed: %s", tree)
			}
			if _, err := os.Stat(filepath.Join(tree, "lib/ollama/rocm_fixture")); !os.IsNotExist(err) {
				t.Fatal("part of an untrusted companion was published")
			}
			if b, err := os.ReadFile(host.Binary); err != nil || string(b) != "fixture executable" {
				t.Fatalf("the standard binary is not the verified one: %q, %v", b, err)
			}
			if len(notes) != 1 || !strings.Contains(notes[0], "ROCm bundle") || !strings.Contains(notes[0], "without it") {
				t.Fatalf("notes = %q; want one saying the runtime was installed without the ROCm bundle", notes)
			}
			if name == "overlap" && !strings.Contains(notes[0], spec.Binary) {
				t.Errorf("note %q does not name the overlapping path", notes[0])
			}
		})
	}
}

// A completion pointer naming a companion that is not official for the
// platform, or one without a valid digest, is refused rather than trusted.
func TestRuntimeInstallerRefusesATamperedCompanionRecord(t *testing.T) {
	f := companionInstallFixture(t, "linux/amd64", "rocm", companionMembers("rocm")...)
	if _, err := f.installer.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	pointer := filepath.Join(f.installer.Dir, f.installer.currentRecordName())
	original, err := os.ReadFile(pointer)
	if err != nil {
		t.Fatal(err)
	}
	for name, edit := range map[string]func(*runtimeInstallation){
		"unofficial companion":     func(r *runtimeInstallation) { r.Companion = "mlx" },
		"companion without digest": func(r *runtimeInstallation) { r.CompanionDigest = "" },
		"digest without companion": func(r *runtimeInstallation) { r.Companion = "" },
		"uppercase digest":         func(r *runtimeInstallation) { r.CompanionDigest = strings.ToUpper(r.CompanionDigest) },
	} {
		t.Run(name, func(t *testing.T) {
			var record runtimeInstallation
			if err := json.Unmarshal(original, &record); err != nil {
				t.Fatal(err)
			}
			edit(&record)
			tampered, _ := json.Marshal(record)
			if err := os.WriteFile(pointer, tampered, 0o600); err != nil {
				t.Fatal(err)
			}
			if host, err := f.installer.Find(); err == nil && host.State == HostInstalled {
				t.Fatalf("a tampered companion record was trusted: %+v", host)
			}
		})
	}
}

// Hardware can change under an installation. A newly needed companion builds
// a new tree beside the old one, which is left exactly as it was; a companion
// no longer needed is kept, since the tree still runs without that hardware.
func TestRuntimeInstallerAddsANeededCompanionBesideTheInstalledTree(t *testing.T) {
	f := companionInstallFixture(t, "linux/amd64", "rocm", companionMembers("rocm")...)
	amd := f.installer.Hardware
	f.installer.Hardware = nil
	standard, err := f.installer.Ensure(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	standardTree := installedTree(t, f.installer, standard)
	before, err := os.Stat(standard.Binary)
	if err != nil {
		t.Fatal(err)
	}

	f.installer.Hardware = amd
	*f.downloads = nil
	withROCm, err := f.installer.Ensure(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if withROCm == standard || installedTree(t, f.installer, withROCm) == standardTree {
		t.Fatalf("the newly needed companion reused the standard tree: %+v", withROCm)
	}
	if len(*f.downloads) != 2 {
		t.Errorf("downloads = %v; want the standard bundle and its companion for the new tree", *f.downloads)
	}
	after, err := os.Stat(standard.Binary)
	if err != nil || !after.ModTime().Equal(before.ModTime()) || after.Size() != before.Size() {
		t.Fatalf("the installed standard tree changed under the upgrade: %v", err)
	}
	if _, err := os.Stat(filepath.Join(standardTree, "lib/ollama/rocm_fixture")); !os.IsNotExist(err) {
		t.Fatal("the companion was written into the tree that was already installed")
	}

	f.installer.client = &http.Client{Transport: runtimeTransport(func(*http.Request) (*http.Response, error) {
		t.Error("reuse touched network")
		return nil, fmt.Errorf("offline")
	})}
	for _, hardware := range []fs.FS{amd, nil} {
		f.installer.Hardware = hardware
		if got, err := f.installer.Ensure(context.Background()); err != nil || got != withROCm {
			t.Fatalf("offline reuse with hardware %v = %+v, %v; want the ROCm tree", hardware != nil, got, err)
		}
	}
}

// A companion only adds an accelerator, and offline reuse is the promise, so
// a companion that cannot be added leaves the working runtime in use and says
// why. Cancellation still stops Ensure.
func TestRuntimeInstallerKeepsTheWorkingRuntimeWhenACompanionCannotBeAdded(t *testing.T) {
	f := companionInstallFixture(t, "linux/amd64", "rocm", companionMembers("rocm")...)
	amd := f.installer.Hardware
	f.installer.Hardware = nil
	standard, err := f.installer.Ensure(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	f.installer.Hardware = amd
	f.installer.client = &http.Client{Transport: runtimeTransport(func(*http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("offline")
	})}
	var notes []string
	f.installer.Progress = func(p RuntimeProgress) {
		if p.Stage == "note" {
			notes = append(notes, p.Note)
		}
	}
	got, err := f.installer.Ensure(context.Background())
	if err != nil || got.Binary != standard.Binary || got.Version != standard.Version {
		t.Fatalf("offline upgrade = %+v, %v; want the working standard runtime", got, err)
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "ROCm bundle for AMD GPU card0") {
		t.Fatalf("notes = %q; want one naming the ROCm bundle as a person reads it", notes)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := f.installer.Ensure(cancelled); err == nil {
		t.Fatal("a cancelled Ensure returned a runtime")
	}
}

// Discovery says which companion setup would still add, so a session can
// complete a tree installed before the hardware needed one, and a question
// can name what a yes downloads. A tree carrying it, or a machine needing
// none, reports nothing missing.
func TestRuntimeInstallerFindReportsAMissingCompanion(t *testing.T) {
	f := companionInstallFixture(t, "linux/amd64", "rocm", companionMembers("rocm")...)
	amd := f.installer.Hardware
	if absent, err := f.installer.Find(); err != nil || absent.State != HostAbsent || absent.MissingCompanion != "ROCm bundle for AMD GPU card0" {
		t.Fatalf("absent Find = %+v, %v; want the companion setup would add", absent, err)
	}
	f.installer.Hardware = nil
	if _, err := f.installer.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.installer.Hardware = amd
	standard, err := f.installer.Find()
	if err != nil || standard.State != HostInstalled || standard.MissingCompanion != "ROCm bundle for AMD GPU card0" {
		t.Fatalf("standard tree on AMD hardware = %+v, %v; want the missing ROCm bundle reported", standard, err)
	}
	if _, err := f.installer.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	complete, err := f.installer.Find()
	if err != nil || complete.MissingCompanion != "" {
		t.Fatalf("tree with its companion = %+v, %v; want nothing missing", complete, err)
	}
}

// The session starter completes a managed tree that lacks its companion
// through the same setup that installs a missing runtime; a complete tree,
// or a user's own Ollama, starts as it is.
func TestHostStarterCompletesAManagedTreeMissingItsCompanion(t *testing.T) {
	for _, c := range []struct {
		name       string
		host       Host
		provisions int32
	}{
		{"missing companion", Host{State: HostInstalled, Managed: true, Binary: "/managed/standard/ollama", MissingCompanion: "ROCm bundle for AMD GPU card0"}, 1},
		{"complete", Host{State: HostInstalled, Managed: true, Binary: "/managed/rocm/ollama"}, 0},
		{"user's own", Host{State: HostInstalled, Binary: "/usr/local/bin/ollama", MissingCompanion: "ROCm bundle for AMD GPU card0"}, 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newStarterFixture(0, 43111)
			f.starter.Binary = ""
			f.starter.Discover = func(context.Context) Host { return c.host }
			var provisions atomic.Int32
			var started string
			start := f.starter.Start
			f.starter.Start = func(ctx context.Context, executable string, args, env []string) (Process, error) {
				started = executable
				return start(ctx, executable, args, env)
			}
			f.starter.Provision = func(context.Context, io.Writer) (Host, error) {
				provisions.Add(1)
				return Host{State: HostInstalled, Managed: true, Binary: "/managed/rocm/ollama", Version: "v99.12.3"}, nil
			}
			if _, err := f.starter.Ensure(context.Background()); err != nil {
				t.Fatal(err)
			}
			want := c.host.Binary
			if c.provisions == 1 {
				want = "/managed/rocm/ollama"
			}
			if provisions.Load() != c.provisions || started != want {
				t.Fatalf("provisions=%d started=%s; want %d and %s", provisions.Load(), started, c.provisions, want)
			}
		})
	}
}

// Hardware no official bundle serves is described, never guessed into a
// companion: an AMD GPU on arm64 has no ROCm bundle.
func TestRuntimeInstallerFindDescribesUnservedAccelerators(t *testing.T) {
	i, _, _ := runtimeInstallFixture(t, "linux/arm64")
	amd := fstest.MapFS{}
	drmCard(amd, "card0", "0x1002")
	i.Hardware = amd
	host, err := i.Find()
	if err != nil || host.MissingCompanion != "" || !strings.Contains(host.AcceleratorNote, "ROCm") || host.UnservedVendor != "amd" {
		t.Fatalf("Find = %+v, %v; want a note about the AMD GPU, AMD marked unserved, and no companion", host, err)
	}
}

// With no runtime yet, the session still says which hardware setup cannot
// serve, so a fit plan and the picker never place a model on a card the
// status calls unused. A remembered failure is not carried: a fresh install
// ignores it and tries the bundle again.
func TestHostStarterWithNoRuntimeKeepsWhatSetupCannotServe(t *testing.T) {
	i, _, _ := runtimeInstallFixture(t, "linux/arm64")
	amd := fstest.MapFS{}
	drmCard(amd, "card0", "0x1002")
	i.Hardware = amd
	unserved := &HostStarter{Discover: func(context.Context) Host {
		host, _ := i.Find()
		return host
	}}
	host := unserved.Host(context.Background())
	if host.State != HostAbsent || host.UnusableVendor() != "amd" || !strings.Contains(host.AcceleratorNote, "ROCm") {
		t.Fatalf("absent runtime = %+v; want the AMD GPU unusable, as its note says", host)
	}

	fresh := &HostStarter{Discover: func(context.Context) Host {
		return Host{MissingCompanion: "ROCm bundle for AMD GPU card0", CompanionVendor: "amd",
			CompanionFailure: "verifying the ROCm bundle: checksum mismatch"}
	}}
	host = fresh.Host(context.Background())
	if host.MissingCompanion == "" || host.CompanionVendor != "amd" || host.CompanionFailure != "" || host.UnusableVendor() != "" {
		t.Fatalf("absent runtime with a leftover failure = %+v; want the bundle setup adds, no failure, and the GPU usable", host)
	}
}

// EnsureInstalled starts only what is installed: it neither completes a tree
// missing its companion nor sets up an absent runtime.
func TestHostStarterEnsureInstalledNeverSetsUp(t *testing.T) {
	for _, c := range []struct {
		name string
		host Host
		err  bool
	}{
		{"missing companion", Host{State: HostInstalled, Managed: true, Binary: "/managed/standard/ollama", MissingCompanion: "ROCm bundle for AMD GPU card0"}, false},
		{"absent", Host{MissingCompanion: "ROCm bundle for AMD GPU card0"}, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newStarterFixture(0, 43111)
			f.starter.Binary = ""
			f.starter.Discover = func(context.Context) Host { return c.host }
			var provisions atomic.Int32
			f.starter.Provision = func(context.Context, io.Writer) (Host, error) {
				provisions.Add(1)
				return Host{State: HostInstalled, Managed: true, Binary: "/managed/rocm/ollama"}, nil
			}
			_, err := f.starter.EnsureInstalled(context.Background())
			if (err != nil) != c.err || provisions.Load() != 0 {
				t.Fatalf("err=%v provisions=%d; want error=%v and no setup", err, provisions.Load(), c.err)
			}
		})
	}
}

// A runtime the session started still says what it lacks: a fallback that
// kept the old tree reports the missing bundle, and a completed tree does not.
func TestHostStarterRemembersAMissingCompanionAfterStarting(t *testing.T) {
	for _, c := range []struct {
		name, provisioned, want string
	}{
		{"fallback kept the tree", "/managed/standard/ollama", "ROCm bundle for AMD GPU card0"},
		{"completed", "/managed/rocm/ollama", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newStarterFixture(0, 43111)
			f.starter.Binary = ""
			f.starter.Discover = func(context.Context) Host {
				return Host{State: HostInstalled, Managed: true, Binary: "/managed/standard/ollama", MissingCompanion: "ROCm bundle for AMD GPU card0"}
			}
			f.starter.Provision = func(context.Context, io.Writer) (Host, error) {
				return Host{State: HostInstalled, Managed: true, Binary: c.provisioned}, nil
			}
			if _, err := f.starter.Ensure(context.Background()); err != nil {
				t.Fatal(err)
			}
			if got := f.starter.Host(context.Background()); got.State != HostRunning || got.MissingCompanion != c.want {
				t.Fatalf("Host after start = %+v; want running with missing %q", got, c.want)
			}
		})
	}
}

// jetpackSwitchFixture serves the arm64 bundle with both JetPack companions.
func jetpackSwitchFixture(t *testing.T) (*RuntimeInstaller, *[]string) {
	t.Helper()
	i, _, standard := runtimeInstallFixture(t, "linux/arm64")
	r := fixtureRuntimeRelease("linux/arm64")
	r.Assets[0].Size, r.Assets[0].Digest = int64(len(standard)), fmt.Sprintf("sha256:%x", sha256.Sum256(standard))
	bodies := map[string][]byte{r.Assets[0].URL: standard}
	for _, name := range []string{"jetpack5", "jetpack6"} {
		extra := runtimeCompanions["linux/arm64"][name]
		data := runtimeCompressed(t, extra.Format, runtimeTar(t, companionMembers(name)...))
		url := "https://github.com/ollama/ollama/releases/download/v99.12.3/" + extra.Asset
		r.Assets = append(r.Assets, runtimeAsset{Name: extra.Asset, Size: int64(len(data)), Digest: fmt.Sprintf("sha256:%x", sha256.Sum256(data)), URL: url})
		bodies[url] = data
	}
	metadata, _ := json.Marshal(r)
	var downloads []string
	i.client = &http.Client{Transport: runtimeTransport(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() == runtimeReleaseURL {
			return runtimeResponse(200, metadata), nil
		}
		if body, ok := bodies[req.URL.String()]; ok {
			downloads = append(downloads, filepath.Base(req.URL.Path))
			return runtimeResponse(200, body), nil
		}
		t.Errorf("unexpected download %s", req.URL)
		return runtimeResponse(404, nil), nil
	})}
	return i, &downloads
}

// A Jetson upgraded from L4T R35 to R36 needs the JetPack 6 bundle: a tree
// carrying JetPack 5 is reported incomplete and a new tree is built beside it.
func TestRuntimeInstallerSwitchesJetPackBundles(t *testing.T) {
	i, downloads := jetpackSwitchFixture(t)
	i.Hardware = companionHardware("jetpack5")
	five, err := i.Ensure(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	fiveBinary, err := os.ReadFile(five.Binary)
	if err != nil {
		t.Fatal(err)
	}
	i.Hardware = companionHardware("jetpack6")
	found, err := i.Find()
	if err != nil || found.MissingCompanion != "JetPack 6 bundle for NVIDIA Jetson, L4T R36" {
		t.Fatalf("Find on a JetPack 5 tree after R36 = %+v, %v; want JetPack 6 reported missing", found, err)
	}
	*downloads = nil
	six, err := i.Ensure(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if six.Binary == five.Binary || !strings.Contains(installedTree(t, i, six), "jetpack6") {
		t.Fatalf("R36 kept the JetPack 5 tree: %+v", six)
	}
	if got := strings.Join(*downloads, ","); !strings.Contains(got, "jetpack6") || strings.Contains(got, "jetpack5") {
		t.Fatalf("downloads = %s; want the JetPack 6 bundle only as the companion", got)
	}
	if after, err := os.ReadFile(five.Binary); err != nil || string(after) != string(fiveBinary) {
		t.Fatalf("the JetPack 5 tree changed: %v", err)
	}
}

// Cancelling setup mid-upgrade is a cancellation, not a companion that could
// not be added: Ensure returns it, emits no note and keeps the pointer.
func TestRuntimeInstallerCancelledUpgradeIsNotAFallback(t *testing.T) {
	f := companionInstallFixture(t, "linux/amd64", "rocm", companionMembers("rocm")...)
	amd := f.installer.Hardware
	f.installer.Hardware = nil
	if _, err := f.installer.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	pointer := filepath.Join(f.installer.Dir, f.installer.currentRecordName())
	before, _ := os.ReadFile(pointer)
	f.installer.Hardware = amd
	base := f.installer.client.Transport
	f.installer.client = &http.Client{Transport: runtimeTransport(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() == runtimeReleaseURL {
			return base.RoundTrip(req)
		}
		<-req.Context().Done()
		return nil, req.Context().Err()
	})}
	var notes []string
	f.installer.Progress = func(p RuntimeProgress) {
		if p.Stage == "note" {
			notes = append(notes, p.Note)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	if _, err := f.installer.Ensure(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled upgrade = %v; want context.Canceled", err)
	}
	if len(notes) != 0 {
		t.Errorf("a cancellation was reported as a companion that could not be added: %q", notes)
	}
	if after, _ := os.ReadFile(pointer); string(after) != string(before) {
		t.Error("a cancelled upgrade moved the installation pointer")
	}
}

// A completed tree left without its pointer is reused only after its record
// validates; a damaged one is refused, never pointed at.
func TestRuntimeInstallerRefusesADamagedTreeWithoutAPointer(t *testing.T) {
	f := companionInstallFixture(t, "linux/amd64", "rocm", companionMembers("rocm")...)
	host, err := f.installer.Ensure(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	pointer := filepath.Join(f.installer.Dir, f.installer.currentRecordName())
	if err := os.Remove(pointer); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installedTree(t, f.installer, host), ".kolk-runtime.json"), []byte(`{"version":"v1.0.0"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := f.installer.Ensure(context.Background()); err == nil {
		t.Fatalf("a damaged tree was pointed at: %+v", got)
	}
	if _, err := os.Stat(pointer); !os.IsNotExist(err) {
		t.Fatal("a pointer was written for a damaged tree")
	}
}

// releaseServer serves one release tag with the given bundle bodies (by asset
// name) and declared digests, recording every bundle download.
func releaseServer(t *testing.T, tag string, bodies, declared map[string][]byte, downloads *[]string) *http.Client {
	t.Helper()
	r := runtimeRelease{Tag: tag}
	for name, declaredBody := range declared {
		r.Assets = append(r.Assets, runtimeAsset{Name: name, Size: int64(len(bodies[name])),
			Digest: fmt.Sprintf("sha256:%x", sha256.Sum256(declaredBody)),
			URL:    "https://github.com/ollama/ollama/releases/download/" + tag + "/" + name})
	}
	metadata, _ := json.Marshal(r)
	return &http.Client{Transport: runtimeTransport(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() == runtimeReleaseURL {
			return runtimeResponse(200, metadata), nil
		}
		name := filepath.Base(req.URL.Path)
		body, ok := bodies[name]
		if !ok || !strings.Contains(req.URL.Path, "/"+tag+"/") {
			t.Errorf("unexpected download %s", req.URL)
			return runtimeResponse(404, nil), nil
		}
		*downloads = append(*downloads, name)
		return runtimeResponse(200, body), nil
	})}
}

// A companion whose content fails verification fails the same way next
// session, so a second session does not download it again for that release:
// it keeps the working runtime and says an earlier attempt failed. A new
// release is tried afresh.
func TestRuntimeInstallerDoesNotRepeatAFailedCompanionForItsRelease(t *testing.T) {
	const platform = "linux/amd64"
	standardName, rocmName := runtimePlatforms[platform].Asset, runtimeCompanions[platform]["rocm"].Asset
	i, _, standard := runtimeInstallFixture(t, platform)
	if _, err := i.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	good := runtimeCompressed(t, "tar.zst", runtimeTar(t, companionMembers("rocm")...))
	bad := append([]byte(nil), good...)
	bad[len(bad)-1] ^= 0xff
	var downloads []string
	session := func(tag string, companion []byte) (*RuntimeInstaller, *[]string) {
		downloads = nil
		next := &RuntimeInstaller{Dir: i.Dir, platform: platform, freeSpace: i.freeSpace, Hardware: companionHardware("rocm"),
			client: releaseServer(t, tag, map[string][]byte{standardName: standard, rocmName: companion},
				map[string][]byte{standardName: standard, rocmName: good}, &downloads)}
		return next, &downloads
	}
	var notes []string
	note := func(p RuntimeProgress) {
		if p.Stage == "note" {
			notes = append(notes, p.Note)
		}
	}

	first, got := session("v99.12.3", bad)
	first.Progress = note
	if _, err := first.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(*got) != 2 || len(notes) != 1 {
		t.Fatalf("first session: downloads=%v notes=%q; want both bundles tried and one note", *got, notes)
	}

	notes = nil
	second, got := session("v99.12.3", bad)
	second.Progress = note
	if _, err := second.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(*got) != 0 {
		t.Fatalf("second session downloaded %v again for a release that already failed verification", *got)
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "earlier attempt") || !strings.Contains(notes[0], "ROCm bundle") {
		t.Fatalf("second session notes = %q; want one saying an earlier attempt failed", notes)
	}

	third, got := session("v99.12.4", good)
	host, err := third.Ensure(context.Background())
	if err != nil || !strings.Contains(installedTree(t, third, host), "rocm") || len(*got) != 2 {
		t.Fatalf("a new release was not tried afresh: host=%+v err=%v downloads=%v", host, err, *got)
	}
}

// A download interrupted by the network says nothing about the release, so
// the next session tries it again.
func TestRuntimeInstallerRetriesACompanionAfterATransportFailure(t *testing.T) {
	f := companionInstallFixture(t, "linux/amd64", "rocm", companionMembers("rocm")...)
	amd := f.installer.Hardware
	f.installer.Hardware = nil
	if _, err := f.installer.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.installer.Hardware = amd
	base := f.installer.client.Transport
	rocm := runtimeCompanions["linux/amd64"]["rocm"].Asset
	var companionTries atomic.Int32
	f.installer.client = &http.Client{Transport: runtimeTransport(func(req *http.Request) (*http.Response, error) {
		if strings.HasSuffix(req.URL.Path, rocm) {
			companionTries.Add(1)
			return nil, fmt.Errorf("connection reset by peer")
		}
		return base.RoundTrip(req)
	})}
	for n := 0; n < 2; n++ {
		if _, err := f.installer.Ensure(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if companionTries.Load() != 2 {
		t.Fatalf("companion tried %d times over two sessions; a transport failure must be retried", companionTries.Load())
	}
}

// Running out of disk while unpacking says nothing about the release: once
// space is freed, the next session downloads and installs it.
func TestRuntimeInstallerDoesNotRememberRunningOutOfRoom(t *testing.T) {
	f := companionInstallFixture(t, "linux/amd64", "rocm", companionMembers("rocm")...)
	amd := f.installer.Hardware
	f.installer.Hardware = nil
	if _, err := f.installer.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.installer.Hardware = amd
	// The check before downloading passes; unpacking then finds no room.
	var checks atomic.Int32
	f.installer.freeSpace = func(string) (uint64, bool) {
		if checks.Add(1) == 1 {
			return 32 << 30, true
		}
		return 64<<20 + 1, true
	}
	if _, err := f.installer.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	*f.downloads = nil
	f.installer.freeSpace = func(string) (uint64, bool) { return 32 << 30, true }
	host, err := f.installer.Ensure(context.Background())
	if err != nil || !strings.Contains(installedTree(t, f.installer, host), "rocm") || len(*f.downloads) != 2 {
		t.Fatalf("after freeing space: host=%+v err=%v downloads=%v; want the release downloaded and installed", host, err, *f.downloads)
	}
}

// A remembered failure is visible, so status can say why the bundle is still
// missing, and an explicit setup can forget it and try the release again.
func TestRuntimeInstallerReportsAndForgetsARememberedFailure(t *testing.T) {
	f := companionInstallFixture(t, "linux/amd64", "rocm", companionMembers("rocm")...)
	amd := f.installer.Hardware
	f.installer.Hardware = nil
	if _, err := f.installer.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.installer.Hardware = amd
	base := f.installer.client.Transport
	rocm := runtimeCompanions["linux/amd64"]["rocm"].Asset
	tampered := true
	f.installer.client = &http.Client{Transport: runtimeTransport(func(req *http.Request) (*http.Response, error) {
		if tampered && strings.HasSuffix(req.URL.Path, rocm) {
			bad := append([]byte(nil), f.companion...)
			bad[len(bad)-1] ^= 0xff
			return runtimeResponse(200, bad), nil
		}
		return base.RoundTrip(req)
	})}
	if _, err := f.installer.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	found, err := f.installer.Find()
	if err != nil || found.MissingCompanion == "" || !strings.Contains(found.CompanionFailure, "SHA-256") || found.UnusableVendor() != "amd" {
		t.Fatalf("Find after a remembered failure = %+v, %v; want the missing bundle, why it failed, and AMD unserved", found, err)
	}
	if err := f.installer.ForgetFailures(context.Background()); err != nil {
		t.Fatal(err)
	}
	if found, _ := f.installer.Find(); found.CompanionFailure != "" {
		t.Fatalf("a forgotten failure is still reported: %+v", found)
	}
	tampered = false
	*f.downloads = nil
	host, err := f.installer.Ensure(context.Background())
	if err != nil || !strings.Contains(installedTree(t, f.installer, host), "rocm") {
		t.Fatalf("after forgetting, the release was not tried again: %+v, %v (downloads %v)", host, err, *f.downloads)
	}
}

// A reused persistent runtime that died leaves no gap behind: when the user's
// own server answers instead, nothing is reported missing from it.
func TestHostStarterAdoptingAUserServerClearsTheMissingCompanion(t *testing.T) {
	f := newStarterFixture(0, 43111)
	f.starter.Persistent = true
	f.starter.addr, f.starter.managed, f.starter.missing = "127.0.0.1:43218", true, "ROCm bundle for AMD GPU card0"
	f.starter.Discover = func(context.Context) Host {
		return Host{State: HostRunning, Addr: "127.0.0.1:11434", Version: "0.34.4"}
	}
	if _, err := f.starter.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := f.starter.Host(context.Background()); got.Managed || got.MissingCompanion != "" {
		t.Fatalf("the user's own server = %+v; want it unmanaged with nothing missing", got)
	}
}

// A later session reusing a persistent runtime still reports what its tree
// lacks, like the session that started it.
func TestPersistentReuseKeepsTheMissingCompanion(t *testing.T) {
	dir, project := t.TempDir(), t.TempDir()
	var starts atomic.Int32
	incomplete := func(context.Context) Host {
		return Host{State: HostInstalled, Managed: true, Binary: "/fake/ollama", MissingCompanion: "ROCm bundle for AMD GPU card0"}
	}
	first, _ := persistentFixture(t, dir, project, &starts)
	first.Discover = incomplete
	if _, err := first.EnsureInstalled(context.Background()); err != nil {
		t.Fatal(err)
	}
	second, _ := persistentFixture(t, dir, project, &starts)
	second.Discover = incomplete
	if _, err := second.EnsureInstalled(context.Background()); err != nil {
		t.Fatal(err)
	}
	if starts.Load() != 1 {
		t.Fatalf("started %d runtimes; want the second session to reuse the first", starts.Load())
	}
	if got := second.Host(context.Background()); got.MissingCompanion != "ROCm bundle for AMD GPU card0" {
		t.Fatalf("reused persistent runtime = %+v; want the missing bundle reported", got)
	}
}

// failureMarker is the one remembered failure in dir.
func failureMarker(t *testing.T, dir string) string {
	t.Helper()
	found, err := filepath.Glob(filepath.Join(dir, "failed-*.json"))
	if err != nil || len(found) != 1 {
		t.Fatalf("failure markers = %v, %v; want exactly one", found, err)
	}
	return found[0]
}

// upgradeWithTamperedCompanion installs the standard tree, then fails one
// upgrade on a tampered ROCm bundle, leaving a remembered failure. It returns
// a switch that makes the served companion good again.
func upgradeWithTamperedCompanion(t *testing.T) (companionFixture, func()) {
	t.Helper()
	f := companionInstallFixture(t, "linux/amd64", "rocm", companionMembers("rocm")...)
	amd := f.installer.Hardware
	f.installer.Hardware = nil
	if _, err := f.installer.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.installer.Hardware = amd
	base := f.installer.client.Transport
	rocm := runtimeCompanions["linux/amd64"]["rocm"].Asset
	var tampered atomic.Bool
	tampered.Store(true)
	f.installer.client = &http.Client{Transport: runtimeTransport(func(req *http.Request) (*http.Response, error) {
		if tampered.Load() && strings.HasSuffix(req.URL.Path, rocm) {
			bad := append([]byte(nil), f.companion...)
			bad[len(bad)-1] ^= 0xff
			return runtimeResponse(200, bad), nil
		}
		return base.RoundTrip(req)
	})}
	if _, err := f.installer.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	failureMarker(t, f.installer.Dir)
	return f, func() { tampered.Store(false) }
}

// A fresh installation never consults remembered failures: with no working
// runtime to fall back to, it downloads and reports the real error.
func TestFreshInstallIgnoresRememberedFailures(t *testing.T) {
	f, _ := upgradeWithTamperedCompanion(t)
	if err := os.Remove(filepath.Join(f.installer.Dir, f.installer.currentRecordName())); err != nil {
		t.Fatal(err)
	}
	*f.downloads = nil
	// The tampered companion is served by the wrapper, which records nothing;
	// the standard download and the real cause in the note show it was tried,
	// not skipped as an earlier attempt. Since F11 the first install then
	// proceeds without the bundle.
	var notes []string
	f.installer.Progress = func(p RuntimeProgress) {
		if p.Stage == "note" {
			notes = append(notes, p.Note)
		}
	}
	_, err := f.installer.Ensure(context.Background())
	if err != nil || len(*f.downloads) != 1 || len(notes) != 1 || !strings.Contains(notes[0], "SHA-256") || strings.Contains(notes[0], "earlier attempt") {
		t.Fatalf("fresh install = %v, downloads %v, notes %q; want the release tried and the real cause", err, *f.downloads, notes)
	}
}

// What docs/localia.md promises about a bundle that fails on a first install:
// the runtime is installed without it and nothing is remembered; the next
// start downloads that release again to add it and remembers a second
// failure; the start after that downloads nothing.
func TestAFirstInstallFailureIsRememberedOnlyByTheNextStart(t *testing.T) {
	f := companionInstallFixture(t, "linux/amd64", "rocm", companionMembers("rocm")...)
	base := f.installer.client.Transport
	rocm := runtimeCompanions["linux/amd64"]["rocm"].Asset
	var bundles atomic.Int32
	f.installer.client = &http.Client{Transport: runtimeTransport(func(req *http.Request) (*http.Response, error) {
		if strings.HasSuffix(req.URL.Path, rocm) {
			bundles.Add(1)
			bad := append([]byte(nil), f.companion...)
			bad[len(bad)-1] ^= 0xff
			return runtimeResponse(200, bad), nil
		}
		return base.RoundTrip(req)
	})}
	markers := func() []string {
		found, _ := filepath.Glob(filepath.Join(f.installer.Dir, "failed-*.json"))
		return found
	}

	host, err := f.installer.Ensure(context.Background())
	if err != nil || strings.Contains(installedTree(t, f.installer, host), "rocm") || bundles.Load() != 1 || len(markers()) != 0 {
		t.Fatalf("first install = %v, bundle tries %d, markers %v; want the runtime alone and nothing remembered", err, bundles.Load(), markers())
	}

	*f.downloads = nil
	if _, err := f.installer.Ensure(context.Background()); err != nil {
		t.Fatalf("the next start failed instead of keeping the runtime: %v", err)
	}
	if len(*f.downloads) != 1 || bundles.Load() != 2 || len(markers()) != 1 {
		t.Fatalf("next start downloaded %v and tried the bundle %d times, markers %v; want the release again and the failure remembered", *f.downloads, bundles.Load(), markers())
	}

	*f.downloads = nil
	if _, err := f.installer.Ensure(context.Background()); err != nil || len(*f.downloads) != 0 || bundles.Load() != 2 {
		t.Fatalf("third start = %v, downloaded %v, bundle tries %d; want nothing downloaded", err, *f.downloads, bundles.Load())
	}
}

// A first install that fails outright (a corrupt standard bundle, so no
// runtime at all) remembers nothing either: a one-off bad download must not
// hold back that release's bundle once the runtime is installed.
func TestAFailedFirstInstallRemembersNothing(t *testing.T) {
	f := companionInstallFixture(t, "linux/amd64", "rocm", companionMembers("rocm")...)
	base := f.installer.client.Transport
	standard := runtimePlatforms["linux/amd64"].Asset
	f.installer.client = &http.Client{Transport: runtimeTransport(func(req *http.Request) (*http.Response, error) {
		resp, err := base.RoundTrip(req)
		if err != nil || !strings.HasSuffix(req.URL.Path, "/"+standard) {
			return resp, err
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}
		body[len(body)-1] ^= 0xff
		return runtimeResponse(200, body), nil
	})}
	if _, err := f.installer.Ensure(context.Background()); err == nil {
		t.Fatal("a corrupt standard bundle installed a runtime")
	}
	if found, _ := filepath.Glob(filepath.Join(f.installer.Dir, "failed-*.json")); len(found) != 0 {
		t.Fatalf("a failed first install remembered %v", found)
	}
}

// Only a small regular file with a reason is a remembered failure. A symlink
// or an empty reason is ignored, and the upgrade is tried.
func TestMalformedFailureMarkersAreIgnored(t *testing.T) {
	for _, shape := range []string{"symlink", "empty reason"} {
		t.Run(shape, func(t *testing.T) {
			f, repair := upgradeWithTamperedCompanion(t)
			marker := failureMarker(t, f.installer.Dir)
			switch shape {
			case "symlink":
				// A relative link inside the directory: os.Root already refuses
				// one that leaves it, or an absolute one, so this is the case
				// the regular-file check itself must refuse.
				target := filepath.Join(f.installer.Dir, "reason-copy.json")
				data, _ := os.ReadFile(marker)
				if err := os.WriteFile(target, data, 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(marker); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Base(target), marker); err != nil {
					t.Fatal(err)
				}
			case "empty reason":
				if err := os.WriteFile(marker, []byte(`{"error":""}`), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			repair()
			host, err := f.installer.Ensure(context.Background())
			if err != nil || !strings.Contains(installedTree(t, f.installer, host), "rocm") {
				t.Fatalf("with a %s marker: %+v, %v; want the upgrade tried and installed", shape, host, err)
			}
		})
	}
}

// A companion that overlaps the standard bundle fails the same way every
// time, so that release is remembered like a checksum mismatch.
func TestAnOverlappingCompanionIsRemembered(t *testing.T) {
	spec := runtimePlatforms["linux/amd64"]
	members := append(companionMembers("rocm"), archiveMember{name: spec.Binary, body: "a companion replacing the runtime", mode: 0o755})
	f := companionInstallFixture(t, "linux/amd64", "rocm", members...)
	amd := f.installer.Hardware
	f.installer.Hardware = nil
	if _, err := f.installer.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.installer.Hardware = amd
	if _, err := f.installer.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	failureMarker(t, f.installer.Dir)
	*f.downloads = nil
	if _, err := f.installer.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(*f.downloads) != 0 {
		t.Fatalf("an overlapping companion was downloaded again: %v", *f.downloads)
	}
}

// A runtime started after setup kept the old tree says why the bundle is
// missing: a failure remembered before, or one remembered by this very attempt.
func TestHostStarterCarriesARememberedFailureIntoTheRunningRuntime(t *testing.T) {
	for _, c := range []struct {
		name          string
		failedAlready bool
	}{{"remembered before", true}, {"remembered by this attempt", false}} {
		t.Run(c.name, func(t *testing.T) {
			f := newStarterFixture(0, 43111)
			f.starter.Binary = ""
			var discoveries atomic.Int32
			f.starter.Discover = func(context.Context) Host {
				host := Host{State: HostInstalled, Managed: true, Binary: "/managed/standard/ollama", MissingCompanion: "ROCm bundle for AMD GPU card0"}
				if c.failedAlready || discoveries.Add(1) > 1 {
					host.CompanionFailure = "native runtime SHA-256 verification failed"
				}
				return host
			}
			f.starter.Provision = func(context.Context, io.Writer) (Host, error) {
				return Host{State: HostInstalled, Managed: true, Binary: "/managed/standard/ollama"}, nil
			}
			if _, err := f.starter.Ensure(context.Background()); err != nil {
				t.Fatal(err)
			}
			got := f.starter.Host(context.Background())
			if got.State != HostRunning || got.MissingCompanion == "" || !strings.Contains(got.CompanionFailure, "SHA-256") {
				t.Fatalf("running runtime after a kept tree = %+v; want the missing bundle and why", got)
			}
		})
	}
}

// Disk exhaustion is never a failure of the release, however deeply the
// error is wrapped; anything else that breaks unpacking is.
func TestRuntimeContentFailureClassification(t *testing.T) {
	enospc := fmt.Errorf("unpacking: %w", errors.Join(errors.New("closing"), &os.PathError{Op: "write", Path: "lib/x.so", Err: syscall.ENOSPC}))
	room := fmt.Errorf("unpacking: %w", fmt.Errorf("%w: need 1 GiB", errRuntimeNoRoom))
	for _, err := range []error{enospc, room} {
		var content *runtimeContentError
		if errors.As(runtimeContentFailure(err), &content) {
			t.Errorf("%v was classified as the release's content", err)
		}
	}
	var content *runtimeContentError
	if !errors.As(runtimeContentFailure(errors.New("zstd: corrupt frame")), &content) {
		t.Error("a corrupt archive was not classified as content")
	}
}

// Status reports only a failure for this platform's needed companion, and
// the newest one when several releases failed.
func TestRememberedFailureMatchesPlatformAndCompanion(t *testing.T) {
	i := &RuntimeInstaller{Dir: t.TempDir(), platform: "linux/arm64"}
	write := func(name, reason string, age time.Duration) {
		path := filepath.Join(i.Dir, name)
		if err := os.WriteFile(path, []byte(`{"error":"`+reason+`"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		when := time.Now().Add(-age)
		if err := os.Chtimes(path, when, when); err != nil {
			t.Fatal(err)
		}
	}
	write("failed-v1.0.0-linux-arm64-aa-jetpack5-bb.json", "jetpack5 broke", time.Minute)
	write("failed-v1.0.0-linux-amd64-aa-rocm-bb.json", "rocm broke", time.Minute)
	if got := i.rememberedFailure("jetpack6"); got != "" {
		t.Fatalf("a JetPack 6 need reported %q from another companion", got)
	}
	if got := i.rememberedFailure("rocm"); got != "" {
		t.Fatalf("arm64 reported %q from an amd64 marker", got)
	}
	write("failed-v1.0.0-linux-arm64-aa-jetpack6-bb.json", "older", 2*time.Hour)
	write("failed-v1.1.0-linux-arm64-cc-jetpack6-dd.json", "newer", time.Minute)
	if got := i.rememberedFailure("jetpack6"); got != "newer" {
		t.Fatalf("remembered failure = %q; want the newest", got)
	}
}

// A companion archive that verifies but will not unpack fails the same way
// every time, so it is remembered.
func TestACorruptCompanionArchiveIsRemembered(t *testing.T) {
	const platform = "linux/amd64"
	standardName, rocmName := runtimePlatforms[platform].Asset, runtimeCompanions[platform]["rocm"].Asset
	i, _, standard := runtimeInstallFixture(t, platform)
	if _, err := i.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	corrupt := []byte("not a zstd frame at all")
	var downloads []string
	i.Hardware = companionHardware("rocm")
	i.client = releaseServer(t, "v99.12.3", map[string][]byte{standardName: standard, rocmName: corrupt},
		map[string][]byte{standardName: standard, rocmName: corrupt}, &downloads)
	if _, err := i.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	failureMarker(t, i.Dir)
}

// A runtime started from an installed tree reports the version discovery
// read from that tree, as one started after setup does.
func TestHostStarterKeepsTheInstalledVersion(t *testing.T) {
	f := newStarterFixture(0, 43111)
	f.starter.Binary = ""
	f.starter.Discover = func(context.Context) Host {
		return Host{State: HostInstalled, Managed: true, Binary: "/managed/ollama", Version: "v0.34.4"}
	}
	if _, err := f.starter.EnsureInstalled(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := f.starter.Host(context.Background()); got.Version != "v0.34.4" {
		t.Fatalf("running runtime = %+v; want the installed version", got)
	}
}

// A release without the companion asset still installs the standard runtime
// on a first install, as the official installer would.
func TestFreshInstallWithoutTheCompanionAssetInstallsTheRuntime(t *testing.T) {
	const platform = "linux/amd64"
	i, _, standard := runtimeInstallFixture(t, platform)
	standardName := runtimePlatforms[platform].Asset
	var downloads []string
	i.Hardware = companionHardware("rocm")
	i.client = releaseServer(t, "v99.12.3", map[string][]byte{standardName: standard}, map[string][]byte{standardName: standard}, &downloads)
	var notes []string
	i.Progress = func(p RuntimeProgress) {
		if p.Stage == "note" {
			notes = append(notes, p.Note)
		}
	}
	host, err := i.Ensure(context.Background())
	if err != nil || host.State != HostInstalled {
		t.Fatalf("a release without ROCm left no runtime: %+v, %v", host, err)
	}
	if len(downloads) != 1 || len(notes) != 1 || !strings.Contains(notes[0], "ROCm bundle") {
		t.Fatalf("downloads=%v notes=%q; want the standard bundle and a note about ROCm", downloads, notes)
	}
	if found, _ := i.Find(); found.MissingCompanion == "" {
		t.Fatalf("status would not report the missing bundle: %+v", found)
	}
}

// A merge that fails part-way has already moved some of the bundle into the
// staging tree; a first install rebuilds the standard tree before publishing
// it, so no fragment of the dropped bundle ships.
func TestFreshInstallRebuildsTheTreeAfterAPartialMerge(t *testing.T) {
	members := []archiveMember{
		{name: "lib/ollama/aaa_gpu/", kind: tar.TypeDir, mode: 0o755},
		{name: "lib/ollama/aaa_gpu/libggml-gpu.so", body: "moved before the overlap", mode: 0o644},
		{name: "lib/ollama/libfixture.so", body: "overlaps the standard library", mode: 0o644},
	}
	f := companionInstallFixture(t, "linux/amd64", "rocm", members...)
	host, err := f.installer.Ensure(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	tree := installedTree(t, f.installer, host)
	if _, err := os.Stat(filepath.Join(tree, "lib/ollama/aaa_gpu")); !os.IsNotExist(err) {
		t.Fatal("a fragment of the dropped companion was published")
	}
	if b, err := os.ReadFile(filepath.Join(tree, "lib/ollama/libfixture.so")); err != nil || string(b) != "companion library" {
		t.Fatalf("the standard library is not the verified one: %q, %v", b, err)
	}
}

// A first install that dropped its bundle (F11) is a new tree without it: the
// session still reports the bundle missing, and the GPU as unusable by the
// runtime it just started.
func TestHostStarterKeepsTheGapAfterAFirstInstallDroppedTheBundle(t *testing.T) {
	f := newStarterFixture(0, 43111)
	f.starter.Binary = ""
	var discoveries atomic.Int32
	f.starter.Discover = func(context.Context) Host {
		if discoveries.Add(1) == 1 {
			return Host{MissingCompanion: "ROCm bundle for AMD GPU card0", CompanionVendor: "amd"}
		}
		return Host{State: HostInstalled, Managed: true, Binary: "/managed/standard/ollama",
			MissingCompanion: "ROCm bundle for AMD GPU card0", CompanionVendor: "amd"}
	}
	f.starter.Provision = func(context.Context, io.Writer) (Host, error) {
		return Host{State: HostInstalled, Managed: true, Binary: "/managed/standard/ollama"}, nil
	}
	if _, err := f.starter.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := f.starter.Host(context.Background())
	if got.MissingCompanion == "" || got.UnusableVendor() != "amd" {
		t.Fatalf("after a first install without its bundle = %+v; want the gap and AMD unusable", got)
	}
}
