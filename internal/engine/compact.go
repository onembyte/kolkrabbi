package engine

import (
	"context"
	"fmt"
	"reflect"
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
// System instructions and the original goal survive. Recent turns are preferred
// verbatim; when they alone are too large, completed tool traffic may be shortened.
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
	for i, message := range messages {
		if len(message.ToolCalls) > 0 && completeToolRound(messages, i) == i {
			return result, nil
		}
	}

	head, tail := splitAtRecentTurns(messages, keepTurns)
	if len(head) == 0 {
		return compactToolRounds(messages, targetTokens), nil
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
	collapsed, collapsedCount := collapseToolTraffic(head)
	if collapsedCount > 0 {
		replaced += collapsedCount
		working = collapsed
	}
	if fits(working, tail, targetTokens) {
		return finish(working, tail, StageToolCalls, replaced, original), nil
	}

	stage := StageToolCalls
	if collapsedCount == 0 {
		stage = StageToolResults
	}
	local := compactRecent(finish(working, tail, stage, replaced, original), targetTokens)
	if estimateTokens(local.Messages) <= targetTokens || len(head) == 1 && head[0].Role == "system" {
		return local, nil
	}
	// 3. Everything older becomes one summary. Try local shrinking first so a
	// summary provider is never required merely to shorten recent tool output.
	if summarize == nil {
		return local, nil
	}
	// The summary must see the facts that cheaper stages proposed removing.
	summary, err := summarize(head)
	if err != nil {
		return local, fmt.Errorf("summarising the older conversation: %w", err)
	}
	if strings.TrimSpace(summary) == "" {
		return local, nil
	}
	kept := []provider.Message{}
	goalKept := false
	for _, message := range head {
		if message.Role == "system" || message.Role == "user" && !goalKept {
			kept = append(kept, message)
			goalKept = goalKept || message.Role == "user"
		}
	}
	kept = append(kept, provider.Message{
		Role:    "assistant",
		Content: "[earlier conversation, summarised]\n" + summary,
	})
	replaced = len(head)
	return compactRecent(finish(kept, tail, StageSummary, replaced, original), targetTokens), nil
}

func compactRecent(result Compaction, target int) Compaction {
	if estimateTokens(result.Messages) <= target {
		return result
	}
	recent := compactToolRounds(result.Messages, target)
	if recent.FreedTokens > 0 {
		if result.Stage != StageNone {
			recent.Stage = result.Stage
		}
		recent.Replaced += result.Replaced
		recent.FreedTokens += result.FreedTokens
		return recent
	}
	return result
}

func finish(head, tail []provider.Message, stage string, replaced, original int) Compaction {
	messages := append(append([]provider.Message(nil), head...), tail...)
	if estimateTokens(messages) >= original {
		stage, replaced = StageNone, 0
	}
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
		end := completeToolRound(messages, i)
		if end == i {
			out = append(out, message)
			continue
		}
		out = append(out, toolRoundDigest(messages, i, end))
		collapsed += end - i
		i = end - 1
	}
	return out, collapsed
}

