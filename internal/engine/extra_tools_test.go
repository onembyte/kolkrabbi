package engine

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/enginetest"
	"github.com/onembyte/kolkrabbi/internal/provider"
)

type fakeExtra struct {
	calls []string
}

func (f *fakeExtra) Definitions() []provider.Tool {
	return []provider.Tool{{Type: "function", Function: provider.FunctionDef{Name: "github__create_issue", Description: "x", Parameters: []byte(`{"type":"object"}`)}}}
}

func (f *fakeExtra) Execute(_ context.Context, name, args string) (string, bool, error) {
	if !strings.HasPrefix(name, "github__") {
		return "", false, nil
	}
	f.calls = append(f.calls, name+" "+args)
	return "created #7", true, nil
}

// Plan 16 §3: an extra tool set joins the built-ins outside chat, never in
// it, and a namespaced call is routed to it through the same permission
// rules as every other tool — `deny mcp(github__*)` stops it before it runs.
func TestExtraToolsJoinOutsideChatAndAnswerToRules(t *testing.T) {
	extra := &fakeExtra{}
	a := New(Options{Mode: ModeCode, Model: "m", ExtraTools: extra, Permission: PermissionFullAuto, Out: io.Discard, Sess: enginetest.NewFakeSession("s", "m")})
	names := func(defs []provider.Tool) string {
		var out []string
		for _, d := range defs {
			out = append(out, d.Function.Name)
		}
		return strings.Join(out, ",")
	}
	if got := names(a.toolsFor(context.Background(), ModeCode)); !strings.Contains(got, "github__create_issue") || !strings.Contains(got, "bash") {
		t.Fatalf("code tools = %s", got)
	}
	if got := a.toolsFor(context.Background(), ModeChat); got != nil {
		t.Fatalf("chat got tools: %v", got)
	}

	srv := enginetest.New(
		enginetest.Step{ToolCalls: []provider.ToolCall{{ID: "c1", Type: "function", Function: provider.FunctionCall{Name: "github__create_issue", Arguments: `{"title":"bug"}`}}}},
		enginetest.Step{Text: "done"},
	)
	defer srv.Close()
	var out bytes.Buffer
	a = New(Options{Client: provider.NewCompatibleClient(srv.URL), Mode: ModeCode, Model: "m", ExtraTools: extra, Permission: PermissionFullAuto, Out: &out, Sess: enginetest.NewFakeSession("s", "m")})
	if err := a.RunTurn(context.Background(), "file a bug"); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if len(extra.calls) != 1 || !strings.Contains(extra.calls[0], `"title":"bug"`) {
		t.Fatalf("extra tool calls = %v\n%s", extra.calls, out.String())
	}

	denied := &fakeExtra{}
	rules, err := ParseRules([]string{"deny mcp(github__*)"})
	if err != nil {
		t.Fatal(err)
	}
	srv2 := enginetest.New(
		enginetest.Step{ToolCalls: []provider.ToolCall{{ID: "c1", Type: "function", Function: provider.FunctionCall{Name: "github__create_issue", Arguments: `{}`}}}},
		enginetest.Step{Text: "ok"},
	)
	defer srv2.Close()
	b := New(Options{Client: provider.NewCompatibleClient(srv2.URL), Mode: ModeCode, Model: "m", ExtraTools: denied, Permission: PermissionFullAuto, Rules: rules, Out: io.Discard, Sess: enginetest.NewFakeSession("s", "m")})
	_ = b.RunTurn(context.Background(), "file a bug")
	if len(denied.calls) != 0 {
		t.Fatalf("a denied mcp tool ran: %v", denied.calls)
	}
	_ = http.StatusOK
}
