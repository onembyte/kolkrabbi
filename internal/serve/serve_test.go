package serve

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/bus"
	"github.com/onembyte/kolkrabbi/internal/xid"
	"github.com/onembyte/kolkrabbi/protocol"
)

type fakeResolver struct {
	resolvedID       string
	resolvedDecision protocol.PermissionDecision
	err              error
}

func (r *fakeResolver) ResolvePermission(id string, decision protocol.PermissionDecision) error {
	r.resolvedID = id
	r.resolvedDecision = decision
	return r.err
}

func TestHelloAndHealthEndpointsUnauthenticated(t *testing.T) {
	b, err := bus.New(xid.New(xid.Session), bus.Options{})
	if err != nil {
		t.Fatal(err)
	}

	handler, err := Mux(Options{
		Bus:   b,
		Token: "secret-token",
		Addr:  "127.0.0.1:8080",
	})
	if err != nil {
		t.Fatal(err)
	}

	// 1. GET / (hello)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / returned %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"name":"kolkrabbi"`) {
		t.Fatalf("unexpected body: %s", rec.Body.String())
	}

	// 2. GET /v1/health
	req = httptest.NewRequest(http.MethodGet, "/v1/health", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /v1/health returned %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"status":"ok"`) {
		t.Fatalf("unexpected health body: %s", rec.Body.String())
	}
}

func TestAuthMiddlewareProtectedRoutes(t *testing.T) {
	b, err := bus.New(xid.New(xid.Session), bus.Options{})
	if err != nil {
		t.Fatal(err)
	}

	handler, err := Mux(Options{
		Bus:   b,
		Token: "secret-token",
		Addr:  "127.0.0.1:8080",
	})
	if err != nil {
		t.Fatal(err)
	}

	// 1. Missing auth header -> 401
	req := httptest.NewRequest(http.MethodGet, "/v1/events", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized, got %d", rec.Code)
	}

	// 2. Wrong token -> 403
	req = httptest.NewRequest(http.MethodGet, "/v1/events", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden, got %d", rec.Code)
	}
}

func TestNonLoopbackWithoutTokenRefused(t *testing.T) {
	b, err := bus.New(xid.New(xid.Session), bus.Options{})
	if err != nil {
		t.Fatal(err)
	}

	_, err = Mux(Options{
		Bus:   b,
		Token: "",
		Addr:  "0.0.0.0:8080",
	})
	if err == nil {
		t.Fatal("expected error binding to 0.0.0.0 without a token")
	}
}

func TestPermissionResolveEndpoint(t *testing.T) {
	b, err := bus.New(xid.New(xid.Session), bus.Options{})
	if err != nil {
		t.Fatal(err)
	}

	resolver := &fakeResolver{}
	handler, err := Mux(Options{
		Bus:      b,
		Token:    "test-token",
		Addr:     "127.0.0.1:8080",
		Resolver: resolver,
	})
	if err != nil {
		t.Fatal(err)
	}

	body := `{"id":"perm_123","decision":"allow"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/permissions/resolve", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
	}
	if resolver.resolvedID != "perm_123" || resolver.resolvedDecision != protocol.PermissionDecisionAllow {
		t.Fatalf("unexpected resolver state: %+v", resolver)
	}
}

func TestSSEStreamAndHeartbeat(t *testing.T) {
	b, err := bus.New(xid.New(xid.Session), bus.Options{})
	if err != nil {
		t.Fatal(err)
	}

	handler, err := Mux(Options{
		Bus:          b,
		Token:        "test-token",
		Addr:         "127.0.0.1:8080",
		PingInterval: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(handler)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/v1/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer test-token")

	// Publish an event before or right as stream connects
	go func() {
		time.Sleep(10 * time.Millisecond)
		_, _ = b.Publish(bus.Event{Turn: "t_01ARYZ6S41TSV4RRFFQ69G5FAW", Type: protocol.EventMessageDelta, Data: json.RawMessage(`{"text":"hello world"}`)})
	}()

	resp, err := http.DefaultClient.Do(req)
	if err != nil && !strings.Contains(err.Error(), "context") {
		t.Fatalf("Do: %v", err)
	}
	if resp != nil {
		defer resp.Body.Close()
		scanner := bufio.NewScanner(resp.Body)
		var collected strings.Builder
		for scanner.Scan() {
			line := scanner.Text()
			collected.WriteString(line + "\n")
			if strings.Contains(collected.String(), "message.delta") && strings.Contains(collected.String(), ": ping") {
				break
			}
		}
		body := collected.String()
		if !strings.Contains(body, "retry: 1000") {
			t.Errorf("missing retry header in SSE stream: %s", body)
		}
		if !strings.Contains(body, "message.delta") {
			t.Errorf("missing event in SSE stream: %s", body)
		}
	}
}
