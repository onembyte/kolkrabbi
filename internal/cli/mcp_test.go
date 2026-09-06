package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/config"
	"github.com/onembyte/kolkrabbi/internal/mcp"
)

// pipeServer is an MCP server on in-memory channels, enough to answer the
// handshake and list one tool.
type pipeServer struct{ in, out chan []byte }

func (s *pipeServer) Send(line []byte) error { s.in <- append([]byte(nil), line...); return nil }
func (s *pipeServer) Next(ctx context.Context) ([]byte, error) {
	select {
	case l := <-s.out:
		return l, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func (s *pipeServer) Close() error { close(s.in); return nil }
func (s *pipeServer) serve() {
	for line := range s.in {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		_ = json.Unmarshal(line, &req)
		if req.ID == nil {
			continue
		}
		var result any
		switch req.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": mcp.ProtocolVersion}
		case "tools/list":
			result = map[string]any{"tools": []map[string]any{{"name": "issue", "description": "make an issue", "inputSchema": map[string]any{"type": "object"}}}}
		}
		out, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
		s.out <- out
	}
}

// /mcp add names a server that the next session starts; list and rm read and
// write the same block; tools starts what is configured through the seam and
// reports what loaded under the budget. Names carry no double underscore,
// since that is the namespace separator.
func TestSlashMCPManagesServersAndReportsTools(t *testing.T) {
	d := isolateHome(t)
	isolateConnectorState(t)
	a, ag, out := replFixture(t, "")
	a.dirs = d
	if a.slash(context.Background(), ag, "/mcp add github npx -y @modelcontextprotocol/server-github") {
		t.Fatal("/mcp must not exit")
	}
	if !strings.Contains(out.String(), "github__<tool>") || !strings.Contains(out.String(), "allow mcp(github__*)") {
		t.Fatalf("add: %q", out.String())
	}
	cfg, err := config.Load(d.ConfigFile())
	if err != nil || cfg.MCP["github"].Command != "npx" || len(cfg.MCP["github"].Args) != 2 {
		t.Fatalf("saved = %+v, %v", cfg.MCP, err)
	}
	out.Reset()
	a.slash(context.Background(), ag, "/mcp add bad__name x")
	if !strings.Contains(out.String(), "lowercase letters") {
		t.Fatalf("bad name accepted: %q", out.String())
	}
	out.Reset()
	a.slash(context.Background(), ag, "/mcp list")
	if !strings.Contains(out.String(), "github") || !strings.Contains(out.String(), "npx") {
		t.Fatalf("list: %q", out.String())
	}

	a.mcpStarter = func(context.Context, mcp.Server) (mcp.Transport, error) {
		s := &pipeServer{in: make(chan []byte, 8), out: make(chan []byte, 8)}
		go s.serve()
		return s, nil
	}
	ag.ExtraTools = a.newMCPPool(cfg)
	out.Reset()
	a.slash(context.Background(), ag, "/mcp tools")
	if !strings.Contains(out.String(), "github__issue") || !strings.Contains(out.String(), "1 tools loaded") || !strings.Contains(out.String(), "schema budget 4096") {
		t.Fatalf("tools: %q", out.String())
	}

	out.Reset()
	a.slash(context.Background(), ag, "/mcp rm github")
	cfg, _ = config.Load(d.ConfigFile())
	if _, ok := cfg.MCP["github"]; ok || !strings.Contains(out.String(), "removed") {
		t.Fatalf("rm: %+v %q", cfg.MCP, out.String())
	}
}
