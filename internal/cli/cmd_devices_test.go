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

func TestDevicesRevokeRemovesOneAndSaysSo(t *testing.T) {
	paired := pairedDevices(t, "phone", "old laptop")

	a, out, _ := newTestApp(t, "")
	if code := a.main(context.Background(), []string{"devices", "revoke", paired[1].ID}); code != ExitOK {
		t.Fatalf("devices revoke exit = %d, want ExitOK", code)
	}
	if !strings.Contains(out.String(), "old laptop") {
		t.Errorf("revoke output %q does not name what was removed", out.String())
	}

	// Gone from disk, not just from the in-memory copy that did the removing.
	// A revoke that is not saved is a revoke that lasts until the next command.
	a, out, _ = newTestApp(t, "")
	if code := a.main(context.Background(), []string{"devices"}); code != ExitOK {
		t.Fatalf("devices exit = %d", code)
	}
	if strings.Contains(out.String(), paired[1].ID) {
		t.Errorf("the revoked device is still listed:\n%s", out.String())
	}
	if !strings.Contains(out.String(), paired[0].ID) {
		t.Errorf("revoke took the wrong device, or all of them:\n%s", out.String())
	}
}

// A typo'd revoke that reports success is worse than an error: it tells someone
// a device they still worry about is gone.
func TestDevicesRevokeRefusesAnIDThatIsNotThere(t *testing.T) {
	paired := pairedDevices(t, "phone")

	a, _, errOut := newTestApp(t, "")
	if code := a.main(context.Background(), []string{"devices", "revoke", "not-an-id"}); code == ExitOK {
		t.Fatal("revoking an unknown id succeeded, want a refusal")
	}
	if !strings.Contains(errOut.String(), paired[0].ID) {
		t.Errorf("the refusal %q does not name what is actually paired", errOut.String())
	}
}

func TestDevicesRevokeNeedsAnID(t *testing.T) {
	pairedDevices(t, "phone")
	a, _, errOut := newTestApp(t, "")
	if code := a.main(context.Background(), []string{"devices", "revoke"}); code == ExitOK {
		t.Fatal("`devices revoke` with no id succeeded, want usage")
	}
	if !strings.Contains(errOut.String(), "revoke <id>") {
		t.Errorf("usage %q does not show the form", errOut.String())
	}
}
