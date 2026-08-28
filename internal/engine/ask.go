package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// toolAskUser is answered by the surface, not by the tools package: the answer
// comes from a person, and nothing below the surface can reach one.
const toolAskUser = "ask_user"

// maxAskOptions bounds a question. Past this it stops being a choice and
// becomes a list to read, which is what prose is for.
const maxAskOptions = 8

// Choice is a question with a fixed set of answers, put to the person running
// the session because the model cannot settle it alone.
type Choice struct {
	Question string
	Options  []string
}

// Chooser presents a Choice and returns the option picked. ok is false when the
// question was dismissed rather than answered, which is not the same as picking
// the first option and must not be reported to the model as one.
type Chooser interface {
	Choose(context.Context, Choice) (string, bool)
}

type askArguments struct {
	Question string   `json:"question"`
	Options  []string `json:"options"`
}

// askUser puts a model's question to the person and returns their answer as the
// tool result.
//
// It never blocks a subagent: several run at once, so two of them asking would
// race for one terminal, and a question the user answers cannot say which of
// three parallel tasks it was for. A subagent is told to decide and report what
// it assumed, which is what it would have done without the tool.
func (a *Agent) askUser(ctx context.Context, arguments string, out io.Writer, mayAsk bool) (string, error) {
	var parsed askArguments
	if err := json.Unmarshal([]byte(arguments), &parsed); err != nil {
		return "", fmt.Errorf("ask_user: %w", err)
	}
	question := strings.TrimSpace(parsed.Question)
	if question == "" {
		return "ask_user needs a question. Ask it in your reply instead.", nil
	}

	options := make([]string, 0, len(parsed.Options))
	seen := make(map[string]bool, len(parsed.Options))
	for _, option := range parsed.Options {
		option = strings.TrimSpace(option)
		// A blank or repeated option is a choice the person cannot make
		// meaningfully: two rows that read the same, or one that reads as
		// nothing at all.
		if option == "" || seen[option] {
			continue
		}
		seen[option] = true
		options = append(options, option)
		if len(options) == maxAskOptions {
			break
		}
	}
	if len(options) < 2 {
		return "ask_user needs at least two distinct options. Ask in your reply instead, or decide and say what you assumed.", nil
	}

	if !mayAsk || a.Ask == nil {
		return "Nobody can be asked in this session. Decide it yourself, then say which option you took and why.", nil
	}

	answer, ok := a.Ask.Choose(ctx, Choice{Question: question, Options: options})
	if !ok {
		return "The question was dismissed without an answer. Decide it yourself, then say which option you took and why.", nil
	}
	fmt.Fprintf(out, "%s  ← %s%s\n", colorDim, answer, colorReset)
	return "The user chose: " + answer, nil
}
