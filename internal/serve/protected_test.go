package serve

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// protectedRoutes is every path that must not answer without the token.
//
// Written as a list so that adding an endpoint without adding it here is a
// visible omission rather than a silent one: a route that nobody remembered to
// protect looks exactly like a route nobody remembered to test.
var protectedRoutes = []struct {
	method, path string
}{
	{http.MethodGet, "/v1/events"},
	{http.MethodPost, "/v1/permissions/resolve"},
}

func servedWithToken(t *testing.T, token string) http.Handler {
	t.Helper()
	handler, err := Mux(Options{Bus: testBus(t), Addr: ":8080", Token: token})
	if err != nil {
		t.Fatal(err)
	}
	return handler
}

func TestEveryProtectedRouteRefusesWithoutAToken(t *testing.T) {
	handler := servedWithToken(t, "s3cret")

	for _, route := range protectedRoutes {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(route.method, route.path, nil))

		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s answered %d without a token", route.method, route.path, recorder.Code)
		}
	}
}

func TestAWrongTokenIsRefusedSeparatelyFromAMissingOne(t *testing.T) {
	handler := servedWithToken(t, "s3cret")

	for _, route := range protectedRoutes {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(route.method, route.path, nil)
		request.Header.Set("Authorization", "Bearer wrong")
		handler.ServeHTTP(recorder, request)

		// 401 means "who are you", 403 means "not you". Collapsing them tells
		// someone probing whether the token exists at all.
		if recorder.Code != http.StatusForbidden {
			t.Fatalf("%s %s answered %d to a wrong token", route.method, route.path, recorder.Code)
		}
	}
}

func TestTheHealthAndHelloRoutesStayOpen(t *testing.T) {
	handler := servedWithToken(t, "s3cret")

	// A liveness probe that needs a credential is a liveness probe nothing can
	// use, and neither route says anything about the session.
	for _, path := range []string{"/", "/v1/health"} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s answered %d", path, recorder.Code)
		}
	}
}

func TestAnUnknownRouteDoesNotLeakWhetherItExists(t *testing.T) {
	handler := servedWithToken(t, "s3cret")

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/sessions/secret-id", nil))

	// Answering 404 to an unauthenticated caller maps the surface for them.
	if recorder.Code == http.StatusNotFound {
		t.Fatalf("an unknown route answered 404 without a token, mapping the API")
	}
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("answered %d, want unauthorized", recorder.Code)
	}
}

func TestTheTokenIsNeverEchoedBack(t *testing.T) {
	handler := servedWithToken(t, "s3cret-value")

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v1/events", nil)
	request.Header.Set("Authorization", "Bearer wrong-but-long")
	handler.ServeHTTP(recorder, request)

	body := recorder.Body.String()
	if strings.Contains(body, "s3cret-value") || strings.Contains(body, "wrong-but-long") {
		t.Fatalf("the response quoted a credential: %q", body)
	}
}

func TestOnlyTwoRoutesAnswerWithoutACredential(t *testing.T) {
	// A ratchet on the policy rather than on the handlers. Every route is
	// protected by default because the middleware wraps the whole mux, so the
	// only way to expose something is to add it here — which should require
	// changing this test and saying why.
	want := map[string]bool{"/": true, "/v1/health": true}

	if len(openRoutes) != len(want) {
		t.Fatalf("open routes = %v, want exactly %v", openRoutes, want)
	}
	for path := range want {
		if !openRoutes[path] {
			t.Fatalf("%s is no longer open", path)
		}
	}
	for path := range openRoutes {
		if !want[path] {
			t.Fatalf("%s was opened without updating this test", path)
		}
	}
}
