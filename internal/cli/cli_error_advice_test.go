package cli

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/provider"
)

// A provider failure that reaches the top level must arrive with its next
// action attached. The advice exists in internal/provider; this is the test
// that it is actually printed, because an advisor nothing calls is worth
// nothing.
func TestProviderFailuresPrintTheirNextAction(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		wantLine string
	}{
		{"401", &provider.HTTPError{StatusCode: http.StatusUnauthorized}, "/key"},
		{"402", &provider.HTTPError{StatusCode: http.StatusPaymentRequired}, "/models"},
		{"429", &provider.HTTPError{StatusCode: http.StatusTooManyRequests}, "free model"},
		{"wrapped 404", fmt.Errorf("turn 1: %w", &provider.HTTPError{StatusCode: http.StatusNotFound}), "/models"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, _, stderr := newTestApp(t, "")
			a.printFailure(tc.err, ExitError)
			out := stderr.String()
			if !strings.Contains(out, tc.wantLine) {
				t.Errorf("printed failure does not tell the user what to do next:\n%s", out)
			}
			if !strings.Contains(out, "error:") {
				t.Errorf("printed failure hides the underlying error:\n%s", out)
			}
		})
	}
}

// Advice must never displace guidance a command wrote for itself: the command
// knows more than a status-code table does.
func TestGuidedErrorsKeepTheirOwnHints(t *testing.T) {
	a, _, stderr := newTestApp(t, "")
	a.printFailure(&GuidedError{Msg: "the key is unreadable", Hint: []string{"run `/key -`"}}, ExitError)
	out := stderr.String()
	if !strings.Contains(out, "run `/key -`") {
		t.Errorf("guided hint was lost:\n%s", out)
	}
	if strings.Contains(out, "/models") {
		t.Errorf("status-code advice displaced the command's own guidance:\n%s", out)
	}
}

func TestOrdinaryErrorsGainNoAdvice(t *testing.T) {
	a, _, stderr := newTestApp(t, "")
	a.printFailure(errors.New("something else entirely"), ExitError)
	out := strings.TrimSpace(stderr.String())
	if out != "error: something else entirely" {
		t.Errorf("an unrelated error grew commentary: %q", out)
	}
}

// The three places a turn can fail must all say the same thing. This is the
// test that catches a fourth print site being added without advice, or one of
// the three losing it in a refactor.
func TestEveryTurnFailureSiteWritesAdvice(t *testing.T) {
	sites := []struct {
		file string
		call string
	}{
		{"cli.go", "writeAdvice(a.stderr, err)"},
		{"repl.go", "writeAdvice(a.stderr, err)"},
		{"tui_repl.go", "writeAdvice(screen, err)"},
	}
	for _, site := range sites {
		source, err := os.ReadFile(site.file)
		if err != nil {
			t.Fatalf("reading %s: %v", site.file, err)
		}
		if !strings.Contains(string(source), site.call) {
			t.Errorf("%s prints a failed turn without its next action", site.file)
		}
	}
}

// A turn that stops because it was going nowhere, and a saga that stops for the
// same reason, must say the same thing. Two vocabularies for one failure is how
// a user learns that the words do not mean anything in particular.
func TestADoomLoopStopReadsLikeTheSagasOwn(t *testing.T) {
	a, _, stderr := newTestApp(t, "")
	a.printFailure(&engine.DoomLoopError{Tool: "read_file", Repeats: 3}, ExitError)
	out := stderr.String()

	if !strings.Contains(out, doomLoopPhrase) {
		t.Errorf("a turn-level stop does not use the shared phrase %q:\n%s", doomLoopPhrase, out)
	}
	if !strings.Contains(out, "read_file") {
		t.Errorf("the stop does not name the tool that repeated:\n%s", out)
	}
	if !strings.Contains(out, "/undo") {
		t.Errorf("the stop gives the user no next action:\n%s", out)
	}
}

// The saga's chapter-level stop is where the phrase came from. If someone
// rewrites either line, this fails and they have to change both on purpose.
func TestBothDoomLoopStopsShareOnePhrase(t *testing.T) {
	saga := sagaStopMessage(engine.StopDoomLoop, &engine.SagaState{Goal: "g"})
	if !strings.Contains(saga, doomLoopPhrase) {
		t.Errorf("the saga's stop no longer uses the shared phrase %q: %q", doomLoopPhrase, saga)
	}
}

// Advice for an ordinary provider failure must still work: the doom-loop case
// is an addition, not a replacement.
func TestProviderAdviceSurvivesTheDoomLoopCase(t *testing.T) {
	a, _, stderr := newTestApp(t, "")
	a.printFailure(&provider.HTTPError{StatusCode: 401}, ExitError)
	if !strings.Contains(stderr.String(), "/key") {
		t.Errorf("provider advice stopped printing:\n%s", stderr.String())
	}
}
