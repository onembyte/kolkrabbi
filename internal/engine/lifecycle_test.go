package engine

import (
	"context"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

type closableBackend struct {
	closed bool
}

func (b *closableBackend) StreamChat(context.Context, string, []provider.Message, []provider.Tool, func(string)) (provider.Message, provider.Meta, error) {
	return provider.Message{}, provider.Meta{}, nil
}

func (b *closableBackend) Close() error {
	b.closed = true
	return nil
}

func TestAgentCloseClosesBackend(t *testing.T) {
	backend := &closableBackend{}
	agent := &Agent{Options: Options{Backend: backend}}
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	if !backend.closed {
		t.Fatal("backend was not closed")
	}
}
