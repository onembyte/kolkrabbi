package engine

import (
	"context"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

type recordingBackend struct{}

func (recordingBackend) StreamChat(context.Context, string, []provider.Message, []provider.Tool, func(string)) (provider.Message, provider.Meta, error) {
	return provider.Message{}, provider.Meta{}, nil
}

func TestNewUsesConfiguredChatBackend(t *testing.T) {
	backend := recordingBackend{}
	agent := New(Options{Backend: backend})
	if agent.Backend != backend {
		t.Fatalf("Backend = %T, want configured backend", agent.Backend)
	}
}

func TestNewKeepsClientAsDefaultChatBackend(t *testing.T) {
	client := provider.NewClient("")
	agent := New(Options{Client: client})
	if agent.Backend != client {
		t.Fatalf("Backend = %T, want *provider.Client default", agent.Backend)
	}
}
