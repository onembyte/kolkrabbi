package engine

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/onembyte/kolkrabbi/internal/tools"
)

// Permission is how much Kolkrabbi may do without asking.
//
// The three tiers are the whole model: there is no separate "off switch", and
// no tier removes the floor below. `--yolo` used to be that off switch, and an
// agent that cannot refuse anything is not one you can leave running.
type Permission string

const (
	// PermissionAsk confirms anything that changes the machine.
	PermissionAsk Permission = "ask"
	// PermissionAutoApprove lets edits inside the project flow, and still asks
	// before running a shell command or reaching outside the project.
	PermissionAutoApprove Permission = "auto-approve"
	// PermissionFullAuto stops asking. It does not stop refusing.
	PermissionFullAuto Permission = "full-auto"
)

// DefaultPermission is what a session starts with when nothing says otherwise.
const DefaultPermission = PermissionAsk

// Verdict is what a policy decided about one action.
type Verdict int

const (
	// VerdictAllow proceeds without interrupting anyone.
	VerdictAllow Verdict = iota
	// VerdictAsk needs a human answer before proceeding.
	VerdictAsk
	// VerdictDeny refuses in every tier. It is a floor, not a preference.
	VerdictDeny
)

func (v Verdict) String() string {
	switch v {
	case VerdictAllow:
		return "allow"
	case VerdictAsk:
		return "ask"
	default:
		return "deny"
	}
}

// NormalizePermission accepts the spellings people actually type.
func NormalizePermission(name string) (Permission, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "ask", "confirm":
		return PermissionAsk, true
	case "auto-approve", "auto_approve", "autoapprove", "auto", "accept-edits":
		return PermissionAutoApprove, true
	case "full-auto", "full_auto", "fullauto", "full":
		return PermissionFullAuto, true
	default:
		return "", false
	}
}

// Judge decides one action, and always returns the reason with it: a refusal
// nobody can explain reads as a bug, and an allowance nobody can see is how
// autonomy becomes untraceable.
func (p Permission) Judge(r tools.Request) (Verdict, string) {
	return p.judgeWith(nil, r)
}

// judgeWith is Judge with the user's standing rules applied. The order is the
// whole design: the floor cannot be argued with, a rule beats the tier because
// the user wrote it down deliberately, and the tier is what is left.
func (p Permission) judgeWith(rules Rules, r tools.Request) (Verdict, string) {
	// The floor is checked first and applies to every tier, including full-auto.
	if reason, blocked := hardline(r); blocked {
		return VerdictDeny, reason
	}

	if rule, matched := rules.Match(r); matched {
		return rule.Decision, fmt.Sprintf("rule %q", rule.Source)
	}

	if r.Outside {
		reason := fmt.Sprintf("%s reaches outside the project: %s", r.Tool, r.Display)
		if p == PermissionFullAuto {
			return VerdictAllow, reason
		}
		return VerdictAsk, reason
	}

	switch r.Tool {
	case "read_file", "list_dir":
		// Reading inside the project is the bulk of the work and carries no
		// risk the user did not accept by running Kolkrabbi here.
		return VerdictAllow, ""
	case "write_file", "edit_file":
		if p == PermissionAsk {
			return VerdictAsk, "changes a file"
		}
		return VerdictAllow, ""
	case "bash":
		if p == PermissionFullAuto {
			return VerdictAllow, "runs a shell command"
		}
		// auto-approve deliberately stops here: an edit is visible and
		// reversible through checkpoints, a command is neither.
		return VerdictAsk, "runs a shell command"
	default:
		return VerdictAsk, "unrecognised tool"
	}
}

// credentialDirs are never read or written by a tool, in any tier. Reading one
// is the exfiltration path that matters: the contents go into the conversation
// and out with the next request, automatically.
var credentialDirs = []string{
	".ssh", ".aws", ".gnupg", ".docker",
}

// credentialFiles are named outright because they are not inside a directory
// that is obviously secret.
var credentialFiles = []string{
	"credentials.json", "id_rsa", "id_ed25519", ".netrc", ".npmrc", ".pypirc",
}

// systemDirs are refused for writes: nothing a coding agent legitimately does
// needs them, and a mistake there is not recoverable from a checkpoint.
var systemDirs = []string{"/etc", "/usr", "/bin", "/sbin", "/boot", "/sys", "/proc", "/dev"}