func estimateTokens(messages []provider.Message) int {
	characters := 0
	for _, message := range messages {
		characters += len(message.Content) + len(message.Reasoning)
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

// compactIfNeeded runs before a turn or after a complete tool round. The
// transform refuses partially answered transactions.
//
// Failure here is never fatal. A session that cannot be compacted should still
// try its turn and let the provider answer or refuse.
func (a *Agent) compactIfNeeded(ctx context.Context) {
	if a.Sess == nil {
		return
	}
	before := a.Sess.GetMessages()
	usage := MeasureContext(a.window(), 0, before)
	usage.Used = max(usage.Used, int(a.lastPromptTokens.Load()))
	if !usage.ShouldCompact() {
		return
	}
	target := int(float64(usage.Window) * compactToFraction)
	result, changed := a.CompactNow(ctx, target)
	if !changed {
		return
	}
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
// Archival must succeed before the complete working transcript is replaced.
func (a *Agent) applyCompaction(before []provider.Message, result Compaction) bool {
	path, err := a.archiveMessages(before)
	if err != nil {
		fmt.Fprintf(a.Out, "could not compact: full history could not be archived: %v\n", err)
		return false
	}
	a.Sess.SetMessages(result.Messages)
	if err := a.Sess.Save(); err != nil {
		a.Sess.SetMessages(before)
		fmt.Fprintf(a.Out, "could not compact: session could not be saved: %v\n", err)
		return false
	}
	a.preCompact, a.lastArchive = before, path
	a.postCompact = result.Messages
	a.lastPromptTokens.Store(0) // The last request measured a different transcript.
	return true
}

func (a *Agent) archiveMessages(messages []provider.Message) (string, error) {
	a.archiveMu.Lock()
	defer a.archiveMu.Unlock()
	if a.ArchiveCompaction != nil {
		return a.ArchiveCompaction(messages)
	}
	if a.Sess == nil {
		return "", fmt.Errorf("no session to retain the conversation")
	}
	return a.Sess.ArchiveMessages(messages)
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
	if a.Sess == nil || a.sessionBackend() == nil {
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
		a.saveFor(saveAutoTitle)
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
		fmt.Fprintf(a.Out, "could not summarize older history: %v\n", err)
	}
	if result.Stage == StageNone || result.Replaced == 0 || result.FreedTokens <= 0 {
		return result, false
	}
	return result, a.applyCompaction(before, result)
}

// recoverFromOverflow compacts after a provider has refused an over-long
// request, so the turn can be retried instead of simply lost. It is allowed
// once per refused request: a second refusal without progress means it cannot be
// made to fit, and retrying again would only spend money to fail again.
func (a *Agent) recoverFromOverflow(ctx context.Context) bool {
	target := overflowTarget(a.window(), a.Sess.GetMessages())
	result, changed := a.CompactNow(ctx, target)
	if !changed {
		return false
	}
	fmt.Fprintf(a.Out, "the request was too long for %s; compacted %d messages (%s) and retrying once\n",
		a.SessionModel(), result.Replaced, result.Stage)
	return true
}

// RestoreCompaction puts back the messages the last compaction replaced.
func (a *Agent) RestoreCompaction() bool {
	if a.Sess == nil || a.preCompact == nil {
		return false
	}
	current := a.Sess.GetMessages()
	if len(current) < len(a.postCompact) || !reflect.DeepEqual(current[:len(a.postCompact)], a.postCompact) {
		fmt.Fprintln(a.Out, "cannot undo compaction because earlier messages changed; the full history remains archived")
		return false
	}
	// Later turns belong to the user too. Restore the replaced prefix and retain
	// everything appended since, instead of rolling the whole session backward.
	restored := append(append([]provider.Message(nil), a.preCompact...), current[len(a.postCompact):]...)
	a.Sess.SetMessages(restored)
	if err := a.Sess.Save(); err != nil {
		a.Sess.SetMessages(current)
		fmt.Fprintf(a.Out, "warning: could not save the restored conversation: %v\n", err)
		return false
	}
	a.preCompact, a.postCompact = nil, nil
	a.lastPromptTokens.Store(0)
	return true
}

// summarizeSpan summarises older history through the fast lane, which exists
// for exactly this and is zero-cost whenever the session model is free.
func (a *Agent) summarizeSpan(ctx context.Context) Summarizer {
	if a.sessionBackend() == nil {
		return nil
	}
	return func(span []provider.Message) (string, error) {
		var transcript strings.Builder
		for _, message := range span {
			transcript.WriteString(message.Role)
			transcript.WriteString(": ")
			transcript.WriteString(message.Content)
			transcript.WriteString("\n")
			for _, call := range message.ToolCalls {
				fmt.Fprintf(&transcript, "tool call %s: %s\n", call.Function.Name, call.Function.Arguments)
			}
		}
		return a.FastLaneChat(ctx, summarySystemPrompt, transcript.String())
	}
}
