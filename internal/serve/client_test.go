package serve

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/bus"
	"github.com/onembyte/kolkrabbi/internal/devices"
	"github.com/onembyte/kolkrabbi/protocol"
)

type clientStarter struct{ prompts []string }

func (r *clientStarter) StartTurn(prompt string) error {
	r.prompts = append(r.prompts, prompt)
	return nil
}

func clientFixture(t *testing.T) (http.Handler, *devices.Store, *devices.Pairing, *clientStarter, *bus.Bus) {
	t.Helper()
	b := testBus(t)
	store, err := devices.Load(filepath.Join(t.TempDir(), "devices.json"))
	if err != nil {
		t.Fatal(err)
	}
	pairing := &devices.Pairing{}
	starter := &clientStarter{}
	handler, err := Mux(Options{Bus: b, Token: "main-token", Addr: "127.0.0.1:8080", Devices: store, Pairing: pairing, Turns: starter, PingInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	return handler, store, pairing, starter, b
}

func deviceCookie(token string) *http.Cookie { return &http.Cookie{Name: clientCookie, Value: token} }

// I26.7: the client is a server-rendered page behind the same auth as the
// API, reached with a device cookie the pairing form set, never a token in a
// URL. Without a cookie it is 401; a read-tier device sees the stream and is
// told it may watch; a steer-tier device also gets the form.
func TestClientPageIsAuthenticatedAndTiered(t *testing.T) {
	handler, store, _, _, _ := clientFixture(t)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/client", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no cookie: %d", rec.Code)
	}
	_, readToken, _ := store.Add("phone", devices.TierRead)
	req := httptest.NewRequest(http.MethodGet, "/v1/client", nil)
	req.AddCookie(deviceCookie(readToken))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, `src="/v1/client/stream"`) || !strings.Contains(body, "may watch") || strings.Contains(body, "<script") {
		t.Fatalf("read page: %d %q", rec.Code, body)
	}
	if strings.Contains(body, `action="/v1/client/turn"`) {
		t.Fatal("a read-tier device was offered the steer form")
	}
	_, steerToken, _ := store.Add("laptop", devices.TierSteer)
	req = httptest.NewRequest(http.MethodGet, "/v1/client", nil)
	req.AddCookie(deviceCookie(steerToken))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `action="/v1/client/turn"`) || !strings.Contains(rec.Body.String(), "manifest.json") {
		t.Fatalf("steer page: %d %q", rec.Code, rec.Body.String())
	}
	// The cookie is honoured only under the client routes; the API keeps its bearer.
	req = httptest.NewRequest(http.MethodGet, "/v1/events", nil)
	req.AddCookie(deviceCookie(steerToken))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("a cookie opened the API: %d", rec.Code)
	}
}

// Steering from the page: a same-origin form post from a steer device
// starts the turn and redirects back; a read device is refused; a post with
// no same-origin evidence is refused as cross-site.
func TestClientTurnFormSteersOnlyFromASteerDeviceSameOrigin(t *testing.T) {
	handler, store, _, starter, _ := clientFixture(t)
	_, steerToken, _ := store.Add("laptop", devices.TierSteer)
	_, readToken, _ := store.Add("phone", devices.TierRead)
	post := func(token, origin string) *httptest.ResponseRecorder {
		form := url.Values{"prompt": {"  run the tests  "}}
		req := httptest.NewRequest(http.MethodPost, "/v1/client/turn", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Host = "127.0.0.1:8080"
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		req.AddCookie(deviceCookie(token))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}
	if rec := post(steerToken, "http://127.0.0.1:8080"); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/v1/client" {
		t.Fatalf("steer post: %d %q %q", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	if len(starter.prompts) != 1 || starter.prompts[0] != "run the tests" {
		t.Fatalf("prompts = %q", starter.prompts)
	}
	if rec := post(readToken, "http://127.0.0.1:8080"); rec.Code != http.StatusForbidden {
		t.Fatalf("read post: %d", rec.Code)
	}
	if rec := post(steerToken, "http://evil.example"); rec.Code != http.StatusForbidden {
		t.Fatalf("cross-site post: %d", rec.Code)
	}
	if rec := post(steerToken, ""); rec.Code != http.StatusForbidden {
		t.Fatalf("post with no origin: %d", rec.Code)
	}
	if len(starter.prompts) != 1 {
		t.Fatalf("a refused post started a turn: %q", starter.prompts)
	}
}

// The stream is chunked HTML with no script: replayed and live events become
// lines as they arrive. Read over a real connection, incrementally, since a
// recorder's buffer cannot be read while the handler writes it.
func TestClientStreamRendersEventsAsTheyArrive(t *testing.T) {
	handler, store, _, _, b := clientFixture(t)
	_, token, _ := store.Add("phone", devices.TierRead)
	data, _ := json.Marshal(protocol.TurnStartedData{Input: "hello there", Model: "m", Mode: "code", Effort: "medium"})
	if _, err := b.Publish(bus.Event{Turn: "t_01ARYZ6S41TSV4RRFFQ69G5FAW", Type: protocol.EventTurnStarted, Data: data}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(handler)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/v1/client/stream", nil)
	req.AddCookie(deviceCookie(token))
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if !strings.Contains(resp.Header.Get("Content-Type"), "text/html") {
		t.Fatalf("content type = %q", resp.Header.Get("Content-Type"))
	}
	var got strings.Builder
	buf := make([]byte, 512)
	for !strings.Contains(got.String(), "hello there") {
		n, err := resp.Body.Read(buf)
		got.Write(buf[:n])
		if err != nil {
			t.Fatalf("stream ended before the event: %v\n%s", err, got.String())
		}
	}
	if strings.Contains(got.String(), "<script") {
		t.Fatalf("stream carries script:\n%s", got.String())
	}
	_ = io.Discard
}

// Pairing from a browser: while armed, GET /v1/pair is the form; a form post
// with the right code sets the device cookie and redirects to the client,
// and the token itself is never in a URL. Not armed, both are 404.
func TestPairingFormSetsTheDeviceCookie(t *testing.T) {
	handler, _, pairing, _, _ := clientFixture(t)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/pair", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("form while not armed: %d", rec.Code)
	}
	code, _, err := pairing.Arm()
	if err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/pair", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `name="code"`) {
		t.Fatalf("form while armed: %d %q", rec.Code, rec.Body.String())
	}
	form := url.Values{"code": {code}, "label": {"my phone"}}
	req := httptest.NewRequest(http.MethodPost, "/v1/pair", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/v1/client" {
		t.Fatalf("form redeem: %d %q %q", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	var cookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == clientCookie {
			cookie = c
		}
	}
	if cookie == nil || cookie.Value == "" || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || !strings.HasPrefix(cookie.Path, "/v1") {
		t.Fatalf("cookie = %+v", cookie)
	}
	if strings.Contains(rec.Header().Get("Location"), cookie.Value) {
		t.Fatal("the token is in the redirect URL")
	}
}

// The open-route set is still exactly two; the client joins the guarded
// routes and the form post joins the steer routes.
func TestClientDoesNotWidenTheOpenRoutes(t *testing.T) {
	if len(openRoutes) != 2 || !openRoutes["/"] || !openRoutes["/v1/health"] {
		t.Fatalf("open routes = %v", openRoutes)
	}
	if !steerRoutes["/v1/client/turn"] {
		t.Fatal("the client's turn post is not a steer route")
	}
}
