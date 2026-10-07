package local

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"testing/iotest"
	"time"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

// blockedSetup is a Provision that holds until released, the shape of a
// first-run runtime download seen from everything waiting behind it.
type blockedSetup struct {
	entered chan struct{}
	release chan error
	calls   atomic.Int32
}

func newBlockedSetup() *blockedSetup {
	return &blockedSetup{entered: make(chan struct{}, 4), release: make(chan error, 4)}
}

func (s *blockedSetup) provision(ctx context.Context, _ io.Writer) (Host, error) {
	s.calls.Add(1)
	s.entered <- struct{}{}
	select {
	case <-ctx.Done():
		return Host{}, ctx.Err()
	case err := <-s.release:
		if err != nil {
			return Host{}, err
		}
		return Host{State: HostInstalled, Binary: "/managed/ollama", Managed: true}, nil
	}
}

func (s *blockedSetup) waitEntered(t *testing.T) {
	t.Helper()
	select {
	case <-s.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("runtime setup never began")
	}
}

// The terminal footer reads the context window on every spinner tick. A first
// run downloads the runtime inside the first turn, so the window must answer
// from what is known instead of waiting minutes behind that download.
func TestContextWindowDoesNotWaitForRuntimeSetup(t *testing.T) {
	f := newStarterFixture(0, 43111)
	f.starter.Binary = ""
	setup := newBlockedSetup()
	f.starter.Provision = setup.provision
	backend := NewLazyHostBackend(f.starter)

	turn := make(chan error, 1)
	go func() {
		_, _, err := backend.StreamChat(context.Background(), "qwen2.5-coder:7b", nil, nil, nil)
		turn <- err
	}()
	setup.waitEntered(t)

	window := make(chan int, 1)
	go func() { window <- backend.ContextWindow("qwen2.5-coder:7b") }()
	blocked := false
	select {
	case got := <-window:
		if got != hostFloorContext {
			t.Errorf("window before any load = %d, want the floor %d", got, hostFloorContext)
		}
	case <-time.After(time.Second):
		blocked = true
		t.Error("ContextWindow waited behind runtime setup; the footer would freeze for the whole download")
	}

	setup.release <- errors.New("fixture: setup refused")
	if err := <-turn; err == nil {
		t.Fatal("a refused setup answered the turn")
	}
	if blocked {
		<-window
	}
	if f.starts.Load() != 0 {
		t.Fatalf("a refused setup started %d processes", f.starts.Load())
	}
}

// Cancelling one caller's setup (Esc on the first turn) belongs to that
// caller. A second caller waiting on the same session runtime sets it up
// itself, and exactly one server runs and is stopped at session exit.
func TestCancelledSetupDoesNotCancelTheNextCaller(t *testing.T) {
	f := newStarterFixture(0, 43111)
	f.starter.Binary = ""
	setup := newBlockedSetup()
	f.starter.Provision = setup.provision

	first, cancelFirst := context.WithCancel(context.Background())
	firstDone := make(chan error, 1)
	go func() {
		_, err := f.starter.Ensure(first)
		firstDone <- err
	}()
	setup.waitEntered(t)

	secondDone := make(chan string, 1)
	secondErr := make(chan error, 1)
	go func() {
		addr, err := f.starter.Ensure(context.Background())
		secondDone <- addr
		secondErr <- err
	}()

	cancelFirst()
	if err := <-firstDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled setup returned %v, want context.Canceled", err)
	}
	if f.starts.Load() != 0 {
		t.Fatalf("a cancelled setup started %d processes", f.starts.Load())
	}

	setup.waitEntered(t)
	setup.release <- nil
	addr, err := <-secondDone, <-secondErr
	if err != nil || addr != "127.0.0.1:43111" {
		t.Fatalf("the waiting caller inherited the cancellation: addr=%q err=%v", addr, err)
	}
	if again, err := f.starter.Ensure(context.Background()); err != nil || again != addr {
		t.Fatalf("a third caller moved or failed: %q, %v", again, err)
	}
	if setup.calls.Load() != 2 || f.starts.Load() != 1 {
		t.Fatalf("setup ran %d times and started %d servers; want 2 and 1", setup.calls.Load(), f.starts.Load())
	}
	if err := f.starter.Close(); err != nil || !f.process.closed.Load() {
		t.Fatalf("session exit left the set-up runtime running: closed=%v err=%v", f.process.closed.Load(), err)
	}
	if _, err := f.starter.Ensure(context.Background()); err == nil {
		t.Fatal("a closed starter set up another runtime")
	}
	if setup.calls.Load() != 2 {
		t.Fatalf("a closed starter ran setup again: %d calls", setup.calls.Load())
	}
}

