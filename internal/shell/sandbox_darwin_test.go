//go:build darwin

package shell

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProbeReportsSeatbeltWhereSandboxExecExists(t *testing.T) {
	name, err := mechanism()
	if err != nil || name != "seatbelt" {
		t.Fatalf("mechanism() = %q, %v; want seatbelt on this machine", name, err)
	}
}

func TestProbeFailsClosedWhenSandboxExecIsMissing(t *testing.T) {
	previous := sandboxExecPath
	sandboxExecPath = filepath.Join(t.TempDir(), "no-such-sandbox-exec")
	defer func() { sandboxExecPath = previous }()

	name, err := mechanism()
	if err == nil {
		t.Fatalf("probe found %q where nothing exists", name)
	}
	if !strings.Contains(err.Error(), sandboxExecPath) {
		t.Fatalf("error %q does not name the missing binary", err)
	}
}

// The profile must name real paths (Seatbelt matches on them), put the
// denylist after the allows (last match wins), and survive a quote in a path.
func TestSeatbeltProfileResolvesSymlinksAndDeniesLast(t *testing.T) {
	home := t.TempDir()
	realRoot := filepath.Join(home, "real-project")
	link := filepath.Join(home, "project-link")
	if err := os.MkdirAll(realRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realRoot, link); err != nil {
		t.Fatal(err)
	}
	quoted := filepath.Join(home, `odd"name`)
	if err := os.MkdirAll(quoted, 0o755); err != nil {
		t.Fatal(err)
	}

	profile, err := seatbeltProfile(Sandbox{
		Root:     link,
		Temp:     filepath.Join(home, "tmp"),
		Writable: []string{quoted},
		Deny:     CredentialDenylist(home, filepath.Join(home, "creds.json")),
		Network:  NetworkAllow,
	})
	if err != nil {
		t.Fatal(err)
	}
	resolvedRoot, _ := filepath.EvalSymlinks(realRoot)
	if !strings.Contains(profile, `(allow file-write* (subpath "`+resolvedRoot+`"))`) {
		t.Fatalf("root not resolved through its symlink:\n%s", profile)
	}
	if strings.Contains(profile, link) {
		t.Fatalf("profile names the unresolved link, which Seatbelt would never match:\n%s", profile)
	}
	if !strings.Contains(profile, `odd\"name`) {
		t.Fatalf("quote in a path was not escaped:\n%s", profile)
	}
	allow := strings.Index(profile, "(allow file-read*)")
	deny := strings.Index(profile, "(deny file-read*")
	if allow < 0 || deny < 0 || deny < allow {
		t.Fatalf("denylist must come after the broad allow so it wins:\n%s", profile)
	}
	if !strings.HasPrefix(profile, "(version 1)\n(deny default)\n") {
		t.Fatalf("profile must be deny-by-default:\n%s", profile)
	}
}

func TestSeatbeltProfileRefusesARootThatDoesNotExist(t *testing.T) {
	_, err := seatbeltProfile(Sandbox{Root: filepath.Join(t.TempDir(), "gone"), Temp: t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "cannot be resolved") {
		t.Fatalf("want a refusal naming the unresolvable root, got %v", err)
	}
}
