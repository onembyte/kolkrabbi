// mockserver is a standalone scripted fake of the OpenRouter API for
// sandboxed manual testing of kolk — no network, no API key, no cost.
//
//	go run ./cmd/mockserver      # prints its URL
//	kolk --base-url <url> -y "create the hello file"
//
// The script below covers a full demo session: one code-mode turn, then one
// orchestrated agent-mode turn (plan → two subagents → synthesis). Edit the
// steps to rehearse other flows; the same mock powers the automated e2e
// tests (internal/mockrouter).
package main

import (
	"fmt"

	"kolkrabbi/internal/api"
	"kolkrabbi/internal/mockrouter"
)

func main() {
	srv := mockrouter.New(
		// --- turn 1 (code mode): tool call + answer ---
		mockrouter.Step{
			Text: "Writing the file.",
			ToolCalls: []api.ToolCall{{
				ID: "call_1",
				Function: api.FunctionCall{
					Name:      "write_file",
					Arguments: `{"path":"hello-from-mock.txt","content":"sandboxed smoke test — ok\n"}`,
				},
			}},
			Cost: 0.0011,
		},
		mockrouter.Step{Text: "All done — hello-from-mock.txt created.", Cost: 0.0008},

		// --- turn 2 (agent mode): plan → subagents → synthesis ---
		mockrouter.Step{Text: `["append a second line to hello-from-mock.txt", "count the lines in the file"]`, Cost: 0.0004},
		mockrouter.Step{ToolCalls: []api.ToolCall{{
			ID: "call_2",
			Function: api.FunctionCall{
				Name:      "bash",
				Arguments: `{"command":"echo second line >> hello-from-mock.txt","description":"append a line"}`,
			},
		}}, Cost: 0.0009},
		mockrouter.Step{Text: "Appended the second line.", Cost: 0.0005},
		mockrouter.Step{ToolCalls: []api.ToolCall{{
			ID: "call_3",
			Function: api.FunctionCall{
				Name:      "bash",
				Arguments: `{"command":"wc -l hello-from-mock.txt","description":"count lines"}`,
			},
		}}, Cost: 0.0009},
		mockrouter.Step{Text: "The file has 2 lines.", Cost: 0.0005},
		mockrouter.Step{Text: "Done: appended a line and verified the file now has 2 lines.", Cost: 0.0012},
	)
	fmt.Println(srv.URL)
	fmt.Println("try: kolk --base-url", srv.URL, `-y "create the hello file"`)
	select {}
}
