package tui

import (
	"strings"
	"testing"
)

// THEME: three named themes — kolkrabbi (the purple the product ships),
// nord (cool blues), quiet (no hue: bold headings, dim meta, diff colours
// only) — each with a 256-colour and a 16-colour tier, chosen by the same
// capability probe as before, and the colourless tier untouched whatever the
// theme. Appearance changes; behaviour and layout do not.
func TestThemesChangeEscapesAndNothingElse(t *testing.T) {
	t.Cleanup(func() { _ = SetTheme("kolkrabbi"); SetPalette("256") })
	SetPalette("256")
	// writeStyled is where a style becomes an escape; View is text-only by
	// design, so the theme is measured at the painter.
	render := func() string {
		var b strings.Builder
		writeStyled(&b, "heading ", styleHeading, true)
		writeStyled(&b, "brand ", stylePurple, true)
		writeStyled(&b, "meta ", styleMeta, true)
		writeStyled(&b, "+add ", styleAdd, true)
		writeStyled(&b, "-del", styleDel, true)
		return b.String()
	}
	if err := SetTheme("kolkrabbi"); err != nil {
		t.Fatal(err)
	}
	purple := render()
	if !strings.Contains(purple, "\x1b[38;5;141") {
		t.Fatalf("the default theme lost its purple:\n%q", purple)
	}
	if err := SetTheme("nord"); err != nil {
		t.Fatal(err)
	}
	nord := render()
	if strings.Contains(nord, "\x1b[38;5;141") || !strings.Contains(nord, "\x1b[38;5;110") {
		t.Fatalf("nord did not change the hue:\n%q", nord)
	}
	if err := SetTheme("quiet"); err != nil {
		t.Fatal(err)
	}
	quiet := render()
	for _, hue := range []string{"\x1b[38;5;141", "\x1b[38;5;110", "\x1b[95"} {
		if strings.Contains(quiet, hue) {
			t.Fatalf("quiet has a hue %q:\n%q", hue, quiet)
		}
	}
	if !strings.Contains(quiet, "\x1b[1m") {
		t.Fatalf("quiet lost the bold heading:\n%q", quiet)
	}
	strip := func(s string) string {
		var b strings.Builder
		for i := 0; i < len(s); i++ {
			if s[i] == 0x1b {
				for i < len(s) && s[i] != 'm' {
					i++
				}
				continue
			}
			b.WriteByte(s[i])
		}
		return b.String()
	}
	if strip(purple) != strip(nord) || strip(nord) != strip(quiet) {
		t.Fatal("a theme changed the text or the layout, not only the escapes")
	}

	SetPalette("none")
	if plain := render(); strings.Contains(plain, "\x1b[") {
		t.Fatalf("NO_COLOR with a theme still coloured:\n%q", plain)
	}
	SetPalette("16")
	_ = SetTheme("nord")
	if sixteen := render(); strings.Contains(sixteen, "38;5;") || !strings.Contains(sixteen, "\x1b[9") && !strings.Contains(sixteen, "\x1b[3") {
		t.Fatalf("the 16-colour tier of nord used 256-colour sequences:\n%q", sixteen)
	}
	if err := SetTheme("neon"); err == nil || !strings.Contains(err.Error(), "kolkrabbi, nord, quiet") {
		t.Fatalf("an unknown theme = %v, want the three named", err)
	}
	if got := Themes(); strings.Join(got, ",") != "kolkrabbi,nord,quiet" {
		t.Fatalf("Themes() = %v", got)
	}
}
