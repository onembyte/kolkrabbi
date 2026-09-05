package shell

import (
	"reflect"
	"testing"
)

// The policy crosses the process boundary as JSON in the environment. It holds
// paths and a network word -- nothing secret -- and it has to come back exactly.
func TestLandlockPolicyRoundTripsThroughTheEnvironment(t *testing.T) {
	in := Sandbox{Root: "/w/p", Temp: "/tmp/k", Writable: []string{"/home/a/.cache", "/home/a/go"},
		Deny: []string{"/home/a/.ssh", "/home/a/creds.json"}, Network: NetworkAllow}
	encoded, err := encodeLandlockPolicy(in)
	if err != nil {
		t.Fatal(err)
	}
	out, err := decodeLandlockPolicy(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("round trip changed the policy:\n in  %+v\n out %+v", in, out)
	}
}

func TestDecodeLandlockPolicyRefusesGarbage(t *testing.T) {
	if _, err := decodeLandlockPolicy("not json"); err == nil {
		t.Fatal("garbage decoded to a policy")
	}
	if _, err := decodeLandlockPolicy(""); err == nil {
		t.Fatal("an empty policy is not a policy")
	}
}

// The child must not hand its own trigger to the command it runs: a `kolk`
// started inside the sandbox would otherwise believe it is the child.
func TestStripLandlockEnvRemovesExactlyTheTwoNames(t *testing.T) {
	env := []string{"PATH=/bin", landlockChildEnv + "=1", "HOME=/h", landlockPolicyEnv + "={}", "KOLK_LANDLOCK_CHILDISH=keep"}
	got := stripLandlockEnv(env)
	want := []string{"PATH=/bin", "HOME=/h", "KOLK_LANDLOCK_CHILDISH=keep"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("stripLandlockEnv = %v, want %v", got, want)
	}
}

// The wrapper re-executes kolk itself in front of the command, unchanged.
func TestLandlockArgvPutsSelfFirstAndKeepsTheCommand(t *testing.T) {
	got := landlockArgv("/opt/kolk", []string{"bash", "-c", "echo hi"})
	want := []string{"/opt/kolk", "bash", "-c", "echo hi"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("landlockArgv = %v, want %v", got, want)
	}
}
