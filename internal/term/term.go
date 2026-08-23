// Package term answers questions about the terminal kolk is attached to,
// without any dependency on how a particular OS represents one.
//
// The questions are small and the consequences are not. Whether stdout is a
// terminal decides whether `kolk key` prints a status page or reads a key from
// a pipe; whether colour is wanted decides whether a redirected log fills with
// escape sequences. Getting either wrong turns kolk into a tool that cannot be
// scripted.
package term

import (
	"os"
	"strconv"
	"strings"
)

// IsTerminal reports whether f is attached to a terminal. The platform probe
// is deliberately stricter than checking os.ModeCharDevice: /dev/null is a
// character device too, and treating it as a terminal breaks redirected use.
func IsTerminal(f *os.File) bool {
	if f == nil {
		return false
	}
	return isTerminal(f.Fd())
}

// IsStdinTerminal reports whether input is coming from a person rather than a
// pipe. This is what separates `kolk key` (show me my status) from
// `echo $KEY | kolk key` (read it from stdin).
func IsStdinTerminal() bool { return IsTerminal(os.Stdin) }

// IsStdoutTerminal reports whether output is going to a screen rather than a
// file or another program.
func IsStdoutTerminal() bool { return IsTerminal(os.Stdout) }

// CanAnimate reports whether cursor-addressed status output is safe for the
// current process. Both sides must be terminals: a person watching redirected
// input is still running a script, and a person typing into redirected output
// expects a clean log. TERM=dumb explicitly has no cursor-control contract.
func CanAnimate() bool {
	return canAnimateFor(IsStdinTerminal(), IsStdoutTerminal(), os.Getenv("TERM"))
}

func canAnimateFor(stdinTTY, stdoutTTY bool, termName string) bool {
	return stdinTTY && stdoutTTY && !strings.EqualFold(strings.TrimSpace(termName), "dumb")
}

// Color reports whether coloured output is wanted on stdout.
//
// The precedence, most specific first, follows the conventions people already
// have configured rather than inventing a kolk-specific one:
//
//	NO_COLOR       set to anything    → never (https://no-color.org)
//	KOLK_NO_COLOR  set to anything    → never
//	FORCE_COLOR    set to anything    → always, even into a pipe
//	CLICOLOR_FORCE non-zero           → always
//	TERM=dumb                         → never
//	otherwise                         → only when stdout is a terminal
//
// NO_COLOR wins over FORCE_COLOR deliberately: a person who has set NO_COLOR
// globally has stated a preference about their whole environment, and a tool
// that overrides it is a tool they have to configure twice.
func Color() bool { return colorFor(os.Getenv, IsStdoutTerminal()) }

func colorFor(getenv func(string) string, isTTY bool) bool {
	if _, set := lookup(getenv, "NO_COLOR"); set {
		return false
	}
	if _, set := lookup(getenv, "KOLK_NO_COLOR"); set {
		return false
	}
	if _, set := lookup(getenv, "FORCE_COLOR"); set {
		return true
	}
	if v, set := lookup(getenv, "CLICOLOR_FORCE"); set && v != "0" {
		return true
	}
	if strings.EqualFold(getenv("TERM"), "dumb") {
		return false
	}
	return isTTY
}

// lookup treats an empty value as set, which is what the NO_COLOR convention
// specifies: presence is the signal, not the value.
func lookup(getenv func(string) string, key string) (string, bool) {
	v := getenv(key)
	return v, v != ""
}

// DefaultWidth is used when the real width cannot be discovered — a pipe, a CI
// log, a terminal that does not say. Eighty is the value every other tool
// falls back to, so wrapped output matches what people already expect.
const DefaultWidth = 80

// Width reports the usable column count.
//
// It reads COLUMNS, which shells export and CI systems set, rather than
// performing a TIOCGWINSZ ioctl: the ioctl needs golang.org/x/sys, and this
// package exists partly to keep the root module's dependency graph empty.
// When the TUI lands it will need live resize events anyway, and that is the
// point at which the dependency earns its place.
func Width() int { return widthFor(os.Getenv) }

func widthFor(getenv func(string) string) int {
	n, err := strconv.Atoi(strings.TrimSpace(getenv("COLUMNS")))
	if err != nil || n < 20 || n > 1000 {
		return DefaultWidth
	}
	return n
}
