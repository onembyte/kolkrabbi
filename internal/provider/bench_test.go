package provider

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// benchFragments splits s into chunks of roughly n runes, always cutting on a
// rune boundary.
//
// This is internal/enginetest's fragmenter, copied rather than imported:
// enginetest imports this package, so a benchmark in package provider cannot
// import it back, and readStream is unexported so an external test package is
// not an option either. The two must stay the same shape -- content at 7,
// tool-call arguments at 9 -- or this stops measuring what a session pays.
func benchFragments(s string, n int) []string {
	if s == "" {
		return nil
	}
	var out []string
	start, count := 0, 0
	for i := range s { // i iterates rune start offsets
		if count == n {
			out = append(out, s[start:i])
			start, count = i, 0
		}
		count++
	}
	return append(out, s[start:])
}

// benchContentSSE is a fragmented content stream of size bytes.
func benchContentSSE(size int) string {
	var body strings.Builder
	body.WriteString("data: {\"choices\":[{\"delta\":{\"role\":\"assistant\"}}]}\n\n")
	for _, frag := range benchFragments(strings.Repeat("x", size), 7) {
		quoted, _ := json.Marshal(frag)
		fmt.Fprintf(&body, "data: {\"choices\":[{\"delta\":{\"content\":%s}}]}\n\n", quoted)
	}
	body.WriteString("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
	body.WriteString("data: {\"model\":\"bench/model\",\"usage\":{\"prompt_tokens\":100,\"completion_tokens\":100}}\n\n")
	body.WriteString("data: [DONE]\n\n")
	return body.String()
}

// benchToolArgsSSE is one write_file call whose arguments arrive in fragments,
// totalling about size bytes of argument text.
func benchToolArgsSSE(size int) string {
	args, err := json.Marshal(map[string]string{"path": "notes.md", "content": strings.Repeat("x", size)})
	if err != nil {
		panic(err)
	}
	var body strings.Builder
	body.WriteString("data: {\"choices\":[{\"delta\":{\"role\":\"assistant\"}}]}\n\n")
	body.WriteString("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_bench\",\"type\":\"function\",\"function\":{\"name\":\"write_file\"}}]}}]}\n\n")
	for _, frag := range benchFragments(string(args), 9) {
		quoted, _ := json.Marshal(frag)
		fmt.Fprintf(&body, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":%s}}]}}]}\n\n", quoted)
	}
	body.WriteString("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n")
	body.WriteString("data: [DONE]\n\n")
	return body.String()
}

// BenchmarkReadStream is the O0.2 baseline for OPTIMIZATION_PLAN.md O2.
//
// content/ is the path that already uses a strings.Builder; toolargs/ is the
// one that accumulates with string += per fragment. The pair is the point: at
// the same payload size the two should cost the same, and the gap between them
// is what O2 is asked to close.
func BenchmarkReadStream(b *testing.B) {
	sizes := []struct {
		name string
		size int
	}{
		{name: "50KB", size: 50 << 10},
		{name: "200KB", size: 200 << 10},
	}

	b.Run("content", func(b *testing.B) {
		for _, size := range sizes {
			body := benchContentSSE(size.size)
			b.Run(size.name, func(b *testing.B) {
				b.SetBytes(int64(len(body)))
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					var meta Meta
					msg, err := readStream(strings.NewReader(body), &meta, nil)
					if err != nil {
						b.Fatalf("readStream: %v", err)
					}
					if len(msg.Content) != size.size {
						b.Fatalf("content = %d bytes, want %d", len(msg.Content), size.size)
					}
				}
			})
		}
	})

	b.Run("toolargs", func(b *testing.B) {
		for _, size := range sizes {
			body := benchToolArgsSSE(size.size)
			b.Run(size.name, func(b *testing.B) {
				b.SetBytes(int64(len(body)))
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					var meta Meta
					msg, err := readStream(strings.NewReader(body), &meta, nil)
					if err != nil {
						b.Fatalf("readStream: %v", err)
					}
					if len(msg.ToolCalls) != 1 || len(msg.ToolCalls[0].Function.Arguments) < size.size {
						b.Fatalf("tool arguments did not reassemble: %d calls", len(msg.ToolCalls))
					}
				}
			})
		}
	})
}
