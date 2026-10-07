package shell

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
)

type loginEndpointKey struct{}

// WithOllamaLoginEndpoint binds the vendor login to the exact local server
// being verified. It never changes this process's environment or credentials.
func WithOllamaLoginEndpoint(ctx context.Context, addr string) (context.Context, error) {
	host, port, err := net.SplitHostPort(addr)
	number, portErr := strconv.Atoi(port)
	if err != nil || portErr != nil || host != "127.0.0.1" || number < 1 || number > 65535 {
		return nil, fmt.Errorf("ollama login requires a loopback host:port, got %q", addr)
	}
	return context.WithValue(ctx, loginEndpointKey{}, addr), nil
}

func loginEnvironment(ctx context.Context) []string {
	env := inheritedEnv(nil)
	if addr, ok := ctx.Value(loginEndpointKey{}).(string); ok {
		filtered := env[:0]
		for _, entry := range env {
			if !strings.HasPrefix(entry, "OLLAMA_HOST=") {
				filtered = append(filtered, entry)
			}
		}
		env = append(filtered, "OLLAMA_HOST="+addr)
	}
	return env
}
