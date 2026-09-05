//go:build darwin || linux

package shell

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The escape tests from plan 13 §7.2, run natively on darwin and linux. Each
// one distinguishes refusals that look alike from the outside: kolk declining
// to run the command at all (exit -1, the Refusal text), the confined child
// refusing before exec (exit 125, "kolk: sandbox child"), and the kernel
// refusing the command while it ran (a real exit code and the platform's own
// phrase -- osRefusalPhrase, "Operation not permitted" on darwin, "Permission
// denied" on linux). Only the last is a sandbox. Before an enforcer exists on a
// platform, every test here sees one of the first two, which is the red.

type escapeFixture struct {
	home, root, temp, creds string
	policy                  Sandbox
}

func newEscapeFixture(t *testing.T) escapeFixture {
	t.Helper()
	home := t.TempDir()
	f := escapeFixture{
		home:  home,
		root:  filepath.Join(home, "project"),
		temp:  filepath.Join(home, "tmp"),
		creds: filepath.Join(home, ".local", "share", "kolk", "credentials.json"),
	}
	for _, d := range []string{f.root, f.temp, filepath.Join(home, ".ssh"), filepath.Dir(f.creds)} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(f.creds, []byte(`{"openrouter":"sk-canary"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	f.policy = Sandbox{Root: f.root, Temp: f.temp, Deny: CredentialDenylist(home, f.creds), Network: NetworkAllow}
	return f
}

func sandboxed(t *testing.T, policy Sandbox, command string, env ...string) Result {
	t.Helper()
	res, err := New().Run(context.Background(), Cmd{Command: command, Sandbox: &policy, Env: env, Timeout: 90 * time.Second})
	if err != nil {
		t.Fatalf("Run aborted: %v", err)
	}
	return res
}

// refusedByTheOS is the assertion that makes these tests mean something: the
// command must have RUN and been stopped by the kernel, not skipped by kolk.
func refusedByTheOS(t *testing.T, res Result, what string) {
	t.Helper()
	if res.ExitCode == -1 {
		t.Fatalf("%s: kolk declined to run the command instead of the sandbox refusing it:\n%s", what, res.Failure)
	}
	if strings.Contains(res.Output, "kolk: sandbox child") {
		t.Fatalf("%s: the confined child refused before running the command (no ruleset yet):\n%s", what, res.Output)
	}
	if res.OK() {
		t.Fatalf("%s: was ALLOWED. output:\n%s", what, res.Output)
	}
	if !strings.Contains(res.Output, osRefusalPhrase) {
		t.Fatalf("%s: failed, but not as a kernel refusal (%q absent). output:\n%s", what, osRefusalPhrase, res.Output)
	}
}

func TestEscape1_WriteOutsideRootIsRefused(t *testing.T) {
	f := newEscapeFixture(t)
	target := filepath.Join(f.home, "outside.txt")
	refusedByTheOS(t, sandboxed(t, f.policy, "echo x > "+Quote(target)), "write outside root")
	if _, err := os.Stat(target); err == nil {
		t.Fatal("the file exists; the refusal was cosmetic")
	}
}

func TestEscape2_WriteThroughASymlinkPointingOutsideIsRefused(t *testing.T) {
	f := newEscapeFixture(t)
	target := filepath.Join(f.home, "target.txt")
	if err := os.WriteFile(target, []byte("before"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(f.root, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	refusedByTheOS(t, sandboxed(t, f.policy, "echo after > "+Quote(link)), "write through symlink")
	if b, _ := os.ReadFile(target); string(b) != "before" {
		t.Fatalf("target was modified through the link: %q", b)
	}
}

func TestEscape3_DotDotTraversalIsRefused(t *testing.T) {
	f := newEscapeFixture(t)
	refusedByTheOS(t, sandboxed(t, f.policy, "echo x > "+Quote(filepath.Join(f.root, "..", "trav.txt"))), "../ traversal")
	if _, err := os.Stat(filepath.Join(f.home, "trav.txt")); err == nil {
		t.Fatal("traversal wrote a file")
	}
}

// Widen the root to the whole (fake) home. ~/.ssh is inside it now, and must
// still be refused: the denylist outranks the root, which is the entire point
// of having a denylist rather than a root.
func TestEscape4_SSHIsRefusedEvenInsideAWidenedRoot(t *testing.T) {
	f := newEscapeFixture(t)
	wide := f.policy
	wide.Root = f.home
	target := filepath.Join(f.home, ".ssh", "authorized_keys")
	refusedByTheOS(t, sandboxed(t, wide, "echo ssh-ed25519 AAAA > "+Quote(target)), "write to ~/.ssh inside widened root")
	if _, err := os.Stat(target); err == nil {
		t.Fatal("authorized_keys was written")
	}
}

func TestEscape5_ReadingTheCredentialStoreIsRefused(t *testing.T) {
	f := newEscapeFixture(t)
	res := sandboxed(t, f.policy, "cat "+Quote(f.creds))
	refusedByTheOS(t, res, "read credential store")
	if strings.Contains(res.Output, "sk-canary") {
		t.Fatal("the credential was read")
	}
}

func TestEscape7_WriteUnderTempIsAllowed(t *testing.T) {
	f := newEscapeFixture(t)
	target := filepath.Join(f.temp, "ok.txt")
	res := sandboxed(t, f.policy, "echo x > "+Quote(target))
	if !res.OK() {
		t.Fatalf("write under temp refused: exit %d\n%s\n%s", res.ExitCode, res.Failure, res.Output)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatal("reported success but wrote nothing")
	}
}

// The one that proves the sandbox is usable and not merely strict: a real
// toolchain, building and running a test binary, entirely inside the policy.
// Go's caches are pointed under Temp so the test owns every path it touches.
func TestEscape8_GoTestInsideRootPasses(t *testing.T) {
	f := newEscapeFixture(t)
	files := map[string]string{
		"go.mod":    "module fixture\n\ngo 1.21\n",
		"x.go":      "package fixture\n\nfunc Two() int { return 2 }\n",
		"x_test.go": "package fixture\n\nimport \"testing\"\n\nfunc TestTwo(t *testing.T) {\n\tif Two() != 2 {\n\t\tt.Fatal(\"no\")\n\t}\n}\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(f.root, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := "cd " + Quote(f.root) + " && go test ./..."
	res := sandboxed(t, f.policy, cmd,
		"GOCACHE="+filepath.Join(f.temp, "gocache"),
		"GOMODCACHE="+filepath.Join(f.temp, "gomod"),
		"GOPATH="+filepath.Join(f.temp, "gopath"),
		"GOTOOLCHAIN=local",
		"GOFLAGS=-mod=mod",
	)
	if !res.OK() || !strings.Contains(res.Output, "ok") {
		t.Fatalf("go test inside the sandbox failed: exit %d\n%s\n%s", res.ExitCode, res.Failure, res.Output)
	}
}

// Landlock has no deny rule: reads are granted by enumerating what is allowed.
// A denylist path two levels under the home therefore forces the enumeration
// to recurse -- grant `.local`'s siblings, then `.local/share`'s siblings, then
// refuse the store -- and the failure mode is over-denying everything nearby.
// Seatbelt expresses the same policy as a plain deny; the test is the same.
func TestEscape9_NestedDenylistPathIsRefusedWhileSiblingsStayReadable(t *testing.T) {
	f := newEscapeFixture(t)
	sibling := filepath.Join(f.home, ".local", "notes.txt")
	top := filepath.Join(f.home, "readme.txt")
	for _, p := range []string{sibling, top} {
		if err := os.WriteFile(p, []byte("readable\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	refusedByTheOS(t, sandboxed(t, f.policy, "cat "+Quote(f.creds)), "read nested credential store")
	for _, p := range []string{sibling, top} {
		res := sandboxed(t, f.policy, "cat "+Quote(p))
		if !res.OK() || !strings.Contains(res.Output, "readable") {
			t.Fatalf("over-denied: %s should be readable: exit %d\n%s\n%s", p, res.ExitCode, res.Failure, res.Output)
		}
	}
}

// listenLoopback opens a real TCP listener so the connect below has somewhere
// to go; a refusal against a closed port would prove nothing.
func listenLoopback(t *testing.T) (port string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	_, port, _ = net.SplitHostPort(ln.Addr().String())
	return port
}

// bash's /dev/tcp is the portable connect: curl fails silently under a denied
// network (exit 7, no message), and the harness needs the kernel's phrase.
func TestEscape6_TCPConnectIsRefusedWhenNetworkIsDenied(t *testing.T) {
	f := newEscapeFixture(t)
	deny := f.policy
	deny.Network = NetworkDeny
	port := listenLoopback(t)
	refusedByTheOS(t, sandboxed(t, deny, "exec 3<>/dev/tcp/127.0.0.1/"+port+" && echo connected"), "TCP connect under network=deny")
}

// The control: the same connect, allowed, must succeed -- otherwise test 6 could
// pass because the command or the listener was broken rather than refused.
func TestEscape6b_TCPConnectSucceedsWhenNetworkIsAllowed(t *testing.T) {
	f := newEscapeFixture(t)
	port := listenLoopback(t)
	res := sandboxed(t, f.policy, "exec 3<>/dev/tcp/127.0.0.1/"+port+" && echo connected")
	if !res.OK() || !strings.Contains(res.Output, "connected") {
		t.Fatalf("allowed connect failed: exit %d\n%s\n%s", res.ExitCode, res.Failure, res.Output)
	}
}
