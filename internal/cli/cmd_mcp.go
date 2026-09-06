package cli

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/onembyte/kolkrabbi/internal/config"
	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/mcp"
	"github.com/onembyte/kolkrabbi/internal/shell"
	"github.com/onembyte/kolkrabbi/internal/tools"
)

// mcpServers is the configured servers as the pool takes them.
func mcpServers(cfg *config.Config) []mcp.Server {
	var out []mcp.Server
	for name, s := range cfg.MCP {
		out = append(out, mcp.Server{Name: name, Command: s.Command, Args: s.Args, Env: s.Env})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// mcpStarter opens a server as a line process the shell package owns: its
// stdout is the transport, its stderr kept for the report, the environment
// only what the server was given.
func mcpStarter(ctx context.Context, server mcp.Server) (mcp.Transport, error) {
	return shell.StartLinesProcessWithOptions(ctx, server.Command, server.Args, shell.ProcessOptions{})
}

// newMCPPool is the session's extra tool set, or nil with nothing configured.
func (a *app) newMCPPool(cfg *config.Config) *mcp.Pool {
	servers := mcpServers(cfg)
	if len(servers) == 0 {
		return nil
	}
	used, _ := tools.SchemaCost()
	starter := a.mcpStarter
	if starter == nil {
		starter = mcpStarter
	}
	return mcp.NewPool(servers, starter, used, tools.SchemaBudgetBytes)
}

// mcpNameOK is a server name a rule can carry: letters, digits, dash and
// underscore-free, since the double underscore is the namespace separator.
func mcpNameOK(name string) bool {
	if name == "" || strings.Contains(name, "__") {
		return false
	}
	for _, r := range name {
		ok := r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-'
		if !ok {
			return false
		}
	}
	return true
}

// runMCP is /mcp: add, rm, list, tools.
func (a *app) runMCP(ctx context.Context, ag *engine.Agent, arg string) error {
	fields := strings.Fields(arg)
	if len(fields) == 0 {
		return usagef("/mcp add <name> <command> [args…] | rm <name> | list | tools")
	}
	d, err := a.resolve()
	if err != nil {
		return err
	}
	cfg, err := config.Load(d.ConfigFile())
	if err != nil {
		return err
	}
	switch fields[0] {
	case "add":
		if len(fields) < 3 {
			return usagef("/mcp add <name> <command> [args…]")
		}
		name := strings.ToLower(fields[1])
		if !mcpNameOK(name) {
			return usagef("a server name is lowercase letters, digits and dashes; %q is not", fields[1])
		}
		if cfg.MCP == nil {
			cfg.MCP = map[string]config.MCPServer{}
		}
		cfg.MCP[name] = config.MCPServer{Command: fields[2], Args: fields[3:]}
		if err := config.Save(d.ConfigFile(), cfg); err != nil {
			return err
		}
		fmt.Fprintf(a.stdout, "mcp %s → %s; its tools appear as %s__<tool> in the next session, and `allow mcp(%s__*)` governs them\n", name, strings.Join(fields[2:], " "), name, name)
	case "rm":
		if len(fields) != 2 {
			return usagef("/mcp rm <name>")
		}
		if _, ok := cfg.MCP[fields[1]]; !ok {
			return fmt.Errorf("no mcp server %q", fields[1])
		}
		delete(cfg.MCP, fields[1])
		if err := config.Save(d.ConfigFile(), cfg); err != nil {
			return err
		}
		fmt.Fprintf(a.stdout, "removed mcp %s\n", fields[1])
	case "list":
		if len(cfg.MCP) == 0 {
			fmt.Fprintln(a.stdout, "no mcp servers; /mcp add <name> <command> [args…]")
			return nil
		}
		for _, s := range mcpServers(cfg) {
			fmt.Fprintf(a.stdout, "%-14s %s %s\n", s.Name, s.Command, strings.Join(s.Args, " "))
		}
	case "tools":
		pool, _ := ag.ExtraTools.(*mcp.Pool)
		if pool == nil {
			fmt.Fprintln(a.stdout, "no mcp servers in this session; /mcp add, then start a new session")
			return nil
		}
		used, _ := tools.SchemaCost()
		fmt.Fprintf(a.stdout, "schema budget %d bytes per request, %d used by the built-ins\n", tools.SchemaBudgetBytes, used)
		for _, report := range pool.Load(ctx) {
			switch {
			case report.Err != nil:
				fmt.Fprintf(a.stdout, "  ✗ %-12s %v\n", report.Name, report.Err)
			default:
				fmt.Fprintf(a.stdout, "  ✓ %-12s %d tools loaded\n", report.Name, len(report.Loaded))
				for _, name := range report.Loaded {
					fmt.Fprintf(a.stdout, "      %s\n", name)
				}
				for _, left := range report.LeftOut {
					fmt.Fprintf(a.stdout, "      · %s left out: %d bytes would pass the budget\n", left.Tool.Name, left.Cost)
				}
			}
		}
	default:
		return usagef("/mcp add <name> <command> [args…] | rm <name> | list | tools")
	}
	return nil
}
