package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/config"
)

// Mouse reporting is on by default and can be turned off, because it takes
// plain drag-select away from the terminal.
func TestMouseSettingIsOnByDefaultAndCanBeTurnedOff(t *testing.T) {
	a, out, _ := newTestApp(t, "")
	if err := a.runConfig(context.Background(), []string{"get", "mouse"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "on") {
		t.Fatalf("the default did not say on:\n%s", out.String())
	}
	if err := a.runConfig(context.Background(), []string{"set", "mouse", "maybe"}); err == nil {
		t.Error("an unknown mouse value was accepted")
	}
	if err := a.runConfig(context.Background(), []string{"set", "mouse", "off"}); err != nil {
		t.Fatal(err)
	}
	dirs, err := a.resolve()
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(dirs.ConfigFile())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MouseEnabled() {
		t.Fatal("mouse stayed on after being turned off")
	}
	listed := false
	for _, setting := range (&config.Config{}).Settings("m", "u") {
		if setting.Key == "mouse" {
			listed = true
		}
	}
	if !listed {
		t.Fatal("the settings table does not list mouse")
	}
}
