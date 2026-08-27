package cli

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"

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
		{"401", &provider.HTTPError{StatusCode: http.StatusUnauthorized}, "kolk key"},
		{"402", &provider.HTTPError{StatusCode: http.StatusPaymentRequired}, "kolk models"},
		{"429", &provider.HTTPError{StatusCode: http.StatusTooManyRequests}, "free model"},
		{"wrapped 404", fmt.Errorf("turn 1: %w", &provider.HTTPError{StatusCode: http.StatusNotFound}), "kolk models"},
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
	a.printFailure(&GuidedError{Msg: "the key is unreadable", Hint: []string{"run `kolk key -`"}}, ExitError)
	out := stderr.String()
	if !strings.Contains(out, "run `kolk key -`") {
		t.Errorf("guided hint was lost:\n%s", out)
	}
	if strings.Contains(out, "kolk models") {
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
