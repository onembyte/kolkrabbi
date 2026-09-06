package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// bodyRecorder keeps the JSON body of the last request and answers with an
// empty stream, so a test can read what would have been sent.
type bodyRecorder struct{ body map[string]any }

func (r *bodyRecorder) RoundTrip(req *http.Request) (*http.Response, error) {
	raw, _ := io.ReadAll(req.Body)
	r.body = map[string]any{}
	_ = json.Unmarshal(raw, &r.body)
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader("data: [DONE]\n\n")), Request: req}, nil
}

// V34.4c.1b: kolk's rung is projected onto the vendor's own reasoning word,
// and a vendor request carries none of the gateway's private fields. The
// gateway and a compatible endpoint are sent exactly what they were before.
func TestAVendorRequestCarriesTheVendorsReasoningWordAndNoGatewayFields(t *testing.T) {
	send := func(t *testing.T, c *Client, effort string) map[string]any {
		t.Helper()
		rec := &bodyRecorder{}
		if c.auth != nil {
			c.auth.Base = rec
		} else {
			c.HTTPClient.Transport = rec
		}
		_, _, _ = c.StreamChat(WithEffort(context.Background(), effort), "m", []Message{{Role: "user", Content: "hi"}}, nil, nil)
		if rec.body == nil {
			t.Fatal("no request reached the transport")
		}
		return rec.body
	}
	xai, err := NewVendorClient("xai", "xai-"+strings.Repeat("0", 24))
	if err != nil {
		t.Fatal(err)
	}
	for rung, want := range map[string]string{"low": "low", "medium": "medium", "high": "high", "max": "xhigh", "ultra": "xhigh"} {
		body := send(t, xai, rung)
		if body["reasoning_effort"] != want {
			t.Fatalf("xai at %s: reasoning_effort = %v, want %q", rung, body["reasoning_effort"], want)
		}
		if _, leaked := body["usage"]; leaked {
			t.Fatalf("xai request carries the gateway's usage.include: %v", body)
		}
	}
	google, err := NewVendorClient("google", "AIza"+strings.Repeat("a", 35))
	if err != nil {
		t.Fatal(err)
	}
	for rung, want := range map[string]string{"low": "low", "max": "high", "ultra": "high"} {
		if body := send(t, google, rung); body["reasoning_effort"] != want {
			t.Fatalf("google at %s: reasoning_effort = %v, want %q", rung, body["reasoning_effort"], want)
		}
	}
	if body := send(t, xai, ""); body["reasoning_effort"] != nil {
		t.Fatalf("no effort in the context still sent %v", body["reasoning_effort"])
	}

	gateway := newCanonicalOpenRouterClient(t, "sk-or-v1-"+strings.Repeat("0", 24))
	body := send(t, gateway, "max")
	if body["reasoning_effort"] != nil || body["usage"] == nil {
		t.Fatalf("the gateway's request changed: %v", body)
	}
	compatible := NewCompatibleClient("http://compatible.invalid/v1")
	body = send(t, compatible, "max")
	if body["reasoning_effort"] != nil || body["usage"] != nil {
		t.Fatalf("a compatible request changed: %v", body)
	}
}
