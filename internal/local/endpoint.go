package local

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// A local endpoint is a name, an address, and whatever answers there (plan
// 37). This file is the "whatever answers there" half: one probe that tells
// the runtimes apart instead of assuming the one kolk grew up with.

// The two runtimes a local endpoint can be.
const (
	// KindOllama speaks Ollama's own API and an OpenAI-compatible one at /v1.
	KindOllama = "ollama"
	// KindOpenAI is everything else worth having: Docker Model Runner,
	// llama.cpp's server, LM Studio, vLLM — anything that serves an
	// OpenAI-compatible model list.
	KindOpenAI = "openai"
)

// identifyBudget bounds the whole probe. An endpoint is asked about while a
// person waits, and a machine that is asleep must not hold the session.
const identifyBudget = 4 * time.Second

// perRequestBudget bounds one question inside that.
const perRequestBudget = 2 * time.Second

// openAIBases are where an OpenAI-compatible runtime serves, in the order
// worth trying: exactly where the user pointed, then the two conventions.
var openAIBases = []string{"", "/v1", "/engines/v1"}

// Runtime is what the probe found: which API to speak, the URL to speak it
// to, the version where one is offered, and the models it listed.
type Runtime struct {
	Kind    string
	Base    string
	Version string
	Models  []string
}

// endpointName is one plain word, because the name is the model-id prefix.
var endpointName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,23}$`)

// reservedEndpointNames cannot be taken: "ollama" is this machine's own
// server, which needs no record, and "here" is what `use` calls it.
var reservedEndpointNames = map[string]bool{"ollama": true, "here": true}

// ValidEndpointName reports whether a name may be given to an endpoint.
func ValidEndpointName(name string) error {
	switch {
	case !endpointName.MatchString(name):
		return fmt.Errorf("an endpoint name is one word of lowercase letters, digits and dashes, up to 24 characters; %q is not", name)
	case reservedEndpointNames[name]:
		return fmt.Errorf("%q is kolk's own name for this machine's server; pick another", name)
	}
	return nil
}

// Identify says what runs at an address. The address may be written as a
// person writes one: bare host:port, with a scheme, and with the base path
// the runtime serves at. Nothing is sent but two questions any model server
// answers for anyone — no key, no prompt.
func Identify(ctx context.Context, addr string) (Runtime, error) {
	root, base, err := splitAddr(addr)
	if err != nil {
		return Runtime{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, identifyBudget)
	defer cancel()
	client := &http.Client{Timeout: perRequestBudget}

	if version, models, ok := identifyOllama(ctx, client, root); ok {
		return Runtime{Kind: KindOllama, Base: root + "/v1", Version: version, Models: models}, nil
	}

	// The base the user pointed at is tried first, then the conventions.
	bases := openAIBases
	if base != "" {
		bases = append([]string{base}, openAIBases...)
	}
	tried := make([]string, 0, len(bases))
	guarded := false
	for _, candidate := range bases {
		endpoint := root + candidate + "/models"
		if contains(tried, endpoint) {
			continue
		}
		tried = append(tried, endpoint)
		models, status, ok := identifyOpenAI(ctx, client, endpoint)
		switch {
		case ok:
			return Runtime{Kind: KindOpenAI, Base: root + candidate, Models: models}, nil
		case status == http.StatusUnauthorized || status == http.StatusForbidden:
			guarded = true
		}
	}
	if guarded {
		return Runtime{}, fmt.Errorf("%s asked for a key, and kolk sends none to a local endpoint; point it at a runtime that serves without one", root)
	}
	return Runtime{}, fmt.Errorf("nothing that serves models answered at %s (tried %s)", root, strings.Join(tried, ", "))
}

// identifyOllama asks for the handshake an Ollama gives and nothing else.
func identifyOllama(ctx context.Context, client *http.Client, root string) (string, []string, bool) {
	body, ok := hostGet(ctx, client, root+"/")
	if !ok || strings.TrimSpace(string(body)) != "Ollama is running" {
		return "", nil, false
	}
	body, ok = hostGet(ctx, client, root+"/api/version")
	if !ok {
		return "", nil, false
	}
	var version struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(body, &version) != nil || version.Version == "" {
		return "", nil, false
	}
	var tags struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	names := []string{}
	if body, ok := hostGet(ctx, client, root+"/api/tags"); ok && json.Unmarshal(body, &tags) == nil {
		for _, model := range tags.Models {
			names = append(names, model.Name)
		}
	}
	return version.Version, names, true
}

// identifyOpenAI asks one base for its model list, and reports the status so
// a refusal can be told from a wrong path.
func identifyOpenAI(ctx context.Context, client *http.Client, endpoint string) ([]string, int, bool) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, 0, false
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, 0, false
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, response.StatusCode, false
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return nil, response.StatusCode, false
	}
	var list struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &list) != nil {
		return nil, response.StatusCode, false
	}
	models := make([]string, 0, len(list.Data))
	for _, model := range list.Data {
		if model.ID != "" {
			models = append(models, model.ID)
		}
	}
	// A list is what makes this an OpenAI-compatible endpoint; a page that
	// happens to be JSON is not.
	if len(models) == 0 {
		return nil, response.StatusCode, false
	}
	return models, response.StatusCode, true
}

// splitAddr turns what a person wrote into a scheme-and-host root and the
// base path they may have included.
func splitAddr(addr string) (root, base string, err error) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return "", "", errors.New("an endpoint needs an address, as host:port")
	}
	if !strings.Contains(addr, "://") {
		addr = "http://" + addr
	}
	parsed, err := url.Parse(addr)
	if err != nil {
		return "", "", fmt.Errorf("%q is not an address kolk can read: %w", addr, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", "", fmt.Errorf("a local endpoint is reached over http or https; %q is not", parsed.Scheme)
	}
	if parsed.Host == "" {
		return "", "", fmt.Errorf("%q names no host", addr)
	}
	base = strings.TrimSuffix(parsed.Path, "/")
	return parsed.Scheme + "://" + parsed.Host, base, nil
}

func contains(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}
