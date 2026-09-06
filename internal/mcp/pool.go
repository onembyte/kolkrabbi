package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

// Server is one configured MCP server: a command kolk starts on demand, its
// arguments, and the environment it is given (nothing else crosses).
type Server struct {
	Name    string
	Command string
	Args    []string
	Env     []string
}

// Starter opens a server's transport; the shell package supplies it, since
// os/exec is the shell's.
type Starter func(ctx context.Context, server Server) (Transport, error)

// Report is what a surface shows about one server after loading.
type Report struct {
	Name    string
	Loaded  []string
	LeftOut []LeftOut
	Err     error
}

// Pool holds the configured servers, starts each once on first need, loads
// the tools that fit the schema budget, and routes a namespaced call to its
// server. It is the engine's extra tool set.
type Pool struct {
	servers []Server
	start   Starter
	used    int
	budget  int

	mu      sync.Mutex
	loaded  bool
	clients map[string]*Client
	defs    []provider.Tool
	reports []Report
}

// NewPool takes the servers, the starter, the bytes the built-in schemas
// already cost, and the budget every request must stay inside.
func NewPool(servers []Server, start Starter, used, budget int) *Pool {
	sorted := append([]Server(nil), servers...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	return &Pool{servers: sorted, start: start, used: used, budget: budget, clients: map[string]*Client{}}
}

// Load starts every server once, lists its tools and keeps what fits. A
// server that fails to start or answer is reported, not fatal: the rest of
// the session does not depend on it.
func (p *Pool) Load(ctx context.Context) []Report {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.loaded {
		return p.reports
	}
	p.loaded = true
	used := p.used
	for _, server := range p.servers {
		report := Report{Name: server.Name}
		transport, err := p.start(ctx, server)
		if err != nil {
			report.Err = fmt.Errorf("mcp %s: start: %w", server.Name, err)
			p.reports = append(p.reports, report)
			continue
		}
		client := NewClient(server.Name, transport)
		if err := client.Initialize(ctx); err != nil {
			_ = client.Close()
			report.Err = err
			p.reports = append(p.reports, report)
			continue
		}
		tools, err := client.ListTools(ctx)
		if err != nil {
			_ = client.Close()
			report.Err = err
			p.reports = append(p.reports, report)
			continue
		}
		fit, left := Fit(server.Name, tools, used, p.budget)
		for _, def := range fit {
			if encoded, err := json.Marshal(def); err == nil {
				used += len(encoded) + 1
			}
			report.Loaded = append(report.Loaded, def.Function.Name)
		}
		report.LeftOut = left
		p.clients[server.Name] = client
		p.defs = append(p.defs, fit...)
		p.reports = append(p.reports, report)
	}
	return p.reports
}

// Definitions is every loaded tool, namespaced; empty before Load.
func (p *Pool) Definitions() []provider.Tool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]provider.Tool(nil), p.defs...)
}

// Execute runs a namespaced tool. handled is false for a name that is not
// one of this pool's, so the caller can try the built-ins.
func (p *Pool) Execute(ctx context.Context, name, args string) (result string, handled bool, err error) {
	server, tool, ok := Split(name)
	if !ok {
		return "", false, nil
	}
	p.mu.Lock()
	client := p.clients[server]
	known := false
	for _, def := range p.defs {
		if def.Function.Name == name {
			known = true
		}
	}
	p.mu.Unlock()
	if client == nil {
		return "", true, fmt.Errorf("%w: %s", ErrNoSuchServer, server)
	}
	if !known {
		return "", true, fmt.Errorf("mcp %s: %s is not a loaded tool (over the schema budget, or not listed)", server, tool)
	}
	text, isError, err := client.CallTool(ctx, tool, json.RawMessage(args))
	if err != nil {
		return "", true, err
	}
	if isError {
		return "tool error: " + text, true, nil
	}
	return text, true, nil
}

// Close ends every server.
func (p *Pool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for name, client := range p.clients {
		_ = client.Close()
		delete(p.clients, name)
	}
}
