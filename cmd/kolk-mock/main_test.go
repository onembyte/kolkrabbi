package main

import (
	"bufio"
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

func TestMockExecutableServesScriptedTurn(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	binary := filepath.Join(t.TempDir(), "kolk-mock")
	if out, err := exec.CommandContext(ctx, "go", "build", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	cmd := exec.CommandContext(ctx, binary)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cancel(); _ = cmd.Wait() }()
	lines := bufio.NewScanner(stdout)
	if !lines.Scan() {
		t.Fatalf("mock did not publish a URL: %v", lines.Err())
	}
	url := lines.Text()
	if !strings.HasPrefix(url, "http://127.0.0.1:") {
		t.Fatalf("non-loopback mock URL: %q", url)
	}
	client := provider.NewCompatibleClient(url)
	messages := []provider.Message{{Role: "user", Content: "create the hello file"}}
	first, _, err := client.StreamChat(ctx, "mock/model", messages, nil, nil)
	if err != nil || len(first.ToolCalls) != 1 || first.ToolCalls[0].Function.Name != "write_file" {
		t.Fatalf("scripted tool call: %+v, %v", first, err)
	}
	messages = append(messages, first, provider.Message{Role: "tool", ToolCallID: first.ToolCalls[0].ID, Content: "fixture: wrote file"})
	last, _, err := client.StreamChat(ctx, "mock/model", messages, nil, nil)
	if err != nil || !strings.Contains(last.Content, "All done") {
		t.Fatalf("scripted reply: %+v, %v", last, err)
	}
}
