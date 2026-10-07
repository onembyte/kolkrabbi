package cli

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/local"
)

func TestLocalPullRechecksFitAfterNativeSetup(t *testing.T) {
	for _, exhausted := range []string{"disk", "memory"} {
		t.Run(exhausted, func(t *testing.T) {
			var pulls, installs atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/pull" {
					pulls.Add(1)
					_, _ = w.Write([]byte("{\"status\":\"success\"}\n"))
					return
				}
				http.NotFound(w, r)
			}))
			defer server.Close()
			a, _, _ := newTestApp(t, "")
			a.probeHardware = func(context.Context, string) local.Hardware {
				h := fakeHardware()
				if exhausted == "memory" {
					// This machine can fit the model only on its GPU. Another
					// workload occupies that VRAM while native setup runs.
					h.SystemRAM.Bytes = 4 << 30
				}
				if installs.Load() > 0 {
					if exhausted == "disk" {
						h.DiskFree = local.Capacity{Known: true}
					} else {
						h.Accelerators[0].AvailableVRAM = local.Capacity{Known: true}
					}
				}
				return h
			}
			a.installLocalRuntime = func(context.Context, string, func(local.RuntimeProgress)) (local.Host, error) {
				installs.Add(1)
				return local.Host{State: local.HostInstalled, Managed: true, Binary: "/fixture/ollama"}, nil
			}
			ag, err := a.newAgent(context.Background(), &options{model: "ollama/qwen2.5-coder:7b"})
			if err != nil {
				t.Fatal(err)
			}
			defer ag.Close()
			_, portText, _ := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
			port, _ := strconv.Atoi(portText)
			process := &localLifetimeProcess{}
			a.localRuntime.Port = func() (int, error) { return port, nil }
			a.localRuntime.Ready = func(context.Context, string) bool { return true }
			a.localRuntime.Start = func(context.Context, string, []string, []string) (local.Process, error) {
				return process, nil
			}
			err = a.pullLocalModel(context.Background(), "qwen2.5-coder:7b", true)
			if err == nil || !strings.Contains(err.Error(), "fit changed") {
				t.Errorf("resource exhaustion after setup should explain why the pull stopped: %v", err)
			}
			if pulls.Load() != 0 {
				t.Errorf("started %d model pulls after %s ran out during setup", pulls.Load(), exhausted)
			}
			if installs.Load() != 1 || process.closed.Load() {
				t.Fatalf("setup lost session ownership: installs=%d, closed=%v", installs.Load(), process.closed.Load())
			}
			if err := ag.Close(); err != nil || !process.closed.Load() {
				t.Fatalf("session exit failed to clean up after refused pull: closed=%v, err=%v", process.closed.Load(), err)
			}
		})
	}
}
