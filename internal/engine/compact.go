package engine

import (
	"context"
	"fmt"
	"strings"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

// Compaction stages, in the order they are spent. Each one gives up more
// meaning than the last, so compaction stops at the first that fits.
const (
	StageNone        = "none"
	StageToolResults = "tool results"
	StageToolCalls   = "tool calls"
	StageSummary     = "summary"
)

// Compaction is what one compaction gave up, so the caller can say so out loud.
type Compaction struct {
	Messages    []provider.Message
	Stage       string
	Replaced    int
	FreedTokens int
}

// Summarizer turns a span of history into one paragraph. It is injected
// because the summary comes from a model call, and the transform itself must
// stay testable without one.
type Summarizer func(messages []provider.Message) (string, error)

// CompactMessages shrinks a conversation toward targetTokens, sacrificing the
// least meaningful content first and stopping as soon as it fits.
//
// The system prompt and the most recent keepTurns turns are never touched: the
// recent turns are what the model needs most, and re-deriving the system prompt
// from a summary would change the agent's own instructions.
//
// Every stage leaves a conversation a provider will accept. Tool results are
// emptied rather than removed, because a tool message carries the id that
// answers a call, and a call left unanswered fails validation before the model
// sees it.
func CompactMessages(messages []provider.Message, keepTurns, targetTokens int, summarize Summarizer) (Compaction, error) {
	original := estimateTokens(messages)
	result := Compaction{Messages: messages, Stage: StageNone}
	if original <= targetTokens {
		return result, nil
	}

	head, tail := splitAtRecentTurns(messages, keepTurns)
	if len(head) == 0 {
		// Everything is recent. There is nothing this transform may touch, and
		// pretending otherwise would break the one guarantee it makes.
		return result, nil
	}

	working := append([]provider.Message(nil), head...)
	replaced := 0

	// 1. Tool output: most of the bytes of a coding session, least of its
	// meaning, and already capped at the tool layer.
	for i := range working {
		if working[i].Role != "tool" || working[i].Content == "" {
			continue
		}
		if strings.HasPrefix(working[i].Content, "[tool output dropped") {
			continue
		}
		working[i].Content = fmt.Sprintf("[tool output dropped: %d chars]", len(working[i].Content))
		replaced++
	}
	if fits(working, tail, targetTokens) {
		return finish(working, tail, StageToolResults, replaced, original), nil
	}

	// 2. The calls themselves, collapsed with their results into one line that
	// still records what ran.
	collapsed, collapsedCount := collapseToolTraffic(working)
	if collapsedCount > 0 {
		replaced += collapsedCount
		working = collapsed
	}
	if fits(working, tail, targetTokens) {
		return finish(working, tail, StageToolCalls, replaced, original), nil
	}

	// 3. Everything older becomes one summary.
	if summarize == nil {
		// Without a summarizer this is as small as it gets. Report honestly
		// rather than claiming a stage that did not run.
		stage := StageToolCalls
		if collapsedCount == 0 {
			stage = StageToolResults
		}
		return finish(working, tail, stage, replaced, original), nil
	}
	summary, err := summarize(working)
	if err != nil {
		return Compaction{}, fmt.Errorf("summarising the older conversation: %w", err)
	}
	kept := []provider.Message{}
	if len(working) > 0 && working[0].Role == "system" {
		kept = append(kept, working[0])
	}
	kept = append(kept, provider.Message{
		Role:    "assistant",
		Content: "[earlier conversation, summarised]\n" + summary,
	})
	replaced = len(working) - len(kept)
	return finish(kept, tail, StageSummary, replaced, original), nil
}

func finish(head, tail []provider.Message, stage string, replaced, original int) Compaction {
	messages := append(append([]provider.Message(nil), head...), tail...)
	return Compaction{
		Messages:    messages,
		Stage:       stage,
		Replaced:    replaced,
		FreedTokens: original - estimateTokens(messages),
	}
}

func fits(head, tail []provider.Message, target int) bool {
	return estimateTokens(head)+estimateTokens(tail) <= target
}

// splitAtRecentTurns returns everything that may be compacted, and the recent
// turns that may not. A turn starts at a user message.
func splitAtRecentTurns(messages []provider.Message, keepTurns int) (head, tail []provider.Message) {
	if keepTurns <= 0 {
		return messages, nil
	}
	seen, boundary := 0, -1
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != "user" {
			continue
		}
		seen++
		if seen == keepTurns {
			boundary = i
			break
		}
	}
	if boundary <= 0 {
		return nil, messages
	}
	return messages[:boundary], messages[boundary:]
}

