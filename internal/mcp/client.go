// Package mcp is kolk's Model Context Protocol client (plan 16 §3): JSON-RPC
// 2.0 over a newline-delimited stdio transport the shell package owns, the
// tools a server lists namespaced <server>__<tool> so a permission rule can
// name one server and nothing else, and the schema budget respected by
// loading only what fits.
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

// ProtocolVersion is the MCP revision kolk speaks.
const ProtocolVersion = "2025-06-18"

// Transport is one line in, one line out: what shell.LinesProcess is.
type Transport interface {
	Send(line []byte) error
	Next(ctx context.Context) ([]byte, error)
	Close() error
}

// Tool is one tool a server lists.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// Client is one server's connection. Calls are serialised: MCP servers may
// answer out of order, but kolk asks one thing at a time and matches by id.
type Client struct {
	Name string
	t    Transport
	mu   sync.Mutex
	next int
}

// NewClient wraps a transport for the named server.
func NewClient(name string, t Transport) *Client {
	return &Client{Name: name, t: t}
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type rpcResponse struct {
	ID     *int            `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	Method string `json:"method"`
}

// call sends one request and waits for its answer, skipping the server's
// notifications and any stray reply with another id.
func (c *Client) call(ctx context.Context, method string, params any, result any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.next++
	id := c.next
	body, err := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params})
	if err != nil {
		return err
	}
	if err := c.t.Send(body); err != nil {
		return fmt.Errorf("mcp %s: %w", c.Name, err)
	}
	for {
		line, err := c.t.Next(ctx)
		if err != nil {
			return fmt.Errorf("mcp %s: waiting for %s: %w", c.Name, method, err)
		}
		var resp rpcResponse
		if json.Unmarshal(line, &resp) != nil || resp.ID == nil || *resp.ID != id {
			continue
		}
		if resp.Error != nil {
			return fmt.Errorf("mcp %s: %s: %s (code %d)", c.Name, method, resp.Error.Message, resp.Error.Code)
		}
		if result == nil || len(resp.Result) == 0 {
			return nil
		}
		return json.Unmarshal(resp.Result, result)
	}
}

func (c *Client) notify(method string, params any) error {
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
	if err != nil {
		return err
	}
	return c.t.Send(body)
}

// Initialize is the handshake: initialize, then the initialized notification.
func (c *Client) Initialize(ctx context.Context) error {
	var result struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	err := c.call(ctx, "initialize", map[string]any{
		"protocolVersion": ProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "kolkrabbi", "version": "1"},
	}, &result)
	if err != nil {
		return err
	}
	return c.notify("notifications/initialized", map[string]any{})
}

// ListTools is the server's tool list.
func (c *Client) ListTools(ctx context.Context) ([]Tool, error) {
	var result struct {
		Tools []Tool `json:"tools"`
	}
	if err := c.call(ctx, "tools/list", map[string]any{}, &result); err != nil {
		return nil, err
	}
	return result.Tools, nil
}

// CallTool runs one tool. The text contents become the result; isError is
// the tool's own failure, which the model should read, not a transport
// error.
func (c *Client) CallTool(ctx context.Context, name string, args json.RawMessage) (string, bool, error) {
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}
	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := c.call(ctx, "tools/call", map[string]any{"name": name, "arguments": args}, &result); err != nil {
		return "", false, err
	}
	var parts []string
	for _, part := range result.Content {
		if part.Type == "text" && part.Text != "" {
			parts = append(parts, part.Text)
		}
	}
	return strings.Join(parts, "\n"), result.IsError, nil
}

// Close ends the transport.
func (c *Client) Close() error { return c.t.Close() }

// Separator joins a server's name and a tool's, so a rule can match a prefix.
const Separator = "__"

// Split is the inverse: server and tool from a namespaced name.
func Split(name string) (server, tool string, ok bool) {
	server, tool, ok = strings.Cut(name, Separator)
	return server, tool, ok && server != "" && tool != ""
}

// Definitions turns a server's tools into the model's tool definitions,
// namespaced.
func Definitions(server string, tools []Tool) []provider.Tool {
	out := make([]provider.Tool, 0, len(tools))
	for _, tool := range tools {
		params := tool.InputSchema
		if len(params) == 0 {
			params = json.RawMessage(`{"type":"object"}`)
		}
		out = append(out, provider.Tool{Type: "function", Function: provider.FunctionDef{
			Name: server + Separator + tool.Name, Description: tool.Description, Parameters: params,
		}})
	}
	return out
}

// LeftOut is a tool that did not fit the schema budget, with its cost.
type LeftOut struct {
	Tool Tool
	Cost int
}

// Fit loads a server's tools in its order until the budget is reached: the
// definitions that fit, and the ones left out with what each would cost.
// Every request pays for every schema, so a server does not get to spend the
// window before the work starts (plan 16 §3).
func Fit(server string, tools []Tool, used, budget int) ([]provider.Tool, []LeftOut) {
	var loaded []provider.Tool
	var left []LeftOut
	for _, tool := range tools {
		def := Definitions(server, []Tool{tool})[0]
		encoded, err := json.Marshal(def)
		cost := len(encoded) + 1
		if err != nil || used+cost > budget {
			left = append(left, LeftOut{Tool: tool, Cost: cost})
			continue
		}
		used += cost
		loaded = append(loaded, def)
	}
	return loaded, left
}

// ErrNoSuchServer is a namespaced name whose server is not configured.
var ErrNoSuchServer = errors.New("mcp: no such server")
