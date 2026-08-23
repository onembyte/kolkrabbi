package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/onembyte/kolkrabbi/internal/secret"
)

var (
	ErrKeyRejected     = errors.New("provider: API key was rejected")
	ErrKeyVerification = errors.New("provider: API key verification failed")
)

// KeyStatus is the safe account information returned by OpenRouter's current
// key endpoint. Nil limit fields mean the key has no configured spend cap.
type KeyStatus struct {
	LimitUSD     *float64
	RemainingUSD *float64
	UsageUSD     float64
	FreeTier     bool
}

// OpenRouterVerifier checks one already-classified OpenRouter credential. The
// BaseURL seam exists for offline tests; production leaves it empty.
type OpenRouterVerifier struct {
	BaseURL string
	Client  *http.Client
}

// Verify makes exactly one bounded request and refuses every redirect. A
// redirect cannot become a credential-discovery mechanism or move the
// Authorization header onto a host the user did not select.
func (v OpenRouterVerifier) Verify(ctx context.Context, key secret.Secret) (KeyStatus, error) {
	if err := ctx.Err(); err != nil {
		return KeyStatus{}, err
	}
	if key.IsZero() {
		return KeyStatus{}, ErrKeyRejected
	}

	baseURL := strings.TrimRight(v.BaseURL, "/")
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	requestCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, baseURL+"/key", nil)
	if err != nil {
		return KeyStatus{}, fmt.Errorf("%w: building request: %w", ErrKeyVerification, err)
	}
	req.Header.Set("Accept", "application/json")

	baseClient := v.Client
	if baseClient == nil {
		baseClient = http.DefaultClient
	}
	client := *baseClient
	baseTransport := baseClient.Transport
	if baseTransport == nil {
		baseTransport = http.DefaultTransport
	}
	client.Transport = &secret.AuthTransport{Token: key, Base: baseTransport}
	client.Jar = nil
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}

	resp, err := client.Do(req)
	if err != nil {
		return KeyStatus{}, fmt.Errorf("%w: %w", ErrKeyVerification, secret.ScrubError(err))
	}
	defer func() { _ = resp.Body.Close() }()

	const maxResponseBytes = 1 << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return KeyStatus{}, fmt.Errorf("%w: reading response: %w", ErrKeyVerification, secret.ScrubError(err))
	}
	if len(body) > maxResponseBytes {
		return KeyStatus{}, fmt.Errorf("%w: response exceeds 1 MiB", ErrKeyVerification)
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return KeyStatus{}, fmt.Errorf("openrouter returned HTTP %d: %w", resp.StatusCode, ErrKeyRejected)
	}
	if resp.StatusCode != http.StatusOK {
		return KeyStatus{}, fmt.Errorf("%w: openrouter returned HTTP %d", ErrKeyVerification, resp.StatusCode)
	}

	var wire struct {
		Data *struct {
			Limit          *float64 `json:"limit"`
			LimitRemaining *float64 `json:"limit_remaining"`
			Usage          float64  `json:"usage"`
			IsFreeTier     bool     `json:"is_free_tier"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return KeyStatus{}, fmt.Errorf("%w: invalid JSON response", ErrKeyVerification)
	}
	if wire.Data == nil {
		return KeyStatus{}, fmt.Errorf("%w: response has no data object", ErrKeyVerification)
	}
	return KeyStatus{
		LimitUSD:     wire.Data.Limit,
		RemainingUSD: wire.Data.LimitRemaining,
		UsageUSD:     wire.Data.Usage,
		FreeTier:     wire.Data.IsFreeTier,
	}, nil
}
