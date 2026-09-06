package serve

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/dash"
	"github.com/onembyte/kolkrabbi/internal/devices"
)

// MANY: a paired device of either tier sees every session on the machine as
// the dash draws it; without a cookie it is 401; a server with no listing
// says so rather than showing an empty box.
func TestClientSessionsPageListsEverySession(t *testing.T) {
	handler, store, _, _, _ := clientFixture(t)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/client/sessions", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no cookie: %d", rec.Code)
	}
	_, token, _ := store.Add("phone", devices.TierRead)
	req := httptest.NewRequest(http.MethodGet, "/v1/client/sessions", nil)
	req.AddCookie(deviceCookie(token))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "not attached to a machine") {
		t.Fatalf("no listing: %d %q", rec.Code, rec.Body.String())
	}

	b := testBus(t)
	h, err := Mux(Options{Bus: b, Token: "main-token", Addr: "127.0.0.1:8080", Devices: store, Sessions: func(context.Context) ([]dash.SessionCard, []dash.SharedCheckout) {
		return []dash.SessionCard{
			{ID: "s1", Name: "one", Model: "m", CWD: "/w", Live: true, BlockedOn: "bash", Branch: "main", Dirty: 2, VCSKnown: true},
			{ID: "s2", Name: "two", Model: "m", CWD: "/w", Live: true},
		}, []dash.SharedCheckout{{Dir: "/w", Sessions: []string{"s1", "s2"}}}
	}})
	if err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodGet, "/v1/client/sessions", nil)
	req.AddCookie(deviceCookie(token))
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	body := rec.Body.String()
	for _, want := range []string{"blocked", "bash", "on main · 2 files changed", "/w", "s2", "<script"} {
		if want == "<script" {
			if strings.Contains(body, want) {
				t.Fatalf("sessions page carries script")
			}
			continue
		}
		if !strings.Contains(body, want) {
			t.Fatalf("sessions page lacks %q:\n%s", want, body)
		}
	}
}
