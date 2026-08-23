package term

import (
	"os"
	"path/filepath"
	"testing"
)

func env(pairs ...string) func(string) string {
	m := map[string]string{}
	for i := 0; i+1 < len(pairs); i += 2 {
		m[pairs[i]] = pairs[i+1]
	}
	return func(k string) string { return m[k] }
}

func TestColorPrecedence(t *testing.T) {
	cases := []struct {
		name  string
		env   func(string) string
		isTTY bool
		want  bool
	}{
		{"a terminal gets colour", env(), true, true},
		{"a pipe does not", env(), false, false},
		{"NO_COLOR wins on a terminal", env("NO_COLOR", "1"), true, false},
		{"NO_COLOR wins even over FORCE_COLOR", env("NO_COLOR", "1", "FORCE_COLOR", "1"), true, false},
		{"KOLK_NO_COLOR wins too", env("KOLK_NO_COLOR", "1"), true, false},
		{"FORCE_COLOR reaches a pipe", env("FORCE_COLOR", "1"), false, true},
		{"CLICOLOR_FORCE=1 reaches a pipe", env("CLICOLOR_FORCE", "1"), false, true},
		{"CLICOLOR_FORCE=0 does not", env("CLICOLOR_FORCE", "0"), false, false},
		{"TERM=dumb refuses", env("TERM", "dumb"), true, false},
		{"TERM=DUMB refuses too", env("TERM", "DUMB"), true, false},
		{"TERM=xterm is fine", env("TERM", "xterm-256color"), true, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := colorFor(c.env, c.isTTY); got != c.want {
				t.Errorf("colorFor = %v, want %v", got, c.want)
			}
		})
	}
}

func TestWidth(t *testing.T) {
	cases := map[string]int{
		"":             DefaultWidth,
		"not-a-number": DefaultWidth,
		"0":            DefaultWidth, // a nonsense width would wrap every line to nothing
		"5":            DefaultWidth,
		"100000":       DefaultWidth,
		"120":          120,
		" 100 ":        100,
	}
	for in, want := range cases {
		if got := widthFor(env("COLUMNS", in)); got != want {
			t.Errorf("widthFor(COLUMNS=%q) = %d, want %d", in, got, want)
		}
	}
}

// A regular file and a nil handle are the two things that must never be
// mistaken for a terminal: `kolk key > file` and `kolk key | cat` both depend
// on it.
func TestIsTerminalOnAFileAndOnNil(t *testing.T) {
	p := filepath.Join(t.TempDir(), "out")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	if IsTerminal(f) {
		t.Error("a regular file was reported as a terminal")
	}
	if IsTerminal(nil) {
		t.Error("a nil file was reported as a terminal")
	}
}

func TestIsTerminalOnAPipe(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close(); _ = w.Close() }()

	if IsTerminal(r) || IsTerminal(w) {
		t.Error("a pipe was reported as a terminal")
	}
}

// A character device is not necessarily a terminal. /dev/null (or NUL on
// Windows) is the counterexample that prevents IsTerminal from using file mode
// as a tty test.
func TestIsTerminalRejectsTheNullDevice(t *testing.T) {
	f, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	if IsTerminal(f) {
		t.Errorf("%s is a character device, not a terminal", os.DevNull)
	}
}