// A setup that cannot reach the official release is not the model endpoint
// hitting a limit: it must not be classified as one (which pauses the turn and
// hides the cause), and it says what failed and what to do.
func TestRuntimeSetupFailureIsNotAModelEndpointLimit(t *testing.T) {
	f := newStarterFixture(0, 43111)
	f.starter.Binary = ""
	dns := &url.Error{Op: "Get", URL: "https://api.github.com/repos/ollama/ollama/releases/latest",
		Err: &net.DNSError{Err: "no such host", Name: "api.github.com", IsNotFound: true}}
	f.starter.Provision = func(context.Context, io.Writer) (Host, error) {
		return Host{}, fmt.Errorf("requesting official Ollama runtime: %w", dns)
	}
	_, err := f.starter.Ensure(context.Background())
	if err == nil {
		t.Fatal("a failed setup started a runtime")
	}
	if limit, ok := provider.Classify(err); ok {
		t.Fatalf("setup failure classified as a %s limit on the model endpoint: %v", limit.Kind, err)
	}
	for _, want := range []string{"api.github.com", "Localia"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}

	cancelled, cancel := context.WithCancel(context.Background())
	f2 := newStarterFixture(0, 43112)
	f2.starter.Binary = ""
	f2.starter.Provision = func(ctx context.Context, _ io.Writer) (Host, error) {
		cancel()
		return Host{}, &url.Error{Op: "Get", URL: "https://api.github.com/x", Err: ctx.Err()}
	}
	if _, err := f2.starter.Ensure(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled setup = %v; want context.Canceled", err)
	}
}

// The installer's own deadline (its release lookup, behind a firewall that
// drops traffic) is a setup failure, not the caller's cancellation: only the
// caller's own context ending passes through unwrapped.
func TestSetupDeadlineIsASetupFailureNotAnEndpointLimit(t *testing.T) {
	f := newStarterFixture(0, 43113)
	f.starter.Binary = ""
	f.starter.Provision = func(context.Context, io.Writer) (Host, error) {
		return Host{}, fmt.Errorf("requesting official Ollama runtime: %w", context.DeadlineExceeded)
	}
	_, err := f.starter.Ensure(context.Background())
	if limit, ok := provider.Classify(err); ok {
		t.Fatalf("the installer's own deadline was classified as a %s limit: %v", limit.Kind, err)
	}
	if err == nil || !strings.Contains(err.Error(), "Localia") {
		t.Fatalf("setup deadline = %v; want a Localia setup failure", err)
	}
}

