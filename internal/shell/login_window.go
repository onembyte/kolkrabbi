package shell

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// A login that kolk must not own the terminal for does not have to steal the
// user's screen either. It can run in a window of its own: kolk opens one,
// the provider CLI signs the user in inside it, the window closes by itself
// when the login returns, and the session in kolk never went anywhere.
//
// The terminal emulator that hosts the window is found from the environment
// first ($TERMINAL, then $TERM_PROGRAM), then from a list of emulators the
// common desktops ship. Whatever it is, the login is wrapped in one `sh -c`
// script so the window's lifetime is exactly the login's lifetime plus a
// short pause showing its outcome — and the vendor's exit status survives it.

// ErrNoTerminal says no terminal emulator could be found to host a login
// window. Callers fall back to handing over this terminal instead.
var ErrNoTerminal = errors.New("no terminal emulator found to open a login window; set $TERMINAL")

// emulatorFlags are the arguments each known emulator needs before its
// command. A missing entry for a user-named emulator falls back to -e.
var emulatorFlags = map[string][]string{
	"kitty":          nil,
	"alacritty":      {"-e"},
	"foot":           nil,
	"footclient":     nil,
	"ghostty":        {"-e"},
	"gnome-terminal": {"--"},
	"konsole":        {"-e"},
	"wezterm":        {"start", "--"},
	"st":             {"-e"},
	"xterm":          {"-e"},
	"urxvt":          {"-e"},
}

// TerminalEmulator finds a terminal emulator to host a child in. The result
// is the emulator executable, the user's own prefix arguments from $TERMINAL
// (options like --single-instance), and whether anything was found.
func TerminalEmulator() (string, []string, bool) {
	if name, prefix, ok := terminalFromEnv("TERMINAL"); ok {
		return name, prefix, true
	}
	// TERM_PROGRAM is set by most GUI terminals on macOS and by some on Linux.
	// Only a program that can host a child is usable here.
	if program := os.Getenv("TERM_PROGRAM"); program != "" {
		for name := range emulatorFlags {
			if strings.EqualFold(program, name) {
				if path, err := LookPath(name); err == nil {
					return path, nil, true
				}
			}
		}
	}
	for name := range emulatorFlags {
		if path, err := LookPath(name); err == nil {
			return path, nil, true
		}
	}
	return "", nil, false
}

// terminalFromEnv splits $TERMINAL into its executable and any options the
// user configured. An executable that is not on the machine is not a terminal
// this machine can open.
func terminalFromEnv(key string) (string, []string, bool) {
	fields := strings.Fields(os.Getenv(key))
	if len(fields) == 0 {
		return "", nil, false
	}
	path, err := LookPath(fields[0])
	if err != nil {
		return "", nil, false
	}
	return path, fields[1:], true
}

// LoginWindow runs the provider's login in a terminal window this process
// opens, waits for it, and returns the vendor's exit status. The window
// closes by itself shortly after the login returns; this terminal — and the
// session running it — are never touched.
func LoginWindow(ctx context.Context, executable string, args []string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	term, prefix, ok := TerminalEmulator()
	if !ok {
		return ErrNoTerminal
	}
	flags, known := emulatorFlags[filepath.Base(term)]
	if !known {
		// An emulator the table does not know got here through the user's own
		// $TERMINAL; -e is the convention most of them share.
		flags = []string{"-e"}
	}
	argv := append([]string{term}, prefix...)
	argv = append(argv, flags...)
	argv = append(argv, "sh", "-c", loginScript(executable, args))
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	// The emulator inherits this environment and hands it to the login
	// unchanged, so the denylist is applied here, one process early.
	cmd.Env = inheritedEnv(nil)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s login window exited unsuccessfully: %w", executable, err)
	}
	return nil
}

// loginScript is the sh script the login window runs: the vendor's login with
// its own prompts untouched, its exit status preserved as the window's, and a
// short pause so the result does not vanish the instant it happens.
func loginScript(executable string, args []string) string {
	words := make([]string, 0, len(args)+1)
	words = append(words, Quote(executable))
	for _, arg := range args {
		words = append(words, Quote(arg))
	}
	var out strings.Builder
	out.WriteString(strings.Join(words, " ") + `
st=$?
if [ "$st" -eq 0 ]; then
  printf '\nFinished. Closing shortly.\n'
else
  printf '\nLogin exited with an error (%s). Closing shortly.\n' "$st"
fi
sleep 3
exit "$st"
`)
	return out.String()
}
