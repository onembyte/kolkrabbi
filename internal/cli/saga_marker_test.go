package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/engine"
)

func TestInlineSagaPromptRemovesOnlyStandaloneMarkers(t *testing.T) {
	tests := []struct {
		name   string
		prompt string
		want   string
		marked bool
	}{
		{name: "end", prompt: "build an ecommerce web app /saga", want: "build an ecommerce web app", marked: true},
		{name: "beginning", prompt: "/saga build an ecommerce web app", want: "build an ecommerce web app", marked: true},
		{name: "middle", prompt: "build an ecommerce /saga web app", want: "build an ecommerce  web app", marked: true},
		{name: "repeated", prompt: "/saga build it /saga", want: "build it", marked: true},
		{name: "empty", prompt: " /saga ", want: "", marked: true},
		{name: "url-like", prompt: "open https://example.test/saga/page", want: "open https://example.test/saga/page", marked: false},
		{name: "word-like", prompt: "repair /saga-mode", want: "repair /saga-mode", marked: false},
		{name: "absent", prompt: "build an ecommerce web app", want: "build an ecommerce web app", marked: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, marked := inlineSagaPrompt(test.prompt)
			if got != test.want || marked != test.marked {
				t.Fatalf("inlineSagaPrompt(%q) = %q, %v; want %q, %v", test.prompt, got, marked, test.want, test.marked)
			}
		})
	}
}

func TestSagaIsAnInlineOnlySurface(t *testing.T) {
	if lookupCommand("saga") != nil {
		t.Fatal("saga is still exposed as a standalone CLI command")
	}
	var markerFound bool
	for _, command := range slashCommandTable {
		if command.name == "saga" {
			markerFound = command.args == "inline marker"
		}
	}
	if !markerFound {
		t.Fatal("/saga is not documented as an inline marker")
	}
}

func TestBareSagaExplainsTheInlineForm(t *testing.T) {
	var out bytes.Buffer
	a := &app{stdout: &out, stderr: &out}
	if a.slash(context.Background(), nil, "/saga") {
		t.Fatal("bare /saga unexpectedly exited the session")
	}
	got := out.String()
	if !strings.Contains(got, "inside your request") || !strings.Contains(got, "/saga") {
		t.Fatalf("bare /saga output = %q, want inline usage", got)
	}
}

func TestSagaWakeMessagesPointToTheInlineMarker(t *testing.T) {
	for _, message := range []string{
		sagaStopMessage(engine.StopWake, &engine.SagaState{Goal: "build it", ActiveChapter: 4}),
		sagaWakeRetryMessage("build it"),
	} {
		if !strings.Contains(message, "/saga") || strings.Contains(message, "resume") || strings.Contains(message, "kolk saga") {
			t.Fatalf("SAGA message = %q, want inline continuation without old commands", message)
		}
	}
}
