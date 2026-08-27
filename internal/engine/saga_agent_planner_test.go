package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/enginetest"
)

func TestThePlannerAsksForOneChapterAndGetsATitle(t *testing.T) {
	srv := enginetest.New(enginetest.Step{Text: "  audit the database package  \n"})
	defer srv.Close()

	agent, _, _, _ := newTestAgentInternal(t, srv, ModeCode)
	title, err := (AgentPlanner{Agent: agent}).Next(context.Background(), "migrate the store", nil)
	if err != nil {
		t.Fatal(err)
	}

	if title != "audit the database package" {
		t.Fatalf("title = %q", title)
	}
	var sent strings.Builder
	for _, m := range srv.Requests[0] {
		sent.WriteString(m.Content)
	}
	if !strings.Contains(sent.String(), "migrate the store") {
		t.Fatalf("the planner did not state the goal: %q", sent.String())
	}
}

func TestThePlannerIsToldWhatIsAlreadyDone(t *testing.T) {
	srv := enginetest.New(enginetest.Step{Text: "next thing"})
	defer srv.Close()

	agent, _, _, _ := newTestAgentInternal(t, srv, ModeCode)
	done := []Chapter{
		{Number: 1, Title: "audit", Status: StatusDone, Commit: "abc1234"},
		{Number: 2, Title: "swap the driver", Status: StatusFailed, Verification: "tests failed"},
	}
	if _, err := (AgentPlanner{Agent: agent}).Next(context.Background(), "g", done); err != nil {
		t.Fatal(err)
	}

	var sent strings.Builder
	for _, m := range srv.Requests[0] {
		sent.WriteString(m.Content)
	}
	// A failed chapter matters more than a finished one: repeating it is how a
	// saga enters the loop the doom detector exists to stop.
	for _, want := range []string{"audit", "swap the driver", "tests failed"} {
		if !strings.Contains(sent.String(), want) {
			t.Fatalf("the planner was not told %q: %s", want, sent.String())
		}
	}
}

func TestAPlannerSayingDoneEndsTheSaga(t *testing.T) {
	for _, reply := range []string{"DONE", "done", "  Done  "} {
		srv := enginetest.New(enginetest.Step{Text: reply})
		agent, _, _, _ := newTestAgentInternal(t, srv, ModeCode)

		title, err := (AgentPlanner{Agent: agent}).Next(context.Background(), "g", nil)
		srv.Close()
		if err != nil {
			t.Fatal(err)
		}
		// The empty title is the loop's signal for "goal met", so the word has
		// to survive whatever casing and spacing a model gives it.
		if title != "" {
			t.Fatalf("%q was read as a chapter title: %q", reply, title)
		}
	}
}

func TestAChapterTitleIsCutDownToOneLine(t *testing.T) {
	srv := enginetest.New(enginetest.Step{Text: "swap the driver\nthen run the tests\nand tidy up"})
	defer srv.Close()

	agent, _, _, _ := newTestAgentInternal(t, srv, ModeCode)
	title, err := (AgentPlanner{Agent: agent}).Next(context.Background(), "g", nil)
	if err != nil {
		t.Fatal(err)
	}

	// "Exactly one discrete task" is the rule; a three-line answer is three
	// tasks wearing one chapter, and it would land in a commit message.
	if strings.Contains(title, "\n") {
		t.Fatalf("title = %q, want one line", title)
	}
	if title != "swap the driver" {
		t.Fatalf("title = %q", title)
	}
}

func TestAPlannerWithNoAgentFails(t *testing.T) {
	if _, err := (AgentPlanner{}).Next(context.Background(), "g", nil); err == nil {
		t.Fatal("a planner with no agent reported success")
	}
}

var _ ChapterPlanner = AgentPlanner{}
