package tui

import (
	"bytes"
	"image/png"
	"strings"
	"testing"
)

func TestInlineOctopusIsTinyTransparentAndKeepsTerminalWidth(t *testing.T) {
	im, err := png.Decode(bytes.NewReader(octopusPNG))
	if err != nil {
		t.Fatal(err)
	}
	if im.Bounds().Dx() > 40 || im.Bounds().Dy() > 40 || len(octopusBase64) > 4096 {
		t.Fatal("icon is too large for a single terminal image chunk")
	}
	if _, _, _, alpha := im.At(0, 0).RGBA(); alpha != 0 {
		t.Fatal("icon has an opaque background")
	}
	SetPalette("256")
	defer SetPalette("256")
	for _, protocol := range []string{"iterm", "kitty"} {
		icon := renderOctopus("🐙 next", true, true, []string{protocol})
		if !strings.HasPrefix(icon, "  \x1b7\x1b[2D") || !strings.HasSuffix(icon, "\x1b8 next") {
			t.Fatal("image cells must be reserved before drawing, not overwritten afterwards")
		}
		m := New(Status{Mode: "code"})
		m.graphics = protocol
		m.AppendTranscript("user said 🐙\n")
		m.SetActivity(activityLine(0, "thinking"))
		view := m.renderView(80, 24, -1)
		if !strings.Contains(view, octopusBase64) || !strings.Contains(view, "user said 🐙") {
			t.Fatalf("%s did not isolate the activity image from the transcript", protocol)
		}
		plain := strings.Split(m.View(80, 24), "\n")
		for i, row := range strings.Split(view, "\n") {
			if visibleWidth(row) != cellWidth(plain[i]) {
				t.Fatalf("%s row %d changed width: %d vs %d", protocol, i, visibleWidth(row), cellWidth(plain[i]))
			}
		}
		SetPalette("none")
		if view := m.renderView(80, 24, -1); strings.Contains(view, "\x1b") {
			t.Fatal("NO_COLOR emitted terminal images or colour")
		}
		SetPalette("256")
	}
}

func TestKittyIconIsDeletedWhenActivityEndsOrRendererCloses(t *testing.T) {
	for _, finish := range []string{"render", "park", "close"} {
		var out bytes.Buffer
		r := NewRenderer(&out)
		if err := r.Start(); err != nil {
			t.Fatal(err)
		}
		view := renderOctopus("🐙 thinking", true, true, []string{"kitty"})
		if err := r.Render(nil, view); err != nil {
			t.Fatal(err)
		}
		out.Reset()
		switch finish {
		case "render":
			_ = r.Render(nil, "ready")
		case "park":
			r.Park()
		case "close":
			_ = r.Close()
		}
		if !strings.Contains(out.String(), deleteOctopus) && !strings.Contains(out.String(), freeOctopus) {
			t.Fatalf("%s left the image behind", finish)
		}
	}
}

func TestKittyIconIsUploadedOnceAcrossSpinnerFrames(t *testing.T) {
	var out bytes.Buffer
	r := NewRenderer(&out)
	_ = r.Start()
	for i := 0; i < 3; i++ {
		_ = r.Render(nil, renderOctopus(activityLine(i, "thinking"), true, true, []string{"kitty"}))
	}
	if got := strings.Count(out.String(), octopusBase64); got != 1 {
		t.Fatalf("uploaded the same image %d times, want 1", got)
	}
}

func TestParkClearsImageRowsAboveTheCursor(t *testing.T) {
	var out bytes.Buffer
	r := NewRenderer(&out)
	_ = r.Start()
	_ = r.Render(nil, renderOctopus("🐙 thinking", true, true, []string{"iterm"})+"\ncomposer\nfooter")
	out.Reset()
	r.Park()
	if !strings.Contains(out.String(), "\x1b[2A") {
		t.Fatal("Park erased only below the cursor, leaving earlier image rows behind")
	}
}
