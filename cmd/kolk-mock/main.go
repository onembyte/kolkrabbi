// mockserver is a standalone scripted fake of the OpenRouter API for
// sandboxed manual testing of kolk — no network, no API key, no cost.
//
//	go run ./cmd/mockserver      # prints its URL
//	kolk --base-url <url> --permission full-auto "create the hello file"
//
// The script below covers a full demo session: one code-mode turn, then one
// orchestrated agent-mode turn (plan → two subagents → synthesis). Edit the
// steps to rehearse other flows; the same mock powers the automated e2e
// tests (internal/mockrouter).
package main

import (
	"fmt"

	"github.com/onembyte/kolkrabbi/internal/enginetest"
	"github.com/onembyte/kolkrabbi/internal/provider"
)

func main() {
	srv := enginetest.New(
		// --- turn 1 (code mode): tool call + answer ---
		enginetest.Step{
			Text: "Writing the file.",
			ToolCalls: []provider.ToolCall{{
				ID: "call_1",
				Function: provider.FunctionCall{
					Name:      "write_file",
					Arguments: `{"path":"hello-from-mock.txt","content":"sandboxed smoke test — ok\n"}`,
				},
			}},
			Cost: 0.0011,
		},
		enginetest.Step{Text: "All done — hello-from-mock.txt created.", Cost: 0.0008},

		// --- turn 2 (agent mode): plan → subagents → synthesis ---
		enginetest.Step{Text: `["append a second line to hello-from-mock.txt", "count the lines in the file"]`, Cost: 0.0004},
		enginetest.Step{ToolCalls: []provider.ToolCall{{
			ID: "call_2",
			Function: provider.FunctionCall{
				Name:      "bash",
				Arguments: `{"command":"echo second line >> hello-from-mock.txt","description":"append a line"}`,
			},
		}}, Cost: 0.0009},
		enginetest.Step{Text: "Appended the second line.", Cost: 0.0005},
		enginetest.Step{ToolCalls: []provider.ToolCall{{
			ID: "call_3",
			Function: provider.FunctionCall{
				Name:      "bash",
				Arguments: `{"command":"wc -l hello-from-mock.txt","description":"count lines"}`,
			},
		}}, Cost: 0.0009},
		enginetest.Step{Text: "The file has 2 lines.", Cost: 0.0005},
		enginetest.Step{Text: "Done: appended a line and verified the file now has 2 lines.", Cost: 0.0012},
	)
	fmt.Println(srv.URL)
	fmt.Println("try: kolk --base-url", srv.URL, `--permission full-auto "create the hello file"`)
	select {}
}
