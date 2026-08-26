package devices

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func storeFile(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "nested", "devices.json")
}

func TestPairingReturnsATokenThatAuthenticates(t *testing.T) {
	file := storeFile(t)
	store, err := Load(file)
	if err != nil {
		t.Fatal(err)
	}

	device, token, err := store.Add("Pixel 9", TierRead)
	if err != nil {
		t.Fatal(err)
	}
	if token == "" || device.ID == "" {
		t.Fatalf("device = %+v, token = %q", device, token)
	}

	got, ok := store.Authenticate(token)
	if !ok {
		t.Fatal("the token it just issued did not authenticate")
	}
	if got.ID != device.ID || got.Label != "Pixel 9" {
		t.Fatalf("authenticated as %+v", got)
	}
}

func TestTheTokenIsNeverStored(t *testing.T) {
	file := storeFile(t)
	store, _ := Load(file)
	_, token, err := store.Add("laptop", TierSteer)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(file); err != nil {
		t.Fatal(err)
	}

	body, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	// A stolen device file should be an inconvenience, not a compromise.
	if strings.Contains(string(body), token) {
		t.Fatalf("the token is on disk in clear text:\n%s", body)
	}
	if !strings.Contains(string(body), "laptop") {
		t.Fatalf("the label was not stored:\n%s", body)
	}
}

func TestTwoDevicesGetDifferentTokens(t *testing.T) {
	store, _ := Load(storeFile(t))
	_, first, _ := store.Add("phone", TierRead)
	_, second, _ := store.Add("tablet", TierRead)

	if first == second {
		t.Fatal("two devices were issued the same token")
	}
	if _, ok := store.Authenticate(first); !ok {
		t.Fatal("the first token stopped working when the second was issued")
	}
}

func TestRevokingOneDeviceLeavesTheOthers(t *testing.T) {
	store, _ := Load(storeFile(t))
	phone, phoneToken, _ := store.Add("phone", TierRead)
	_, tabletToken, _ := store.Add("tablet", TierRead)

	if !store.Revoke(phone.ID) {
		t.Fatal("revoke reported nothing was removed")
	}

	// Losing a phone must not cost everyone else their access.
	if _, ok := store.Authenticate(phoneToken); ok {
		t.Fatal("a revoked device still authenticates")
	}
	if _, ok := store.Authenticate(tabletToken); !ok {
		t.Fatal("revoking one device revoked another")
	}
	if store.Revoke(phone.ID) {
		t.Fatal("revoking a device that is gone reported success")
	}
}

func TestAnUnknownTokenIsRejected(t *testing.T) {
	store, _ := Load(storeFile(t))
	store.Add("phone", TierRead)

	for _, token := range []string{"", "not-a-token", strings.Repeat("a", 43)} {
		if _, ok := store.Authenticate(token); ok {
			t.Fatalf("%q authenticated", token)
		}
	}
}

func TestDevicesSurviveAReload(t *testing.T) {
	file := storeFile(t)
	store, _ := Load(file)
	device, token, _ := store.Add("phone", TierSteer)
	if err := store.Save(file); err != nil {
		t.Fatal(err)
	}

	reloaded, err := Load(file)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := reloaded.Authenticate(token)
	if !ok {
		t.Fatal("a paired device stopped working after a restart")
	}
	if got.ID != device.ID || got.Tier != TierSteer {
		t.Fatalf("reloaded as %+v", got)
	}
}

func TestUsingADeviceRecordsThat(t *testing.T) {
	store, _ := Load(storeFile(t))
	store.Now = func() time.Time { return time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC) }
	_, token, _ := store.Add("phone", TierRead)

	store.Now = func() time.Time { return time.Date(2026, 8, 27, 9, 30, 0, 0, time.UTC) }
	if _, ok := store.Authenticate(token); !ok {
		t.Fatal("authenticate failed")
	}

	// "Which of these is still in use" is the question someone asks before
	// revoking, and a list that cannot answer it invites revoking the wrong one.
	listed := store.List()
	if len(listed) != 1 {
		t.Fatalf("listed %d devices", len(listed))
	}
	if !listed[0].LastSeen.Equal(time.Date(2026, 8, 27, 9, 30, 0, 0, time.UTC)) {
		t.Fatalf("last seen = %v", listed[0].LastSeen)
	}
}

func TestAMissingFileIsAnEmptyStore(t *testing.T) {
	store, err := Load(storeFile(t))
	if err != nil {
		t.Fatalf("loading a file that is not there: %v", err)
	}
	if len(store.List()) != 0 {
		t.Fatalf("got %d devices", len(store.List()))
	}
}

func TestACorruptFileNamesItself(t *testing.T) {
	file := storeFile(t)
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := Load(file)
	if err == nil || !strings.Contains(err.Error(), file) {
		t.Fatalf("err = %v, want it to name %s", err, file)
	}
}

func TestTheFileIsPrivate(t *testing.T) {
	file := storeFile(t)
	store, _ := Load(file)
	store.Add("phone", TierRead)
	if err := store.Save(file); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("mode = %v, want 0600", perm)
	}
}

func TestTheStoreSurvivesConcurrentUse(t *testing.T) {
	store, _ := Load(storeFile(t))
	_, token, _ := store.Add("phone", TierRead)

	// Every HTTP request authenticates, and authenticating writes a
	// last-seen time. Two devices talking at once is the normal case for a
	// server, not an edge one.
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			store.Authenticate(token)
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			store.List()
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, _ = store.Add("another", TierRead)
		}()
	}
	wg.Wait()
}
