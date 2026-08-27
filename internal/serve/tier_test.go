package serve

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/devices"
)

func tierFixture(t *testing.T) (http.Handler, *devices.Store) {
	t.Helper()
	store, err := devices.Load(filepath.Join(t.TempDir(), "devices.json"))
	if err != nil {
		t.Fatal(err)
	}
	handler, err := Mux(Options{
		Bus: testBus(t), Addr: ":8080", Token: "operator-token", Devices: store,
	})
	if err != nil {
		t.Fatal(err)
	}
	return handler, store
}

// as makes one request and returns its status.
//
// The context is cancelled before the request runs: /v1/events is a stream that
// never ends on its own, and a test that waits for it to finish is a test that
// hangs. Authentication happens before the handler either way, so a refusal is
// still observed exactly.
func as(t *testing.T, handler http.Handler, token, method, path string) int {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, nil).WithContext(ctx)
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	handler.ServeHTTP(recorder, request)
	return recorder.Code
}

func TestAReadDeviceCanWatchButNotAct(t *testing.T) {
	handler, store := tierFixture(t)
	_, token, err := store.Add("phone", devices.TierRead)
	if err != nil {
		t.Fatal(err)
	}

	if code := as(t, handler, token, http.MethodGet, "/v1/events"); code == http.StatusForbidden {
		t.Fatal("a read device could not watch the session")
	}
	// The safe posture for a phone paired on a train is that it cannot
	// approve what the agent does next.
	if code := as(t, handler, token, http.MethodPost, "/v1/permissions/resolve"); code != http.StatusForbidden {
		t.Fatalf("a read device answered a permission prompt: %d", code)
	}
}

func TestASteerDeviceCanAct(t *testing.T) {
	handler, store := tierFixture(t)
	_, token, _ := store.Add("laptop", devices.TierSteer)

	for _, route := range []struct{ method, path string }{
		{http.MethodGet, "/v1/events"},
		{http.MethodPost, "/v1/permissions/resolve"},
	} {
		if code := as(t, handler, token, route.method, route.path); code == http.StatusForbidden {
			t.Fatalf("a steer device was refused %s %s", route.method, route.path)
		}
	}
}

func TestTheOperatorTokenIsNotLimited(t *testing.T) {
	handler, _ := tierFixture(t)

	// It is the token the person running the server chose for themselves.
	// Tiering it would be Kolkrabbi restricting its own operator.
	if code := as(t, handler, "operator-token", http.MethodPost, "/v1/permissions/resolve"); code == http.StatusForbidden {
		t.Fatal("the operator token was tier-limited")
	}
}

func TestARevokedDeviceStopsWorking(t *testing.T) {
	handler, store := tierFixture(t)
	device, token, _ := store.Add("lost phone", devices.TierSteer)

	if !store.Revoke(device.ID) {
		t.Fatal("revoke failed")
	}

	if code := as(t, handler, token, http.MethodGet, "/v1/events"); code != http.StatusForbidden {
		t.Fatalf("a revoked device still reached the stream: %d", code)
	}
}

func TestAnUnknownTokenIsRefused(t *testing.T) {
	handler, _ := tierFixture(t)

	if code := as(t, handler, "nonsense", http.MethodGet, "/v1/events"); code != http.StatusForbidden {
		t.Fatalf("an unknown token answered %d", code)
	}
	if code := as(t, handler, "", http.MethodGet, "/v1/events"); code != http.StatusUnauthorized {
		t.Fatalf("a missing token answered %d", code)
	}
}

func TestUsingADeviceIsRecorded(t *testing.T) {
	handler, store := tierFixture(t)
	device, token, _ := store.Add("phone", devices.TierRead)

	as(t, handler, token, http.MethodGet, "/v1/events")

	listed := store.List()
	if len(listed) != 1 || listed[0].ID != device.ID {
		t.Fatalf("devices = %+v", listed)
	}
	// Last-seen is the only way to answer "which of these is still in use"
	// before revoking one.
	if listed[0].LastSeen.IsZero() {
		t.Fatal("using a device over HTTP did not record it")
	}
}

func TestOnlyTheActingRoutesNeedSteer(t *testing.T) {
	// The ratchet: adding a write endpoint without listing it here leaves it
	// answerable by any paired device, which is the failure this exists to
	// make loud.
	//
	// It made exactly that noise when /v1/turns was mounted (I26.7b), which is
	// why this list grew on purpose rather than by accident. Both entries let a
	// device *act*: one answers a permission prompt, the other asks for work.
	// Anything authenticated and absent from this list is readable only.
	want := map[string]bool{
		"/v1/permissions/resolve": true,
		"/v1/turns":               true,
	}

	if len(steerRoutes) != len(want) {
		t.Fatalf("steer routes = %v, want exactly %v", steerRoutes, want)
	}
	for path := range want {
		if !steerRoutes[path] {
			t.Fatalf("%s no longer requires steer", path)
		}
	}
	for path := range steerRoutes {
		if !want[path] {
			t.Fatalf("%s was added without updating this test", path)
		}
	}
}

func TestWithNoTokenAtAllNothingIsTiered(t *testing.T) {
	// An empty token is only reachable on loopback — I26.1 refuses to serve
	// anything else without one — so this is the local case, and a local
	// session must not have to pair with itself.
	handler, err := Mux(Options{Bus: testBus(t), Addr: "127.0.0.1:8080"})
	if err != nil {
		t.Fatal(err)
	}
	if code := as(t, handler, "", http.MethodPost, "/v1/permissions/resolve"); code == http.StatusForbidden {
		t.Fatal("a loopback server with no token refused its own operator")
	}
}
