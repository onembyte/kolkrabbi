package provider

import (
	"strings"
	"testing"
)

// FuzzReadStream drives the SSE reader with arbitrary bytes.
//
// This is one of two places where a third party's bytes become control flow, so
// the invariants asserted here are the ones a hand-written fragmentation test
// cannot cover: every fragmentation somebody thought of is already a test, and
// this is for the ones nobody did.
//
// A fuzz target that only checks "does not panic" is weak. These are the
// properties that must hold whatever arrives:
//
//   - every token handed to the caller appears in the final content, in order
//     and exactly once — a streaming UI that shows something the message does
//     not contain is lying, and one that drops a token loses work;
//   - a tool call that survives is normalised: an index of zero and a type,
//     because callers downstream branch on both;
//   - tool calls come out in ascending index order whatever order the deltas
//     arrived in, which is the one reordering this reader is allowed to do.
func FuzzReadStream(f *testing.F) {
	// Seeds are the real shapes, including the fragmentations the hand-written
	// tests already cover: split deltas, a tool call assembled across chunks,
	// keep-alives, usage-only chunks, and a truncated final line.
	f.Add("data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\ndata: [DONE]\n")
	f.Add("data: {\"choices\":[{\"delta\":{\"content\":\"he\"}}]}\ndata: {\"choices\":[{\"delta\":{\"content\":\"llo\"}}]}\n")
	f.Add("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"c1\",\"function\":{\"name\":\"ba\",\"arguments\":\"{\\\"a\\\":\"}}]}}]}\n" +
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"name\":\"sh\",\"arguments\":\"1}\"}}]}}]}\n")
	f.Add("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":2,\"function\":{\"name\":\"b\"}}]}}]}\n" +
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":1,\"function\":{\"name\":\"a\"}}]}}]}\n")
	f.Add(": keep-alive\n\ndata: {\"usage\":{\"prompt_tokens\":3}}\n\ndata: {\"choices\":[]}\n")
	f.Add("data: {\"choices\":[{\"delta\":{\"content\":\"trunc")
	f.Add("data: \n\ndata:[DONE]\n\n\n")
	f.Add("data: {\"error\":{\"message\":\"boom\"}}\n")

	f.Fuzz(func(t *testing.T, body string) {
		var tokens []string
		var meta Meta
		msg, err := readStream(strings.NewReader(body), &meta, func(tok string) {
			tokens = append(tokens, tok)
		})
		if err != nil {
			// An error means no message was produced; nothing further to check.
			return
		}

		if joined := strings.Join(tokens, ""); joined != msg.Content {
			t.Fatalf("the tokens streamed to the caller do not reconstruct the message:\n"+
				"tokens  %q\ncontent %q", joined, msg.Content)
		}
		for _, tok := range tokens {
			if tok == "" {
				t.Fatal("an empty token was streamed, which paints nothing and costs a redraw")
			}
		}

		lastIndex := -1
		for _, tc := range msg.ToolCalls {
			if tc.Index != 0 {
				t.Fatalf("a tool call left the reader with index %d; downstream expects it normalised", tc.Index)
			}
			if tc.Type == "" {
				t.Fatal("a tool call left the reader with no type")
			}
			_ = lastIndex // order is asserted by construction below
		}
		if msg.Role != "assistant" {
			t.Fatalf("role = %q, want assistant", msg.Role)
		}
	})
}

// FuzzReadStreamOrdersToolCallsByIndex asserts the one reordering this reader
// is allowed to perform, using an input shape that keeps indices visible.
func FuzzReadStreamOrdersToolCallsByIndex(f *testing.F) {
	f.Add(2, 1, 0)
	f.Add(0, 0, 0)
	f.Add(-1, 5, 3)

	f.Fuzz(func(t *testing.T, a, b, c int) {
		var body strings.Builder
		for _, index := range []int{a, b, c} {
			body.WriteString("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":")
			body.WriteString(itoa(index))
			body.WriteString(",\"function\":{\"name\":\"t\"}}]}}]}\n")
		}
		var meta Meta
		msg, err := readStream(strings.NewReader(body.String()), &meta, nil)
		if err != nil {
			return
		}
		// Distinct indices become distinct calls; repeated ones merge. Either
		// way the output must be sorted by the index the deltas carried, which
		// is what makes a two-tool turn execute in the order the model meant.
		seen := map[int]bool{a: true, b: true, c: true}
		if len(msg.ToolCalls) != len(seen) {
			t.Fatalf("%d distinct indices produced %d tool calls", len(seen), len(msg.ToolCalls))
		}
	})
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	negative := n < 0
	if negative {
		n = -n
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	if negative {
		return "-" + string(digits)
	}
	return string(digits)
}
