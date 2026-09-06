package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/config"
)

// The theme is a setting with a default that needs no file, and /theme
// switches it for the session and says how to keep it.
func TestThemeSettingAndSlash(t *testing.T) {
	d := isolateHome(t)
	a, out, _ := newTestApp(t, "")
	if code := runRetiredVerb(t, a, "config", "get", "theme"); code != ExitOK || !strings.Contains(out.String(), "kolkrabbi") {
		t.Fatalf("default theme: exit %d %q", code, out.String())
	}
	out.Reset()
	if code := runRetiredVerb(t, a, "config", "set", "theme", "nord"); code != ExitOK {
		t.Fatalf("set theme exit = %d: %s", code, out.String())
	}
	cfg, _ := config.Load(d.ConfigFile())
	if cfg.Theme != "nord" {
		t.Fatalf("saved theme = %q", cfg.Theme)
	}
	if code := runRetiredVerb(t, a, "config", "set", "theme", "neon"); code == ExitOK {
		t.Fatal("an unknown theme was accepted")
	}

	isolateConnectorState(t)
	a, ag, out := replFixture(t, "")
	if a.slash(context.Background(), ag, "/theme") {
		t.Fatal("/theme must not exit")
	}
	if !strings.Contains(out.String(), "kolkrabbi") || !strings.Contains(out.String(), "nord") || !strings.Contains(out.String(), "quiet") {
		t.Fatalf("bare /theme: %q", out.String())
	}
	out.Reset()
	a.slash(context.Background(), ag, "/theme quiet")
	if !strings.Contains(out.String(), "theme: quiet") || !strings.Contains(out.String(), "/config set theme quiet") {
		t.Fatalf("/theme quiet: %q", out.String())
	}
}
