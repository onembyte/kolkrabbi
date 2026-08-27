package cli

import (
	"context"
	"strings"
	"testing"
)

func TestResumeWorksTheChaptersRatherThanDescribingThem(t *testing.T) {
	a, out := sagaFixture(t, "### Chapter 1: done already\n- **Status**: completed\n\n")

	if err := a.runSaga(context.Background(), []string{"resume"}); err != nil {
		t.Fatalf("resume: %v", err)
	}

	got := out.String()
	// The message this replaced said the loop was "not wired to this command
	// yet". S10.6 wired it, and nothing walked back to the sentence claiming
	// otherwise — which is the exact failure gate 8 was written for.
	if strings.Contains(got, "not wired") {
		t.Fatalf("resume still says the loop is unwired: %q", got)
	}
	if !strings.Contains(got, "nothing left") {
		t.Fatalf("resume did not work the chapters: %q", got)
	}
}

func TestResumeWithNoSagaSaysSo(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	a, out := &app{stdout: &strings.Builder{}}, &strings.Builder{}
	a.stdout, a.stderr = out, out

	if err := a.runSaga(context.Background(), []string{"resume"}); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if !strings.Contains(out.String(), "no saga") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestRewindStillSaysWhatItCannotDo(t *testing.T) {
	a, out := sagaFixture(t, "### Chapter 1: done\n- **Status**: completed\n\n")

	if err := a.runSaga(context.Background(), []string{"rewind"}); err != nil {
		t.Fatalf("rewind: %v", err)
	}
	// Rewind genuinely is not built. An honest refusal is right, and this test
	// exists so that whoever builds it has to delete a failing assertion
	// rather than leave the sentence behind.
	if !strings.Contains(out.String(), "not wired") {
		t.Fatalf("rewind no longer states its limit: %q", out.String())
	}
}
