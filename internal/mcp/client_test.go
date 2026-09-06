package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// fakeServer speaks MCP's JSON-RPC over an in-memory line transport: it
// answers initialize, tools/list and tools/call, ignores the initialized
// notification, and can be told to answer a call with an error result.
type fakeServer struct {
	in, out  chan []byte
	seen     []string
	failCall bool
}

func newFakeServer() *fakeServer {
	s := &fakeServer{in: make(chan []byte, 16), out: make(chan []byte, 16)}
	go s.serve()
	return s
}

func (s *fakeServer) Send(line []byte) error { s.in <- append([]byte(nil), line...); return nil }
func (s *fakeServer) Next(ctx context.Context) ([]byte, error) {
	select {
	case line := <-s.out:
		return line, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func (s *fakeServer) Close() error { close(s.in); return nil }

func (s *fakeServer) serve() {
	for line := range s.in {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		_ = json.Unmarshal(line, &req)
		s.seen = append(s.seen, req.Method)
		if req.ID == nil {
			continue // a notification
		}
		var result any
		switch req.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]any{"name": "fake", "version": "1"}}
		case "tools/list":
			result = map[string]any{"tools": []map[string]any{
				{"name": "create_issue", "description": "Create an issue", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"title": map[string]any{"type": "string"}}, "required": []string{"title"}}},
				{"name": "search", "description": "Search", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"q": map[string]any{"type": "string"}}}},
			}}
		case "tools/call":
			var p struct {
				Name string          `json:"name"`
				Args json.RawMessage `json:"arguments"`
			}
			_ = json.Unmarshal(req.Params, &p)
			if p.Name == "nothing" {
				out, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": req.ID, "error": map[string]any{"code": -32602, "message": "unknown tool"}})
				s.out <- out
				continue
			}
			if s.failCall {
				result = map[string]any{"content": []map[string]any{{"type": "text", "text": "no such issue"}}, "isError": true}
			} else {
				result = map[string]any{"content": []map[string]any{{"type": "text", "text": "created #7 from " + string(p.Args)}, {"type": "image", "data": "..."}}}
			}
		default:
			out, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": req.ID, "error": map[string]any{"code": -32601, "message": "no such method"}})
			s.out <- out
			continue
		}
		out, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
		s.out <- out
	}
}

// MCP over stdio, plan 16 §3: initialize, the initialized notification, the
// tool list, a call whose text content is the result and whose isError is
// the tool's own failure. Tools are namespaced <server>__<tool> so a rule can
// name one server and nothing else.
func TestClientSpeaksTheProtocolAndNamespacesTools(t *testing.T) {
	srv := newFakeServer()
	c := NewClient("github", srv)
	if err := c.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	tools, err := c.ListTools(context.Background())
	if err != nil || len(tools) != 2 || tools[0].Name != "create_issue" {
		t.Fatalf("tools = %+v, %v", tools, err)
	}
	defs := Definitions("github", tools)
	if len(defs) != 2 || defs[0].Function.Name != "github__create_issue" || !strings.Contains(string(defs[0].Function.Parameters), `"title"`) {
		t.Fatalf("definitions = %+v", defs)
	}
	text, isError, err := c.CallTool(context.Background(), "create_issue", json.RawMessage(`{"title":"bug"}`))
	if err != nil || isError || !strings.Contains(text, "created #7") || strings.Contains(text, "image") {
		t.Fatalf("call = %q %v %v; want the text content only", text, isError, err)
	}
	srv.failCall = true
	text, isError, err = c.CallTool(context.Background(), "create_issue", json.RawMessage(`{}`))
	if err != nil || !isError || !strings.Contains(text, "no such issue") {
		t.Fatalf("failed call = %q %v %v; want the tool's own error", text, isError, err)
	}
	if _, _, err := c.CallTool(context.Background(), "nothing", nil); err == nil {
		t.Fatal("a server error was not surfaced")
	}
	if strings.Join(srv.seen[:2], ",") != "initialize,notifications/initialized" {
		t.Fatalf("handshake = %v", srv.seen)
	}
}

// The schema budget (4096 bytes on every request, plan 16 §3): a server whose
// tools would push the total past it has only the tools that fit loaded, in
// the server's order, and says which were left out and what each costs.
func TestLoadRespectsTheSchemaBudget(t *testing.T) {
	tools := []Tool{
		{Name: "a", Description: "A", InputSchema: json.RawMessage(`{"type":"object"}`)},
		{Name: "b", Description: strings.Repeat("b", 300), InputSchema: json.RawMessage(`{"type":"object"}`)},
		{Name: "c", Description: "C", InputSchema: json.RawMessage(`{"type":"object"}`)},
	}
	loaded, left := Fit("srv", tools, 3800, 3800+250)
	if len(loaded) != 2 || loaded[0].Function.Name != "srv__a" || loaded[1].Function.Name != "srv__c" {
		t.Fatalf("loaded = %+v", loaded)
	}
	if len(left) != 1 || left[0].Tool.Name != "b" || left[0].Cost < 300 {
		t.Fatalf("left out = %+v", left)
	}
}
