package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/devices"
)

func pairFixture(t *testing.T) (http.Handler, *devices.Pairing, *devices.Store) {
	t.Helper()
	store, err := devices.Load(filepath.Join(t.TempDir(), "devices.json"))
	if err != nil {
		t.Fatal(err)
	}
	pairing := &devices.Pairing{}
	handler, err := Mux(Options{
		Bus: testBus(t), Addr: ":8080", Token: "s3cret",
		Devices: store, Pairing: pairing,
		DeviceFile: filepath.Join(t.TempDir(), "devices.json"),
	})
	if err != nil {
		t.Fatal(err)
	}
	return handler, pairing, store
}

func redeem(t *testing.T, handler http.Handler, code string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	body := strings.NewReader(`{"code":"` + code + `","label":"Pixel 9"}`)
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1/pair", body))
	return recorder
}

func TestPairingIsNotEvenAnEndpointUntilItIsArmed(t *testing.T) {
	handler, _, _ := pairFixture(t)

	got := redeem(t, handler, "123456")

	// Not 401: an unarmed pairing route should not advertise that pairing
	// exists on this machine at all.
	if got.Code != http.StatusNotFound {
		t.Fatalf("answered %d, want 404 while unarmed", got.Code)
	}
}

func TestArmedPairingIssuesADeviceToken(t *testing.T) {
	handler, pairing, store := pairFixture(t)
	code, _, err := pairing.Arm()
	if err != nil {
		t.Fatal(err)
	}

	got := redeem(t, handler, code)

	if got.Code != http.StatusOK {
		t.Fatalf("answered %d: %s", got.Code, got.Body.String())
	}
	var reply struct {
		Token    string `json:"token"`
		DeviceID string `json:"device_id"`
		Tier     string `json:"tier"`
	}
	if err := json.Unmarshal(got.Body.Bytes(), &reply); err != nil {
		t.Fatalf("reply is not JSON: %s", got.Body.String())
	}
	if reply.Token == "" || reply.DeviceID == "" {
		t.Fatalf("reply = %+v", reply)
	}
	// Read is the default: the safe posture for a device someone just paired
	// on a train is that it cannot approve anything.
	if reply.Tier != string(devices.TierRead) {
		t.Fatalf("tier = %q, want read", reply.Tier)
	}
	if _, ok := store.Authenticate(reply.Token); !ok {
		t.Fatal("the issued token does not authenticate against the store")
	}
}

func TestAWrongCodeDoesNotIssueAnything(t *testing.T) {
	handler, pairing, store := pairFixture(t)
	pairing.Arm()

	got := redeem(t, handler, "000000")

	if got.Code != http.StatusForbidden {
		t.Fatalf("answered %d, want forbidden", got.Code)
	}
	if len(store.List()) != 0 {
		t.Fatalf("a device was created anyway: %+v", store.List())
	}
}

func TestPairingClosesAfterOneSuccess(t *testing.T) {
	handler, pairing, _ := pairFixture(t)
	code, _, _ := pairing.Arm()

	if got := redeem(t, handler, code); got.Code != http.StatusOK {
		t.Fatalf("first redeem answered %d", got.Code)
	}
	if got := redeem(t, handler, code); got.Code != http.StatusNotFound {
		t.Fatalf("second redeem answered %d, want 404", got.Code)
	}
}

func TestTheAttemptCapClosesTheEndpoint(t *testing.T) {
	handler, pairing, _ := pairFixture(t)
	code, _, _ := pairing.Arm()

	for range 5 {
		redeem(t, handler, "000000")
	}

	if got := redeem(t, handler, code); got.Code != http.StatusNotFound {
		t.Fatalf("answered %d after the cap, want 404", got.Code)
	}
}

func TestPairingSurvivesARestart(t *testing.T) {
	file := filepath.Join(t.TempDir(), "devices.json")
	store, _ := devices.Load(file)
	pairing := &devices.Pairing{}
	handler, err := Mux(Options{
		Bus: testBus(t), Addr: ":8080", Token: "s3cret",
		Devices: store, Pairing: pairing, DeviceFile: file,
	})
	if err != nil {
		t.Fatal(err)
	}
	code, _, _ := pairing.Arm()
	got := redeem(t, handler, code)
	if got.Code != http.StatusOK {
		t.Fatalf("redeem answered %d", got.Code)
	}

	// A device that has to pair again after every restart is a device nobody
	// pairs.
	reloaded, err := devices.Load(file)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.List()) != 1 {
		t.Fatalf("the paired device was not written: %+v", reloaded.List())
	}
}

func TestABodyThatIsNotJSONIsRejected(t *testing.T) {
	handler, pairing, _ := pairFixture(t)
	pairing.Arm()

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1/pair", strings.NewReader("{not json")))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("answered %d, want bad request", recorder.Code)
	}
}

func TestOnlyPostPairs(t *testing.T) {
	handler, pairing, _ := pairFixture(t)
	pairing.Arm()

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/pair", nil))

	// A GET that pairs is a GET a link can trigger.
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("answered %d to GET, want method not allowed", recorder.Code)
	}
}
