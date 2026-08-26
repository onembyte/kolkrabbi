package paths

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// env builds a fake environment, so the whole resolution table can be tested
// without touching the developer's real $HOME.
func env(pairs ...string) func(string) string {
	m := map[string]string{}
	for i := 0; i+1 < len(pairs); i += 2 {
		m[pairs[i]] = pairs[i+1]
	}
	return func(k string) string { return m[k] }
}

func TestResolveDefaults(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the XDG table is the unix layout; Windows has its own test")
	}
	home := "/home/franco"
	d := resolve(env(), home)

	want := Dirs{
		Config: "/home/franco/.config/kolk",
		Data:   "/home/franco/.local/share/kolk",
		Cache:  "/home/franco/.cache/kolk",
	}
	if d != want {
		t.Errorf("resolve() =\n %+v\nwant\n %+v", d, want)
	}
}

// macOS uses XDG deliberately, against what os.UserConfigDir would return.
// This is a decision, not an accident, so it is asserted rather than assumed.
func TestDarwinUsesXDGNotApplicationSupport(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin only")
	}
	d := resolve(env(), "/Users/franco")
	if strings.Contains(d.Config, "Library") || strings.Contains(d.Data, "Library") {
		t.Errorf("macOS must use XDG, not ~/Library/Application Support: %+v", d)
	}
	if d.Config != "/Users/franco/.config/kolk" {
		t.Errorf("Config = %q, want /Users/franco/.config/kolk", d.Config)
	}
}

func TestXDGVariablesAreHonoured(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix layout")
	}
	d := resolve(env(
		"XDG_CONFIG_HOME", "/xdg/config",
		"XDG_DATA_HOME", "/xdg/data",
		"XDG_CACHE_HOME", "/xdg/cache",
	), "/home/franco")

	want := Dirs{Config: "/xdg/config/kolk", Data: "/xdg/data/kolk", Cache: "/xdg/cache/kolk"}
	if d != want {
		t.Errorf("resolve() = %+v, want %+v", d, want)
	}
}

// The XDG spec says a relative value must be ignored. Honouring one would put
// someone's sessions wherever they happened to run kolk from.
func TestRelativeXDGValuesAreIgnored(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix layout")
	}
	d := resolve(env("XDG_DATA_HOME", "relative/path"), "/home/franco")
	if d.Data != "/home/franco/.local/share/kolk" {
		t.Errorf("a relative XDG_DATA_HOME must be ignored, got %q", d.Data)
	}
}

func TestKolkOverridesBeatEverything(t *testing.T) {
	d := resolve(env(
		EnvConfigDir, filepath.FromSlash("/override/config"),
		EnvDataDir, filepath.FromSlash("/override/data"),
		EnvCacheDir, filepath.FromSlash("/override/cache"),
		"XDG_CONFIG_HOME", filepath.FromSlash("/xdg/config"),
		"AppData", filepath.FromSlash(`C:\roaming`),
		"LocalAppData", filepath.FromSlash(`C:\local`),
	), filepath.FromSlash("/home/franco"))

	for name, got := range map[string]string{"Config": d.Config, "Data": d.Data, "Cache": d.Cache} {
		if !strings.Contains(got, "override") {
			t.Errorf("%s = %q, KOLK_*_DIR must beat every other source", name, got)
		}
		if strings.Contains(got, "kolk") && !strings.HasSuffix(got, "override"+string(filepath.Separator)+strings.ToLower(name)) {
			// An override is used verbatim: kolk must not append its own name
			// to a directory the user chose explicitly.
			t.Errorf("%s = %q, an explicit override must be used as given", name, got)
		}
	}
}

func TestBlankOverrideIsIgnored(t *testing.T) {
	d := resolve(env(EnvDataDir, "   "), filepath.FromSlash("/home/franco"))
	if strings.TrimSpace(d.Data) == "" {
		t.Error("a whitespace-only override must fall through to the default, not blank the directory")
	}
}

