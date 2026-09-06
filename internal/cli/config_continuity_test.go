package cli

import (
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/config"
)

// /config knows the four continuity keys, validates their words, stores the
// preferred list as given, and shows the old routing knobs as deprecated
// aliases.
func TestConfigContinuityKeys(t *testing.T) {
	d := isolateHome(t)
	a, out, _ := newTestApp(t, "")
	for _, args := range [][]string{
		{"config", "set", "continuity.mode", "on"},
		{"config", "set", "continuity.select", "preferred"},
		{"config", "set", "continuity.preferred", "ChatGPT Plus/gpt-5.6-sol, gemini-2.5-pro"},
		{"config", "set", "continuity.order", "paid,subs,free"},
	} {
		if code := runRetiredVerb(t, a, args...); code != ExitOK {
			t.Fatalf("%v exit = %d: %s", args, code, out.String())
		}
	}
	cfg, err := config.Load(d.ConfigFile())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Continuity.Mode != "on" || cfg.Continuity.Select != "preferred" || len(cfg.Continuity.Preferred) != 2 || cfg.Continuity.Preferred[1] != "gemini-2.5-pro" || strings.Join(cfg.Continuity.Order, ",") != "paid,subscription,free" {
		t.Fatalf("saved continuity = %+v", cfg.Continuity)
	}
	if code := runRetiredVerb(t, a, "config", "set", "continuity.select", "sometimes"); code == ExitOK {
		t.Fatal("a bad select word was accepted")
	}
	if code := runRetiredVerb(t, a, "config", "set", "continuity.order", "paid,paid"); code == ExitOK {
		t.Fatal("a bad order was accepted")
	}
	out.Reset()
	if code := runRetiredVerb(t, a, "config", "get", "routing.on_subscription_limit"); code != ExitOK {
		t.Fatal("get failed")
	}
	if !strings.Contains(out.String(), "deprecated") || !strings.Contains(out.String(), "continuity.mode") {
		t.Fatalf("the old knob does not say it is an alias: %q", out.String())
	}
	out.Reset()
	if code := runRetiredVerb(t, a, "config", "unset", "continuity.preferred"); code != ExitOK {
		t.Fatal("unset failed")
	}
	cfg, _ = config.Load(d.ConfigFile())
	if len(cfg.Continuity.Preferred) != 0 {
		t.Fatalf("preferred not cleared: %+v", cfg.Continuity)
	}
}
