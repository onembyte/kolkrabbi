package tui

import (
	"strconv"
	"strings"
)

// Question is a fixed-option decision put to the user by the model or host. It is a
// picker rather than a typed reply because the answers are known: typing one
// out invites a spelling the model then has to interpret, and re-reading the
// options to type one is work the arrow keys already do.
type Question struct {
	// Title names a host-owned picker. Empty retains the model-question heading.
	Title   string
	Prompt  string
	Options []string
	// InitialIndex selects the current setting without changing the default
	// first-option behavior of model questions. Invalid indexes select zero.
	InitialIndex int
}

// RequestQuestion opens the picker at InitialIndex. Model questions default to
// the first option, where their recommendation belongs; host pickers can start
// at the current setting without changing it.
func (c *Controller) RequestQuestion(question Question) {
	options := make([]string, len(question.Options))
	copy(options, question.Options)
	c.question = &Question{Title: question.Title, Prompt: question.Prompt, Options: options, InitialIndex: question.InitialIndex}
	c.lastOptions = options
	c.questionIndex = question.InitialIndex
	if c.questionIndex < 0 || c.questionIndex >= len(options) {
		c.questionIndex = 0
	}
	c.beforeQuestion = c.status.Lifecycle
	c.setLifecycle("question")
}

// Question returns a defensive copy of the open picker, or nil.
func (c *Controller) Question() *Question {
	if c.question == nil {
		return nil
	}
	options := make([]string, len(c.question.Options))
	copy(options, c.question.Options)
	return &Question{Title: c.question.Title, Prompt: c.question.Prompt, Options: options, InitialIndex: c.question.InitialIndex}
}

// QuestionIndex is the highlighted row, for tests and adapters.
func (c *Controller) QuestionIndex() int { return c.questionIndex }

func (c *Controller) handleQuestionKey(key Key) Effect {
	count := len(c.question.Options)
	switch key.Kind {
	case KeyInterrupt, KeyEscape:
		return c.resolveQuestion(0, true, false)
	case KeyEOF:
		return c.resolveQuestion(0, true, true)
	case KeyUp:
		// Wrapping, because the list is short enough to see whole: stopping at
		// the top of four rows is a dead key, not a guard rail.
		c.questionIndex = (c.questionIndex - 1 + count) % count
		return Effect{}
	case KeyDown:
		c.questionIndex = (c.questionIndex + 1) % count
		return Effect{}
	case KeyEnter:
		return c.resolveQuestion(c.questionIndex+1, false, false)
	case KeyText:
		// The number beside a row picks it outright. Someone who can already
		// see the answer should not have to walk to it.
		if index, err := strconv.Atoi(strings.TrimSpace(key.Text)); err == nil && index >= 1 && index <= count {
			return c.resolveQuestion(index, false, false)
		}
		return Effect{}
	}
	return Effect{}
}

func (c *Controller) resolveQuestion(choice int, dismissed, exit bool) Effect {
	c.question = nil
	c.questionIndex = 0
	c.setLifecycle(c.beforeQuestion)
	c.beforeQuestion = ""
	return Effect{Choice: choice, ChoiceDismissed: dismissed, Exit: exit}
}

// chosen resolves a picking Effect against the question it came from. The
// controller keeps the last options for exactly this: the Effect carries an
// index, and an index alone cannot be turned back into an answer.
func (c *Controller) chosen(effect Effect) questionReply {
	if effect.ChoiceDismissed || effect.Choice < 1 || effect.Choice > len(c.lastOptions) {
		return questionReply{dismissed: true}
	}
	return questionReply{option: c.lastOptions[effect.Choice-1]}
}

// questionLines draws the picker. It carries no key legend: the marker and the
// numbers say what to do, and the rest of the app teaches the same two keys.
func (c *Controller) questionLines(width int) []string {
	title := sanitizeTerminalLine(c.question.Title)
	if title == "" {
		title = "question"
	}
	lines := []string{
		horizontalRule(title, width),
		clipLine(sanitizeTerminalLine(c.question.Prompt), width),
	}
	for index, option := range c.question.Options {
		marker := "  "
		if index == c.questionIndex {
			marker = "> "
		}
		row := marker + strconv.Itoa(index+1) + "  " + sanitizeTerminalLine(option)
		lines = append(lines, clipLine(row, width))
	}
	return append(lines, strings.Repeat("─", max(0, width)))
}
