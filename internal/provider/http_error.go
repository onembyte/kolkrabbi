package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/onembyte/kolkrabbi/internal/secret"
)

// HTTPError is a non-success response received before response streaming
// begins. Its text fields are scrubbed when the value is built so callers can
// safely inspect classification metadata as well as print the error.
type HTTPError struct {
	StatusCode   int
	ResponseBody string
	Message      string
	ProviderName string
	LimitSource  string
	RemedyHint   string
	RetryAfter   time.Duration
}

func (e *HTTPError) Error() string {
	detail := strings.TrimSpace(e.Message)
	if detail == "" {
		detail = strings.TrimSpace(e.ResponseBody)
	}
	if detail == "" {
		detail = http.StatusText(e.StatusCode)
	}
	var classification []string
	if e.ProviderName != "" {
		classification = append(classification, e.ProviderName)
	}
	if e.LimitSource != "" {
		classification = append(classification, e.LimitSource)
	}
	if len(classification) > 0 {
		detail += " (" + strings.Join(classification, "; ") + ")"
	}
	if e.RemedyHint != "" {
		detail += ": " + e.RemedyHint
	}
	return fmt.Sprintf("openrouter: HTTP %d: %s", e.StatusCode, detail)
}

func newHTTPError(statusCode int, header http.Header, body []byte) *HTTPError {
	e := &HTTPError{
		StatusCode:   statusCode,
		ResponseBody: secret.Scrub(strings.TrimSpace(string(body))),
		RetryAfter:   parseRetryAfter(header.Get("Retry-After"), time.Now()),
	}
	var envelope struct {
		Error struct {
			Message  string `json:"message"`
			Metadata struct {
				ProviderName string `json:"provider_name"`
				LimitSource  string `json:"limit_source"`
				RemedyHint   string `json:"remedy_hint"`
			} `json:"metadata"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &envelope) == nil {
		e.Message = secret.Scrub(envelope.Error.Message)
		e.ProviderName = secret.Scrub(envelope.Error.Metadata.ProviderName)
		e.LimitSource = secret.Scrub(envelope.Error.Metadata.LimitSource)
		e.RemedyHint = secret.Scrub(envelope.Error.Metadata.RemedyHint)
	}
	return e
}

func parseRetryAfter(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
		if seconds <= 0 {
			return 0
		}
		return time.Duration(seconds) * time.Second
	}
	when, err := http.ParseTime(value)
	if err != nil || !when.After(now) {
		return 0
	}
	return when.Sub(now)
}
