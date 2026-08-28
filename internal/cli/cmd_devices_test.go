package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/devices"
)

// pairedDevices writes a device file into the isolated home, as pairing would.
func pairedDevices(t *testing.T, labels ...string) []devices.Device {
	t.Helper()
	d := isolateHome(t)
	store, err := devices.Load(d.DevicesFile())
	if err != nil {
		t.Fatal(err)
	}
	added := make([]devices.Device, 0, len(labels))
	for _, label := range labels {
		device, _, err := store.Add(label, devices.TierRead)
		if err != nil {
			t.Fatal(err)
		}
		added = append(added, device)
	}
	if err := store.Save(d.DevicesFile()); err != nil {
		t.Fatal(err)
	}
	return added
}

func TestDevicesListsWhatIsPaired(t *testing.T) {
	paired := pairedDevices(t, "phone", "old laptop")

	a, out, _ := newTestApp(t, "")
	if code := a.main(context.Background(), []string{"devices"}); code != ExitOK {
		t.Fatalf("devices exit = %d, want ExitOK", code)
	}
	listing := out.String()
	for _, device := range paired {
		if !strings.Contains(listing, device.Label) {
			t.Errorf("listing does not name %q:\n%s", device.Label, listing)
		}
		if !strings.Contains(listing, device.ID) {
			t.Errorf("listing does not show the id of %q, so nothing can be revoked:\n%s", device.Label, listing)
		}
	}
	if !strings.Contains(listing, string(devices.TierRead)) {
		t.Errorf("listing does not show what a device is allowed to do:\n%s", listing)
	}
}

// A blank screen cannot be told from a command that failed, so nothing paired
// has to be a sentence.
func TestDevicesSaysSoWhenNothingIsPaired(t *testing.T) {
	isolateHome(t)
	a, out, _ := newTestApp(t, "")
	if code := a.main(context.Background(), []string{"devices"}); code != ExitOK {
		t.Fatalf("devices exit = %d, want ExitOK", code)
	}
	if strings.TrimSpace(out.String()) == "" {
		t.Fatal("devices printed nothing at all when none are paired")
	}
	if !strings.Contains(out.String(), "no devices") {
		t.Errorf("output %q does not say plainly that nothing is paired", out.String())
	}
}

func TestDevicesIsInTheCommandTable(t *testing.T) {
	if lookupCommand("devices") == nil {
		t.Fatal("devices is not a command, so `kolk help` cannot mention it")
	}
}
