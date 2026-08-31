package local

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	// cloudCatalogURL is deliberately fixed. A catalog URL supplied by an
	// environment variable would make a model-listing command an SSRF client,
	// and following redirects could move a future credential-bearing request to
	// an unexpected host.
	cloudCatalogURL = "https://ollama.com/api/tags"

	// cloudCatalogBudget bounds the optional public lookup. The caller can
	// still cancel sooner; this ceiling prevents a dead network from holding a
	// picker or command forever.
	cloudCatalogBudget = 3 * time.Second

	// These limits are intentionally independent: a valid public response may
	// contain many small rows, while one hostile row must not consume the whole
	// response budget or leave an unbounded string for a terminal surface.
	cloudCatalogMaxBodyBytes  = 1 << 20
	cloudCatalogMaxRows       = 256
	cloudCatalogMaxNameBytes  = 512
	cloudCatalogMaxFieldBytes = 256
)

// CloudCatalogModel is the public Ollama Cloud catalogue's metadata. It is
// not a claim that the local server can run the model; E11.2 proves that
// separately through the local /api/show proxy.
type CloudCatalogModel struct {
	Name         string
	Digest       string
	Size         uint64
	Family       string
	Parameters   string
	Quantization string
}

// ListCloudCatalog reads the public Cloud catalogue without sending a user
// credential. The public list is optional discovery, so callers can preserve
// already-known host rows when this returns an error.
func ListCloudCatalog(ctx context.Context) ([]CloudCatalogModel, error) {
	ctx, cancel := context.WithTimeout(ctx, cloudCatalogBudget)
	defer cancel()
	return fetchCloudCatalog(ctx, &http.Client{Timeout: cloudCatalogBudget}, cloudCatalogURL)
}

// fetchCloudCatalog is separated from ListCloudCatalog so tests can use a
// loopback server without changing the production endpoint or global state.
func fetchCloudCatalog(ctx context.Context, client *http.Client, endpoint string) ([]CloudCatalogModel, error) {
	if client == nil {
		client = &http.Client{}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("ollama cloud catalogue request: %w", err)
	}

	// Copy the caller's client so test transports and production timeout
	// settings survive, while this boundary always refuses redirects. The
	// request has no Authorization header and no cookie jar is installed.
	safeClient := *client
	safeClient.Jar = nil
	safeClient.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	response, err := safeClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("ollama cloud catalogue request: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama cloud catalogue returned HTTP %d", response.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, cloudCatalogMaxBodyBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read ollama cloud catalogue: %w", err)
	}
	if len(body) > cloudCatalogMaxBodyBytes {
		return nil, fmt.Errorf("ollama cloud catalogue exceeds %d bytes", cloudCatalogMaxBodyBytes)
	}

	var envelope *struct {
		Models []struct {
			Name    string `json:"name"`
			Digest  string `json:"digest"`
			Size    uint64 `json:"size"`
			Details struct {
				Family       string `json:"family"`
				Parameters   string `json:"parameter_size"`
				Quantization string `json:"quantization_level"`
			} `json:"details"`
		} `json:"models"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("decode ollama cloud catalogue: %w", err)
	}
	if envelope == nil {
		return nil, fmt.Errorf("decode ollama cloud catalogue: document is null")
	}
	if len(envelope.Models) > cloudCatalogMaxRows {
		return nil, fmt.Errorf("ollama cloud catalogue has %d rows; limit is %d", len(envelope.Models), cloudCatalogMaxRows)
	}
	models := make([]CloudCatalogModel, 0, len(envelope.Models))
	for index, entry := range envelope.Models {
		name, err := boundedCloudCatalogName(entry.Name)
		if err != nil {
			return nil, fmt.Errorf("ollama cloud catalogue row %d: %w", index+1, err)
		}
		for field, value := range map[string]string{
			"digest": entry.Digest, "family": entry.Details.Family,
			"parameter size": entry.Details.Parameters, "quantization": entry.Details.Quantization,
		} {
			if err := boundedCloudCatalogField(field, value); err != nil {
				return nil, fmt.Errorf("ollama cloud catalogue row %d: %w", index+1, err)
			}
		}
		models = append(models, CloudCatalogModel{
			Name: name, Digest: entry.Digest, Size: entry.Size,
			Family: entry.Details.Family, Parameters: entry.Details.Parameters,
			Quantization: entry.Details.Quantization,
		})
	}
	return models, nil
}

func boundedCloudCatalogName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", fmt.Errorf("model name is empty")
	}
	if !utf8.ValidString(name) {
		return "", fmt.Errorf("model name is not valid UTF-8")
	}
	if len(name) > cloudCatalogMaxNameBytes {
		return "", fmt.Errorf("model name exceeds %d bytes", cloudCatalogMaxNameBytes)
	}
	if strings.IndexFunc(name, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) }) >= 0 {
		return "", fmt.Errorf("model name contains whitespace or a control character")
	}
	return name, nil
}

func boundedCloudCatalogField(label, value string) error {
	if len(value) > cloudCatalogMaxFieldBytes {
		return fmt.Errorf("%s exceeds %d bytes", label, cloudCatalogMaxFieldBytes)
	}
	if !utf8.ValidString(value) {
		return fmt.Errorf("%s is not valid UTF-8", label)
	}
	return nil
}
