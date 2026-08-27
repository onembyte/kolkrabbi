package cli

import (
	"context"
	"regexp"
	"strings"
	"testing"
)

func TestDoctorReportsEverySection(t *testing.T) {
	a, stdout, _ := newTestApp(t, "")
	if err := a.runDoctor(context.Background(), nil); err != nil {
		t.Fatalf("runDoctor: %v", err)
	}
	out := stdout.String()
	for _, section := range []string{"keys", "directories", "terminal", "network"} {
		if !strings.Contains(strings.ToLower(out), section) {
			t.Errorf("doctor never mentions %s:\n%s", section, out)
		}
	}
}

// The whole point of a diagnostic is that a person can paste it into an issue.
// It prints what it found, never what it found with.
func TestDoctorNeverPrintsKeyMaterial(t *testing.T) {
	const key = "sk-or-v1-0123456789abcdef0123456789abcdef0123"
	a, stdout, _ := newTestApp(t, "")
	// After newTestApp, which clears it as part of isolating the environment.
	t.Setenv("OPENROUTER_API_KEY", key)
	if err := a.runDoctor(context.Background(), nil); err != nil {
		t.Fatalf("runDoctor: %v", err)
	}
	out := stdout.String()

	if strings.Contains(out, key) {
		t.Fatal("doctor printed the API key in full")
	}
	// Not even a long prefix: the last four are what `kolk key` shows, and more
	// than that is a substring somebody can search a leaked log for.
	if strings.Contains(out, key[:20]) {
		t.Fatal("doctor printed a searchable prefix of the API key")
	}
	if !strings.Contains(out, key[len(key)-4:]) {
		t.Errorf("doctor does not identify which key is configured:\n%s", out)
	}
	if !strings.Contains(out, "OPENROUTER_API_KEY") {
		t.Errorf("doctor does not say where the key came from:\n%s", out)
	}
}

func TestDoctorSaysWhenNoKeyIsConfigured(t *testing.T) {
	a, stdout, _ := newTestApp(t, "")
	if err := a.runDoctor(context.Background(), nil); err != nil {
		t.Fatalf("runDoctor: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "kolk key") {
		t.Errorf("a machine with no key is not told how to get one:\n%s", out)
	}
}

// Offline is the normal case for a diagnostic — someone runs it *because*
// something is not working. It must report the failure and finish, not hang and
// not error out of the command.
func TestDoctorFinishesWithNoNetwork(t *testing.T) {
	a, stdout, _ := newTestApp(t, "")
	t.Setenv("OPENROUTER_BASE_URL", unroutableBaseURL)

	if err := a.runDoctor(context.Background(), nil); err != nil {
		t.Fatalf("doctor failed the command because the network was down: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "✗") && !strings.Contains(strings.ToLower(out), "unreachable") {
		t.Errorf("doctor did not report the network as unreachable:\n%s", out)
	}
	if !strings.Contains(strings.ToLower(out), "terminal") {
		t.Errorf("doctor stopped at the failing check instead of finishing:\n%s", out)
	}
}

// Every line is going into a bug report, so none of them may contain an
// absolute home path that names the person who ran it.
func TestDoctorDoesNotLeakTheUsersName(t *testing.T) {
	a, stdout, _ := newTestApp(t, "")
	if err := a.runDoctor(context.Background(), nil); err != nil {
		t.Fatalf("runDoctor: %v", err)
	}
	if regexp.MustCompile(`/(home|Users)/[a-z]`).MatchString(stdout.String()) {
		t.Errorf("doctor printed an absolute home path:\n%s", stdout.String())
	}
}
