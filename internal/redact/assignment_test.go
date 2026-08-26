package redact

import (
	"strings"
	"testing"
)

func mustScrub(t *testing.T, input string) string {
	t.Helper()
	out := Scrub(input)
	if out == input {
		t.Fatalf("not scrubbed: %q", input)
	}
	return out
}

func TestSecretAssignmentsAreScrubbedWhateverTheVendor(t *testing.T) {
	// The vendor prefixes cover the keys Kolkrabbi knows. Most secrets in a
	// real .env belong to nobody it has heard of.
	for _, line := range []string{
		"DATABASE_PASSWORD=hunter2correcthorsebattery",
		"MY_SERVICE_TOKEN=abcdef0123456789abcdef0123456789",
		// A test-key prefix, not a live one: see the note in
		// engine/scrub_tools_test.go. What is under test is the assignment
		// shape, not the vendor's exact live-key format.
		"export STRIPE_SECRET_KEY=rk_test_0123456789abcdefghijkl",
		"api_key: 9f8e7d6c5b4a39281706abcdef012345",
		`  "clientSecret": "s3cr3t-value-long-enough-here"`,
		"AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCY6ptR41Bx9q",
	} {
		out := mustScrub(t, line)
		if strings.Contains(out, "hunter2correcthorse") || strings.Contains(out, "abcdef0123456789abcdef") ||
			strings.Contains(out, "wJalrXUtnFEMI") {
			t.Fatalf("the value survived: %q", out)
		}
		// The name must stay, or a scrubbed config becomes unreadable.
		if !strings.ContainsAny(out, "=:") {
			t.Fatalf("the assignment shape was destroyed: %q", out)
		}
	}
}

func TestOrdinaryCodeIsNotRedacted(t *testing.T) {
	// Over-redaction is not a safe default: it corrupts the output the model
	// needs, and it teaches people to distrust the scrubber.
	for _, line := range []string{
		"the config parser drops trailing commas",
		"PRIMARY_KEY=id",
		"SORT_KEY=created_at",
		"API_KEY=${OPENROUTER_API_KEY}",
		"API_KEY=<your key here>",
		"password: changeme",
		"KEY=short",
		"cache_key := fmt.Sprintf(\"%s:%d\", name, id)",
		"foreign_key: user_id",
		"DEBUG=true",
		"PORT=8080",
	} {
		if got := Scrub(line); got != line {
			t.Fatalf("ordinary line was redacted:\n  in:  %q\n  out: %q", line, got)
		}
	}
}

func TestCredentialsInsideAURLAreScrubbed(t *testing.T) {
	out := mustScrub(t, "postgres://admin:s3cr3tpassw0rd@db.example.com:5432/app")
	if strings.Contains(out, "s3cr3tpassw0rd") {
		t.Fatalf("the password survived: %q", out)
	}
	// The rest of the URL is what makes the line useful.
	for _, want := range []string{"postgres://", "admin", "db.example.com", "5432"} {
		if !strings.Contains(out, want) {
			t.Fatalf("scrubbing destroyed the URL: %q", out)
		}
	}
}

func TestAURLWithoutCredentialsIsUntouched(t *testing.T) {
	for _, url := range []string{
		"https://api.openrouter.ai/v1/chat",
		"postgres://db.example.com:5432/app",
		"see http://localhost:8080/health for the check",
	} {
		if got := Scrub(url); got != url {
			t.Fatalf("%q became %q", url, got)
		}
	}
}

func TestAWSAccessKeyIDsAreScrubbed(t *testing.T) {
	out := mustScrub(t, "aws_access_key_id = AKIA4T7RQZB2NKLMPX9F")
	if strings.Contains(out, "AKIA4T7RQZB2NKLMPX9F") {
		t.Fatalf("the key survived: %q", out)
	}
}

// The project's false-positive corpus already settled this: a template full of
// EXAMPLE and YOUR_KEY_HERE must survive scrubbing, because redacting
// documentation is noise. AWS's own published example key is one of those.
func TestDocumentedExampleKeysAreLeftAlone(t *testing.T) {
	for _, line := range []string{
		"AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		"aws_access_key_id = AKIAIOSFODNN7EXAMPLE",
	} {
		if got := Scrub(line); got != line {
			t.Fatalf("a documented example was redacted:\n  in:  %q\n  out: %q", line, got)
		}
	}
}

func TestScrubbingIsStableForTheSameValue(t *testing.T) {
	// A sentinel that changes every call makes a transcript unreadable and
	// hides that two occurrences were the same secret.
	line := "TOKEN=abcdef0123456789abcdef0123456789"
	first := Scrub(line)
	second := Scrub(line)
	if first != second {
		t.Fatalf("the same secret produced two sentinels: %q then %q", first, second)
	}
	// Two occurrences in one text must also agree, or a reader cannot tell
	// that they were the same secret.
	both := Scrub(line + "\n" + line)
	if strings.Count(both, first[strings.Index(first, "["):]) != 2 {
		t.Fatalf("the same secret twice produced different sentinels: %q", both)
	}
}