// A setup failure advises only what can fix it. "Check the network" is for a
// failure to reach or read the release; a full disk needs room; a platform
// Kolk cannot set up already names its remedy, and no retry changes it. A
// bundle that fails verification gets no network advice either.
func TestRuntimeSetupErrorAdvisesOnlyWhatCanFixIt(t *testing.T) {
	const network, room, own = "check the network and retry", "free some disk space and retry", "installing Ollama yourself also works"
	serve := func(i *RuntimeInstaller, asset func(data []byte) *http.Response) {
		base := i.client.Transport
		i.client = &http.Client{Transport: runtimeTransport(func(req *http.Request) (*http.Response, error) {
			resp, err := base.RoundTrip(req)
			if err != nil || req.URL.String() == runtimeReleaseURL {
				return resp, err
			}
			data, err := io.ReadAll(resp.Body)
			if err != nil {
				return nil, err
			}
			return asset(data), nil
		})}
	}
	for _, c := range []struct {
		name, platform string
		breakIt        func(*RuntimeInstaller)
		want, not      []string
	}{
		{"offline", "linux/amd64", func(i *RuntimeInstaller) {
			i.client = &http.Client{Transport: runtimeTransport(func(*http.Request) (*http.Response, error) {
				return nil, &net.OpError{Op: "dial", Net: "tcp", Err: &net.DNSError{Err: "no such host", Name: "api.github.com", IsNotFound: true}}
			})}
		}, []string{network}, []string{room, own}},
		{"dropped reading the release", "linux/amd64", func(i *RuntimeInstaller) {
			i.client = &http.Client{Transport: runtimeTransport(func(*http.Request) (*http.Response, error) {
				reset := &net.OpError{Op: "read", Net: "tcp", Err: os.NewSyscallError("read", syscall.ECONNRESET)}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(iotest.ErrReader(reset)), ContentLength: -1}, nil
			})}
		}, []string{network}, []string{room, own}},
		{"dropped mid-download", "linux/amd64", func(i *RuntimeInstaller) {
			serve(i, func(data []byte) *http.Response {
				reset := &net.OpError{Op: "read", Net: "tcp", Err: os.NewSyscallError("read", syscall.ECONNRESET)}
				body := io.MultiReader(bytes.NewReader(data[:len(data)/2]), iotest.ErrReader(reset))
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(body), ContentLength: int64(len(data))}
			})
		}, []string{network}, []string{room, own}},
		{"truncated download", "linux/amd64", func(i *RuntimeInstaller) {
			serve(i, func(data []byte) *http.Response {
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(data[:len(data)/2])), ContentLength: -1}
			})
		}, []string{network}, []string{room, own}},
		{"failed verification", "linux/amd64", func(i *RuntimeInstaller) {
			serve(i, func(data []byte) *http.Response {
				data[len(data)-1] ^= 0xff
				return runtimeResponse(200, data)
			})
		}, []string{own}, []string{network, room}},
		{"no room", "linux/amd64", func(i *RuntimeInstaller) {
			i.freeSpace = func(string) (uint64, bool) { return 1 << 20, true }
		}, []string{room}, []string{network}},
		{"unsupported platform", "windows/amd64", nil, []string{"install Ollama separately"}, []string{network, room, own, "retry"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			var i *RuntimeInstaller
			if _, ok := runtimePlatforms[c.platform]; ok {
				i, _, _ = runtimeInstallFixture(t, c.platform)
			} else {
				i = &RuntimeInstaller{Dir: filepath.Join(t.TempDir(), "runtimes"), platform: c.platform}
			}
			if c.breakIt != nil {
				c.breakIt(i)
			}
			f := newStarterFixture(0, 43120)
			f.starter.Binary = ""
			f.starter.Provision = func(ctx context.Context, _ io.Writer) (Host, error) { return i.Ensure(ctx) }
			_, err := f.starter.Ensure(context.Background())
			if err == nil {
				t.Fatal("a broken setup started a runtime")
			}
			if limit, ok := provider.Classify(err); ok {
				t.Fatalf("setup failure classified as a %s limit on the model endpoint: %v", limit.Kind, err)
			}
			for _, want := range c.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error lacks %q: %v", want, err)
				}
			}
			for _, not := range c.not {
				if strings.Contains(err.Error(), not) {
					t.Errorf("error advises %q, which cannot fix it: %v", not, err)
				}
			}
			if n := strings.Count(err.Error(), "install Ollama"); n > 1 {
				t.Errorf("error says to install Ollama %d times: %v", n, err)
			}
		})
	}
}
