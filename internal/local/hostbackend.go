package local

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

// hostFloorContext is the window assumed for a host model until the server
// says otherwise. Ollama truncates an over-long prompt from the front, which
// drops kolk's system prompt and tool schemas first and says nothing — so an
// unknown window is treated as small, and compaction errs on the side of a
// model that still knows what it was asked. 4096 is Ollama's historical
// default and the smallest window it picks by VRAM.
const hostFloorContext = 4096

// hostWindowBudget bounds one /api/ps or /api/show lookup. Measured at 0.3 ms
// on the owner's machine; a second is generous.
const hostWindowBudget = time.Second

// warmKeepAlive is how long a warmed model stays loaded with nobody using it.
// Long enough to cover the pause between choosing a model and typing.
const warmKeepAlive = "15m"

// hostBackend is the route for the user's own Ollama: a keyless client, plus
// what the server has said about each model's effective window. Shared by the
// adopted server (address known at startup) and the one kolk starts (address
// known after the first turn asks for it).
type hostBackend struct {
	addr func(context.Context) (string, error)

	mu      sync.Mutex
	client  *provider.Client
	windows map[string]int
}

func newHostBackend(addr func(context.Context) (string, error)) *hostBackend {
	return &hostBackend{addr: addr, windows: map[string]int{}}
}

// NewHostBackend is the route for a server that is already running.
func NewHostBackend(addr string) *hostBackend {
	return newHostBackend(func(context.Context) (string, error) { return addr, nil })
}

func (b *hostBackend) ensureClient(ctx context.Context) (*provider.Client, string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	addr, err := b.addr(ctx)
	if err != nil {
		return nil, "", err
	}
	if b.client == nil {
		b.client = provider.NewHostClient(addr)
	}
	return b.client, addr, nil
}

func (b *hostBackend) StreamChat(ctx context.Context, model string, messages []provider.Message, tools []provider.Tool, onToken func(string)) (provider.Message, provider.Meta, error) {
	client, addr, err := b.ensureClient(ctx)
	if err != nil {
		return provider.Message{}, provider.Meta{Model: model}, err
	}
	message, meta, err := client.StreamChat(ctx, model, messages, tools, onToken)
	if err == nil {
		// The model is loaded now, so the server can say what window it
		// actually runs it with. Once per model: the answer does not change
		// while the model stays loaded, and 0.3 ms per turn adds up.
		b.learnWindow(ctx, addr, model)
	}
	return message, meta, err
}

// ContextWindow is the window the server runs model with, or the floor when
// it has not said yet. The engine asks this when the session has no window of
// its own, which for a host model is always.
func (b *hostBackend) ContextWindow(model string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	if window, ok := b.windows[model]; ok && window > 0 {
		return window
	}
	return hostFloorContext
}

// Warm loads the model so the first turn is not a cold load, and records the
// window the load produced. Bounded, because a load can take a while and the
// caller runs this off the turn path.
func (b *hostBackend) Warm(ctx context.Context, model string) {
	_, addr, err := b.ensureClient(ctx)
	if err != nil {
		return
	}
	body, _ := json.Marshal(map[string]string{"model": model, "keep_alive": warmKeepAlive})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+addr+"/api/generate", bytes.NewReader(body))
	if err != nil {
		return
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return
	}
	_ = response.Body.Close()
	b.learnWindow(ctx, addr, model)
}

// learnWindow asks the server once what window model runs with: /api/ps for a
// model loaded here, else /api/show for a cloud model, whose window is the
// trained one because nothing local constrains it.
func (b *hostBackend) learnWindow(ctx context.Context, addr, model string) {
	b.mu.Lock()
	_, known := b.windows[model]
	b.mu.Unlock()
	if known {
		return
	}
	window := effectiveWindow(ctx, addr, model)
	if window <= 0 {
		return
	}
	b.mu.Lock()
	b.windows[model] = window
	b.mu.Unlock()
}

func effectiveWindow(ctx context.Context, addr, model string) int {
	ctx, cancel := context.WithTimeout(ctx, hostWindowBudget)
	defer cancel()
	client := &http.Client{Timeout: hostWindowBudget}
	base := "http://" + addr

	if body, ok := hostGet(ctx, client, base+"/api/ps"); ok {
		var loaded struct {
			Models []struct {
				Name          string `json:"name"`
				Model         string `json:"model"`
				ContextLength int    `json:"context_length"`
			} `json:"models"`
		}
		if json.Unmarshal(body, &loaded) == nil {
			for _, m := range loaded.Models {
				if (m.Name == model || m.Model == model) && m.ContextLength > 0 {
					return m.ContextLength
				}
			}
		}
	}
	// Not loaded here. A cloud model never is; its window is what the proxy
	// reports for it.
	if shown, ok := showHostModel(ctx, client, base, model); ok && shown.remote && shown.contextLength > 0 {
		return shown.contextLength
	}
	return 0
}
