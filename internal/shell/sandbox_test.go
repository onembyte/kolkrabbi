package shell

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// A command that asks for a sandbox on a machine that cannot provide one must
// not run. It is a Result the model can read, not an error that aborts the
// turn, because "I refused to run this" is information the conversation needs.
func TestRunRefusesASandboxedCommandWhenNoMechanismExists(t *testing.T) {
	restore := overrideMechanism(func() (string, error) { return "", ErrSandboxUnsupported })
	defer restore()

	res, err := New().Run(context.Background(), Cmd{
		Command: "echo ran",
		Sandbox: &Sandbox{Root: t.TempDir(), Temp: t.TempDir(), Network: NetworkAllow},
	})
	if err != nil {
		t.Fatalf("a refusal is a Result, not an error from Run: %v", err)
	}
	if res.OK() || res.ExitCode != -1 {
		t.Fatalf("refused command must report failure with no exit code: %+v", res)
	}
	if strings.Contains(res.Output, "ran") {
		t.Fatalf("the command ran despite the refusal: %q", res.Output)
	}
	for _, want := range []string{"sandbox could not be established", ErrSandboxUnsupported.Error(), "/sandbox off"} {
		if !strings.Contains(res.Failure, want) {
			t.Errorf("Failure = %q, want it to contain %q", res.Failure, want)
		}
	}
}

// Off by default means exactly that: no policy attached, nothing changes.
func TestRunWithoutASandboxPolicyIsUnaffected(t *testing.T) {
	restore := overrideMechanism(func() (string, error) { return "", ErrSandboxUnsupported })
	defer restore()

	res, err := New().Run(context.Background(), Cmd{Command: "echo ran"})
	if err != nil || !res.OK() {
		t.Fatalf("unsandboxed Run should succeed: err=%v res=%+v", err, res)
	}
	if !strings.Contains(res.Output, "ran") {
		t.Fatalf("Output = %q", res.Output)
	}
}

func TestRefusalNamesTheReasonAndTheSwitch(t *testing.T) {
	got := Refusal(errors.New("/usr/bin/sandbox-exec is not present"))
	want := "the sandbox could not be established: /usr/bin/sandbox-exec is not present.\n" +
		"Commands will not run unconfined while the sandbox is on. To run them anyway: /sandbox off"
	if got != want {
		t.Fatalf("Refusal =\n%q\nwant\n%q", got, want)
	}
}

// The denylist is plan 13 §3's hardline paths, so the kernel refuses what the
// blocklist used to string-match.
func TestCredentialDenylistCoversTheHardlinePaths(t *testing.T) {
	got := CredentialDenylist("/home/ada", "/home/ada/.local/share/kolk/credentials.json")
	for _, want := range []string{
		"/home/ada/.ssh", "/home/ada/.aws", "/home/ada/.gnupg",
		"/home/ada/.local/share/kolk/credentials.json",
	} {
		found := false
		for _, p := range got {
			if p == want {
				found = true
			}
		}
		if !found {
			t.Errorf("denylist %v is missing %q", got, want)
		}
	}
}