// collapseToolTraffic replaces each assistant tool call and its results with a
// single line naming what ran, which keeps the history valid while removing the
// arguments and the answers.
func collapseToolTraffic(messages []provider.Message) ([]provider.Message, int) {
	out := make([]provider.Message, 0, len(messages))
	collapsed := 0
	for i := 0; i < len(messages); i++ {
		message := messages[i]
		if len(message.ToolCalls) == 0 {
			out = append(out, message)
			continue
		}
		names := make([]string, 0, len(message.ToolCalls))
		for _, call := range message.ToolCalls {
			names = append(names, call.Function.Name)
		}
		answered := map[string]bool{}
		for _, call := range message.ToolCalls {
			answered[call.ID] = true
		}
		skipped := 0
		for j := i + 1; j < len(messages); j++ {
			if messages[j].Role != "tool" || !answered[messages[j].ToolCallID] {
				break
			}
			skipped++
		}
		out = append(out, provider.Message{
			Role:    "assistant",
			Content: fmt.Sprintf("[ran: %s]", strings.Join(names, ", ")),
		})
		collapsed += 1 + skipped
		i += skipped
	}
	return out, collapsed
}

func estimateTokens(messages []provider.Message) int {
	characters := 0
	for _, message := range messages {
		characters += len(message.Content)
		for _, call := range message.ToolCalls {
			characters += len(call.Function.Name) + len(call.Function.Arguments)
		}
	}
	return characters / charsPerToken
}

// keepRecentTurns is how much recent conversation compaction never touches.
// Two turns is enough for the model to see what it just did and what was just
// asked, which is the context a summary is worst at reproducing.
const keepRecentTurns = 2

// compactToFraction is how far below the window compaction aims. Shrinking to
// the threshold would compact again on the very next turn; halving the window
// buys back real room for the cost of one summary.
const compactToFraction = 0.5

const summarySystemPrompt = `You compress a coding session's older history so work can continue.
Preserve, in this order: the user's goal, decisions taken, files created or modified, commands whose
results still matter, and work left open. Drop conversational texture entirely. Be specific about
names and paths. Write at most 200 words of plain prose, no preamble.`

// compactIfNeeded shrinks the session at a turn boundary when the window is
// filling. It is deliberately called before a turn begins and never during one:
// compacting between a tool call and its result would orphan the call, which is
// the exact damage the session repair exists to undo.
//
// Failure here is never fatal. A session that cannot be compacted should still
// try its turn and let the provider answer or refuse.
func (a *Agent) compactIfNeeded(ctx context.Context) {
	if a.Sess == nil {
		return
	}
	usage := a.contextUsage(a.lastPromptTokens)
	if !usage.ShouldCompact() {
		return
	}
	before := a.Sess.GetMessages()
	target := int(float64(usage.Window) * compactToFraction)
	result, err := CompactMessages(before, keepRecentTurns, target, a.summarizeSpan(ctx))
	if err != nil {
		fmt.Fprintf(a.Out, "could not compact the session: %v\n", err)
		return
	}
	if result.Stage == StageNone || result.Replaced == 0 {
		return
	}
	// Kept so the step can be undone within this session. Compaction is the one
	// operation that makes the model forget, so it must be reversible.
	a.applyCompaction(before, result)
	// Said out loud, always. A user who cannot see this happen cannot explain
	// why the model suddenly forgot something.
	fmt.Fprintf(a.Out, "compacted %d messages (%s), freeing about %d tokens\n",
		result.Replaced, result.Stage, result.FreedTokens)
	if a.lastArchive != "" {
		fmt.Fprintf(a.Out, "the replaced conversation is in %s\n", a.lastArchive)
	}
}

// applyCompaction swaps in the smaller conversation, keeping what it replaced
// both in memory for this session and on disk for after it.
//
// Neither failure stops the compaction. The session had to fit, and losing the
// archive costs reversibility beyond this process rather than the ability to
// keep working.
func (a *Agent) applyCompaction(before []provider.Message, result Compaction) {
	a.preCompact = before
	a.lastArchive = ""
	if a.ArchiveCompaction != nil {
		path, err := a.ArchiveCompaction(before)
		if err != nil {
			fmt.Fprintf(a.Out, "warning: could not archive the replaced conversation: %v\n", err)
		} else {
			a.lastArchive = path
		}
	}
	a.Sess.SetMessages(result.Messages)
	if err := a.Sess.Save(); err != nil {
		fmt.Fprintf(a.Out, "warning: compacted session could not be saved: %v\n", err)
	}
}

const titleSystemPrompt = `You name a coding session in at most six words.
Answer with the name only: no quotes, no punctuation at the end, no preamble.
Name the work, not the conversation: "add the config parser", not "user asks for help".`