// hardline is the floor: actions refused in every tier because no plausible
// task needs them and each one is either unrecoverable or a credential theft.
//
// It is deliberately a short list of specific shapes rather than an attempt at
// a perimeter. The jail and the tiers are the control; this catches the few
// things that must never happen even when someone has said "stop asking".
func hardline(r tools.Request) (string, bool) {
	if r.Path != "" {
		clean := filepath.ToSlash(filepath.Clean(r.Path))
		for _, dir := range credentialDirs {
			if pathHasSegment(clean, dir) {
				return fmt.Sprintf("%s holds credentials; Kolkrabbi never reads or writes it", dir), true
			}
		}
		base := filepath.Base(clean)
		for _, name := range credentialFiles {
			if strings.EqualFold(base, name) {
				return fmt.Sprintf("%s holds credentials; Kolkrabbi never reads or writes it", base), true
			}
		}
		if r.Tool == "write_file" || r.Tool == "edit_file" {
			for _, dir := range systemDirs {
				if clean == dir || strings.HasPrefix(clean, dir+"/") {
					return fmt.Sprintf("%s is a system directory; a mistake there is not recoverable", dir), true
				}
			}
		}
	}
	if r.Command != "" {
		if reason, blocked := hardlineCommand(r.Command); blocked {
			return reason, true
		}
	}
	return "", false
}

func pathHasSegment(clean, segment string) bool {
	for _, part := range strings.Split(clean, "/") {
		if strings.EqualFold(part, segment) {
			return true
		}
	}
	return false
}

// hardlineCommand looks at what a command would actually do. It reads the
// command's own words rather than substrings, so a command that merely mentions
// a dangerous string — writing documentation about `rm -rf /`, grepping for
// "sudo" — is not caught.
func hardlineCommand(command string) (string, bool) {
	words := strings.Fields(command)
	if len(words) == 0 {
		return "", false
	}
	lowered := strings.ToLower(command)

	for i, word := range words {
		switch {
		case i == 0 && word == "sudo":
			return "sudo escalates beyond what Kolkrabbi was given", true
		case word == "mkfs" || strings.HasPrefix(word, "mkfs."):
			return "mkfs destroys a filesystem", true
		}
	}
	if words[0] == "dd" && strings.Contains(lowered, "of=/dev/") {
		return "dd writing to a device destroys it", true
	}
	if reason, blocked := dangerousRemove(words); blocked {
		return reason, true
	}
	// A download piped into a shell executes code nobody reviewed.
	if (strings.Contains(lowered, "curl ") || strings.Contains(lowered, "wget ")) &&
		pipesIntoShell(lowered) {
		return "piping a download into a shell runs code nobody reviewed", true
	}
	if strings.HasPrefix(lowered, "git push") &&
		(strings.Contains(lowered, " --force") || strings.Contains(lowered, " -f")) &&
		!strings.Contains(lowered, "--force-with-lease") {
		return "a force push can discard work that is not yours", true
	}
	return "", false
}

// dangerousRemove catches a recursive delete aimed at a root, a home, or the
// whole filesystem, without catching the ordinary `rm -rf ./build`.
func dangerousRemove(words []string) (string, bool) {
	if words[0] != "rm" {
		return "", false
	}
	recursive := false
	for _, word := range words[1:] {
		if strings.HasPrefix(word, "-") && strings.Contains(word, "r") {
			recursive = true
		}
	}
	if !recursive {
		return "", false
	}
	for _, word := range words[1:] {
		if strings.HasPrefix(word, "-") {
			continue
		}
		switch filepath.Clean(word) {
		case "/", "~", "$HOME", "${HOME}", "/*":
			return "a recursive delete of " + word + " is not recoverable", true
		}
		if strings.HasPrefix(word, "/") && len(strings.Split(filepath.Clean(word), "/")) <= 2 {
			return "a recursive delete of " + word + " is not recoverable", true
		}
	}
	return "", false
}

// pipesIntoShell reports whether a command pipes into an interpreter, which is
// the shape that turns a download into execution.
func pipesIntoShell(lowered string) bool {
	for _, segment := range strings.Split(lowered, "|")[1:] {
		switch strings.Fields(segment)[0] {
		case "sh", "bash", "zsh", "python", "python3", "perl", "ruby", "node":
			return true
		}
	}
	return false
}
