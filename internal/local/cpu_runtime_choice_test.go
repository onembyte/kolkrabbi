package local

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestStartingOnCPUDoesNotForgetACompanionFailure(t *testing.T) {
	t.Run("recorded before startup", func(t *testing.T) { testCPUCompanionFailure(t, true) })
	t.Run("recorded while running", func(t *testing.T) { testCPUCompanionFailure(t, false) })
}

func testCPUCompanionFailure(t *testing.T, beforeStartup bool) {
	t.Helper()
	ctx := context.Background()
	f := companionInstallFixture(t, "linux/amd64", "rocm", companionMembers("rocm")...)
	amd := f.installer.Hardware
	f.installer.Hardware = nil
	if _, err := f.installer.Ensure(ctx); err != nil {
		t.Fatal(err)
	}
	f.installer.Hardware = amd
	transport := f.installer.client.Transport
	f.installer.client = &http.Client{Transport: runtimeTransport(func(req *http.Request) (*http.Response, error) {
		if strings.HasSuffix(req.URL.Path, runtimeCompanions["linux/amd64"]["rocm"].Asset) {
			bad := append([]byte(nil), f.companion...)
			bad[len(bad)-1] ^= 0xff
			return runtimeResponse(http.StatusOK, bad), nil
		}
		return transport.RoundTrip(req)
	})}
	var want Host
	recordFailure := func() {
		if _, err := f.installer.Ensure(ctx); err != nil {
			t.Fatal(err)
		}
		var err error
		want, err = f.installer.Find()
		if err != nil || want.CompanionFailure == "" {
			t.Fatalf("missing fixture failure: %+v, %v", want, err)
		}
	}
	if beforeStartup {
		recordFailure()
	}
	f.installer.CPUOnly = true
	running := newStarterFixture(0, 43111)
	running.starter.Discover = func(context.Context) Host {
		host, err := f.installer.Find()
		if err != nil {
			t.Fatal(err)
		}
		return host
	}
	if _, err := running.starter.EnsureInstalled(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = running.starter.Close() }()
	f.installer.CPUOnly = false
	if !beforeStartup {
		recordFailure()
	}
	got := running.starter.Host(ctx)
	if got.CompanionFailure != want.CompanionFailure {
		t.Errorf("switching to auto lost the recorded failure: got %q, want %q", got.CompanionFailure, want.CompanionFailure)
	}
	before := len(*f.downloads)
	if _, err := f.installer.Ensure(ctx); err != nil {
		t.Fatal(err)
	}
	if len(*f.downloads) != before {
		t.Fatal("the failed release was downloaded again")
	}
	if err := f.installer.ForgetFailures(ctx); err != nil {
		t.Fatal(err)
	}
	running.starter.ClearCompanionFailure()
	if got := running.starter.Host(ctx); got.CompanionFailure != "" || got.MissingCompanion == "" {
		t.Fatalf("explicit setup did not clear failure while retaining the missing bundle: %+v", got)
	}
}

func TestCPUStartupKeepsOldTreeFactsAfterAnotherSetup(t *testing.T) {
	ctx := context.Background()
	f := companionInstallFixture(t, "linux/amd64", "rocm", companionMembers("rocm")...)
	f.installer.CPUOnly = true
	old, err := f.installer.Ensure(ctx)
	if err != nil {
		t.Fatal(err)
	}
	running := newStarterFixture(0, 43111)
	running.starter.CPUChosen = func() bool { return f.installer.CPUOnly }
	running.starter.Discover = func(context.Context) Host {
		host, err := f.installer.Find()
		if err != nil {
			t.Fatal(err)
		}
		return host
	}
	if _, err := running.starter.EnsureInstalled(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = running.starter.Close() }()
	f.installer.CPUOnly = false
	upgraded, err := f.installer.Ensure(ctx)
	if err != nil || upgraded.Binary == old.Binary {
		t.Fatalf("fixture did not publish a new tree: %+v, %v", upgraded, err)
	}
	for _, cpu := range []bool{false, true, false} {
		f.installer.CPUOnly = cpu
		got := running.starter.Host(ctx)
		if got.Binary != old.Binary || got.UnusableVendor() != "amd" {
			t.Fatalf("running tree lost its facts: %+v", got)
		}
		if (got.MissingCompanion == "") != cpu {
			t.Errorf("cpu=%v: missing bundle reporting no longer follows the choice: %+v", cpu, got)
		}
	}
}
