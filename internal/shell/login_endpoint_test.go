package shell

import (
	"context"
	"testing"
)

func TestLoginEndpointRejectsRemoteOrMalformedAddresses(t *testing.T) {
	for _, addr := range []string{"10.0.0.1:11434", "ollama.com:443", "localhost:11434", "127.0.0.1:0", "127.0.0.1:65536", "127.0.0.1:11434;env", "http://127.0.0.1:11434"} {
		if _, err := WithOllamaLoginEndpoint(context.Background(), addr); err == nil {
			t.Errorf("accepted %q", addr)
		}
	}
}
