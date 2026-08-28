package engine

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"
)

type recordingChooser struct {
	got    Choice
	answer string
	ok     bool
	calls  int
}

func (c *recordingChooser) Choose(_ context.Context, choice Choice) (string, bool) {
	c.calls++
	c.got = choice
	return c.answer, c.ok
}

func TestAskUserReturnsWhatThePersonChose(t *testing.T) {
	chooser := &recordingChooser{answer: "Postgres", ok: true}
	agent := &Agent{Options: Options{Ask: chooser}}
	result, err := agent.askUser(context.Background(),
		`{"question":"Which database?","options":["Postgres","SQLite"]}`, io.Discard, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "Postgres") {
		t.Errorf("result = %q, want the chosen option", result)
	}
	if chooser.got.Question != "Which database?" || len(chooser.got.Options) != 2 {
		t.Errorf("chooser got %#v", chooser.got)
	}
}

// Dismissing is not choosing the first option. Reporting it as one would put
// words in the user's mouth and let the model build on a decision nobody made.
func TestADismissedQuestionIsNotAnAnswer(t *testing.T) {
	agent := &Agent{Options: Options{Ask: &recordingChooser{ok: false}}}
	result, err := agent.askUser(context.Background(),
		`{"question":"Which database?","options":["Postgres","SQLite"]}`, io.Discard, true)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result, "Postgres") || strings.Contains(result, "chose") {
		t.Errorf("a dismissal was reported as a choice: %q", result)
	}
	if !strings.Contains(result, "Decide it yourself") {
		t.Errorf("result = %q, want the model told to proceed on its own", result)
	}
}

// Several subagents run at once, so two of them asking would race for one
// terminal and neither answer would say which task it belonged to.
func TestASubagentIsNeverPutToThePerson(t *testing.T) {
	chooser := &recordingChooser{answer: "Postgres", ok: true}
	agent := &Agent{Options: Options{Ask: chooser}}
	result, err := agent.askUser(context.Background(),
		`{"question":"Which database?","options":["Postgres","SQLite"]}`, io.Discard, false)
	if err != nil {
		t.Fatal(err)
	}
	if chooser.calls != 0 {
		t.Error("a subagent reached the person")
	}
	if !strings.Contains(result, "Decide it yourself") {
		t.Errorf("result = %q, want the subagent told to decide", result)
	}
}

// With no surface able to ask, waiting would hang the run for an answer that
// cannot arrive.
func TestWithNobodyToAskTheModelIsToldToDecide(t *testing.T) {
	agent := &Agent{}
	result, err := agent.askUser(context.Background(),
		`{"question":"Which database?","options":["Postgres","SQLite"]}`, io.Discard, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "Decide it yourself") {
		t.Errorf("result = %q", result)
	}
}

func TestAskUserRejectsQuestionsThatCannotBeAnswered(t *testing.T) {
	chooser := &recordingChooser{answer: "x", ok: true}
	agent := &Agent{Options: Options{Ask: chooser}}
	for name, arguments := range map[string]string{
		"no question":     `{"question":"  ","options":["a","b"]}`,
		"one option":      `{"question":"Which?","options":["only"]}`,
		"no options":      `{"question":"Which?","options":[]}`,
		"blank options":   `{"question":"Which?","options":["a","  ",""]}`,
		"same twice over": `{"question":"Which?","options":["a","a"]}`,
	} {
		result, err := agent.askUser(context.Background(), arguments, io.Discard, true)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !strings.Contains(result, "instead") {
			t.Errorf("%s: result = %q, want the model redirected to prose", name, result)
		}
	}
	if chooser.calls != 0 {
		t.Errorf("an unanswerable question still reached the person (%d times)", chooser.calls)
	}
}

// Past a handful it stops being a choice and becomes a list to read.
func TestAQuestionIsCappedAtEightOptions(t *testing.T) {
	chooser := &recordingChooser{answer: "1", ok: true}
	agent := &Agent{Options: Options{Ask: chooser}}
	if _, err := agent.askUser(context.Background(),
		`{"question":"Which?","options":["1","2","3","4","5","6","7","8","9","10"]}`,
		io.Discard, true); err != nil {
		t.Fatal(err)
	}
	if len(chooser.got.Options) != maxAskOptions {
		t.Errorf("presented %d options, want %d", len(chooser.got.Options), maxAskOptions)
	}
}

func TestAskUserReportsMalformedArguments(t *testing.T) {
	agent := &Agent{Options: Options{Ask: &recordingChooser{}}}
	if _, err := agent.askUser(context.Background(), `{not json`, io.Discard, true); err == nil {
		t.Error("malformed arguments were accepted")
	}
}

// The tool existed and the picker worked, but nothing in the system prompt
// invited the model to reach for either -- and the surrounding text pushed the
// other way ("keep using tools until that checkpoint is complete"). A
// capability the model is never told about is one it does not have.
func TestTheCodePromptSaysWhenToAsk(t *testing.T) {
	agent := &Agent{Options: Options{Mode: ModeCode}}
	prompt := agent.systemPrompt(ModeCode)
	if !strings.Contains(prompt, toolAskUser) {
		t.Fatalf("the prompt never mentions %s, so the model will not use it:\n%s", toolAskUser, prompt)
	}
	// It has to bound the invitation too, or the model asks permission to
	// continue and the session turns into a questionnaire.
	for _, bound := range []string{"decide yourself", "permission to continue"} {
		if !strings.Contains(prompt, bound) {
			t.Errorf("the prompt invites asking without bounding it: %q is missing", bound)
		}
	}
}

// Chat mode has no tools at all, so promising one would be a lie the model
// cannot act on.
func TestTheChatPromptDoesNotOfferATool(t *testing.T) {
	agent := &Agent{Options: Options{Mode: ModeChat}}
	if prompt := agent.systemPrompt(ModeChat); strings.Contains(prompt, toolAskUser) {
		t.Errorf("chat mode offers a tool it does not have:\n%s", prompt)
	}
}

// The lifecycle-event id memo is written from the per-task goroutines. Without
// a lock two subagents starting together race on the map, which the detector
// catches and which a real run can turn into a concurrent map write: a panic
// mid-turn, not a wrong number. Run under -race, this is the guard.
func TestSubagentIDsAreMintedSafelyInParallel(t *testing.T) {
	agent := &Agent{}
	agent.lastTurnID = "turn-1"

	const tasks = 16
	ids := make([]string, tasks)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for index := range tasks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start // all at once, which is when the race shows
			ids[index] = agent.subagentTaskID(index)
		}()
	}
	close(start)
	wg.Wait()

	seen := map[string]bool{}
	for index, id := range ids {
		if id == "" {
			t.Fatalf("task %d got no id", index)
		}
		if seen[id] {
			t.Errorf("task %d reused id %s: its start and finish would pair with another task", index, id)
		}
		seen[id] = true
	}
	// The same index must keep its id, or a finish can never be paired with the
	// start it belongs to and the count never comes back down.
	for index, id := range ids {
		if again := agent.subagentTaskID(index); again != id {
			t.Errorf("task %d minted %s then %s", index, id, again)
		}
	}
}