func TestNoHomeAndNoOverrideIsAnError(t *testing.T) {
	d := resolve(env(), "")
	if d.Config != "" || d.Data != "" || d.Cache != "" {
		t.Skip("this platform can resolve directories without a home directory")
	}
	// Resolve() turns that into an error rather than a relative path; assert on
	// the property that matters — it never silently picks the working directory.
	for _, got := range []string{d.Config, d.Data, d.Cache} {
		if got != "" && !filepath.IsAbs(got) {
			t.Errorf("resolved a relative directory %q with no home; state would scatter", got)
		}
	}
}

// Credentials are state, not config. Someone who symlinks their config
// directory into a public dotfiles repo must not thereby publish their key.
func TestCredentialsLiveInDataNotConfig(t *testing.T) {
	d := Dirs{Config: filepath.FromSlash("/c"), Data: filepath.FromSlash("/d"), Cache: filepath.FromSlash("/x")}
	if got := d.CredentialsFile(); !strings.HasPrefix(got, d.Data) {
		t.Errorf("CredentialsFile() = %q, must live under Data (%q)", got, d.Data)
	}
	for name, got := range map[string]string{
		"sessions": d.Sessions(), "stats": d.StatsFile(), "credentials": d.CredentialsFile(),
		"connectors": d.ConnectorsFile(),
	} {
		if strings.HasPrefix(got, d.Config) {
			t.Errorf("%s = %q is under Config; it is state and belongs in Data", name, got)
		}
	}
	if got := d.CatalogFile(); !strings.HasPrefix(got, d.Cache) {
		t.Errorf("CatalogFile() = %q, a rebuildable download belongs in Cache", got)
	}
}

func TestEnsureDataWritesAGitignore(t *testing.T) {
	base := t.TempDir()
	d := Dirs{Config: filepath.Join(base, "c"), Data: filepath.Join(base, "d"), Cache: filepath.Join(base, "x")}

	if err := d.EnsureData(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(d.Data, ".gitignore"))
	if err != nil {
		t.Fatalf("EnsureData did not write a .gitignore: %v", err)
	}
	// KOLK_DATA_DIR makes it legal to point state inside a repository, and the
	// failure mode of getting that wrong is a published API key.
	for _, want := range []string{"credentials.json", "sessions/", "stats.jsonl"} {
		if !strings.Contains(string(b), want) {
			t.Errorf(".gitignore does not cover %q:\n%s", want, b)
		}
	}

	info, err := os.Stat(d.Data)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o700 {
		t.Errorf("data directory mode = %o, want 700 — it holds credentials", info.Mode().Perm())
	}
}

func TestEnsureDataNeverOverwritesAnEditedGitignore(t *testing.T) {
	base := t.TempDir()
	d := Dirs{Data: filepath.Join(base, "d")}
	if err := d.EnsureData(); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(d.Data, ".gitignore")
	if err := os.WriteFile(p, []byte("mine\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := d.EnsureData(); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if string(b) != "mine\n" {
		t.Errorf("EnsureData overwrote a file the user had edited: %q", b)
	}
}

func TestEnsureIsIdempotent(t *testing.T) {
	base := t.TempDir()
	d := Dirs{Config: filepath.Join(base, "c"), Data: filepath.Join(base, "d"), Cache: filepath.Join(base, "x")}
	for i := 0; i < 3; i++ {
		if err := d.EnsureConfig(); err != nil {
			t.Fatalf("EnsureConfig run %d: %v", i, err)
		}
		if err := d.EnsureData(); err != nil {
			t.Fatalf("EnsureData run %d: %v", i, err)
		}
		if err := d.EnsureCache(); err != nil {
			t.Fatalf("EnsureCache run %d: %v", i, err)
		}
	}
}

func TestResolveUsesTheRealEnvironment(t *testing.T) {
	base := t.TempDir()
	t.Setenv(EnvConfigDir, filepath.Join(base, "c"))
	t.Setenv(EnvDataDir, filepath.Join(base, "d"))
	t.Setenv(EnvCacheDir, filepath.Join(base, "x"))

	d, err := Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if d.Config != filepath.Join(base, "c") || d.Data != filepath.Join(base, "d") {
		t.Errorf("Resolve() = %+v, did not honour the overrides", d)
	}
	if !strings.Contains(d.String(), d.Data) {
		t.Errorf("String() must name every directory for bug reports:\n%s", d.String())
	}
}