// titleSessionIfNeeded replaces Kolkrabbi's first guess at a session name with
// a better one, once, after enough has happened to name.
//
// The first title is the opening line the user typed, which is often the least
// descriptive sentence of the whole session. This runs at a turn boundary like
// compaction, costs one fast-lane call, and never touches a title the user
// chose: `kolk sessions rename` marks a title as theirs.
func (a *Agent) titleSessionIfNeeded(ctx context.Context) {
	if a.Sess == nil || a.Backend == nil {
		return
	}
	// Asked before the call, not after: generating a name Kolkrabbi is not
	// allowed to use spends a model call on nothing, every turn.
	if !a.Sess.TitleIsAuto() {
		return
	}
	messages := a.Sess.GetMessages()
	if countTurns(messages) < turnsBeforeTitling {
		return
	}
	var transcript strings.Builder
	for _, message := range messages {
		if message.Content == "" || message.Role == "tool" {
			continue
		}
		transcript.WriteString(message.Role)
		transcript.WriteString(": ")
		transcript.WriteString(message.Content)
		transcript.WriteString("\n")
	}
	title, err := a.FastLaneChat(ctx, titleSystemPrompt, transcript.String())
	if err != nil || strings.TrimSpace(title) == "" {
		// Naming is a nicety. A session that cannot be named still works, and
		// saying so would be noise about something the user never asked for.
		return
	}
	if a.Sess.SetAutoTitle(strings.TrimSpace(title)) {
		a.save()
	}
}

// turnsBeforeTitling is how much has to have happened before a name is worth
// more than the opening line.
const turnsBeforeTitling = 2

func countTurns(messages []provider.Message) int {
	turns := 0
	for _, message := range messages {
		if message.Role == "user" {
			turns++
		}
	}
	return turns
}

// CompactNow compacts regardless of how full the window is, for a user asking
// explicitly and for recovering from a provider that has already refused. It
// returns what it gave up and whether anything changed.
func (a *Agent) CompactNow(ctx context.Context, target int) (Compaction, bool) {
	if a.Sess == nil {
		return Compaction{}, false
	}
	before := a.Sess.GetMessages()
	if target <= 0 {
		// No window to aim at: halve what is there, which is the same promise
		// compaction makes anywhere else.
		target = estimateTokens(before) / 2
	}
	result, err := CompactMessages(before, keepRecentTurns, target, a.summarizeSpan(ctx))
	if err != nil {
		fmt.Fprintf(a.Out, "could not compact the session: %v\n", err)
		return Compaction{}, false
	}
	if result.Stage == StageNone || result.Replaced == 0 {
		return result, false
	}
	a.applyCompaction(before, result)
	return result, true
}

// recoverFromOverflow compacts after a provider has refused an over-long
// request, so the turn can be retried instead of simply lost. It is allowed
// once per turn: a second refusal after compacting means the request cannot be
// made to fit, and retrying again would only spend money to fail again.
func (a *Agent) recoverFromOverflow(ctx context.Context) bool {
	target := int(float64(a.ContextWindow) * compactToFraction)
	result, changed := a.CompactNow(ctx, target)
	if !changed {
		return false
	}
	fmt.Fprintf(a.Out, "the request was too long for %s; compacted %d messages (%s) and retrying once\n",
		a.Model, result.Replaced, result.Stage)
	return true
}

// RestoreCompaction puts back the messages the last compaction replaced.
func (a *Agent) RestoreCompaction() bool {
	if a.Sess == nil || a.preCompact == nil {
		return false
	}
	a.Sess.SetMessages(a.preCompact)
	a.preCompact = nil
	// Reporting the conversation as restored while it is not on disk is a quiet
	// half-success: the next session would silently be the compacted one.
	if err := a.Sess.Save(); err != nil {
		fmt.Fprintf(a.Out, "warning: could not save the restored conversation: %v\n", err)
	}
	return true
}

// summarizeSpan summarises older history through the fast lane, which exists
// for exactly this and is zero-cost whenever the session model is free.
func (a *Agent) summarizeSpan(ctx context.Context) Summarizer {
	if a.Backend == nil {
		return nil
	}
	return func(span []provider.Message) (string, error) {
		var transcript strings.Builder
		for _, message := range span {
			if message.Content == "" {
				continue
			}
			transcript.WriteString(message.Role)
			transcript.WriteString(": ")
			transcript.WriteString(message.Content)
			transcript.WriteString("\n")
		}
		return a.FastLaneChat(ctx, summarySystemPrompt, transcript.String())
	}
}
