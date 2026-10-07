//go:build darwin || linux

package local

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"testing"
)

// A write failure while copying the download is the disk's, not the
// network's: marked as a fetch failure, setup would tell someone whose disk
// is full to check the network. RLIMIT_FSIZE is process-wide, so the failing
// install runs in a helper process and no other test sees the limit.
func TestDownloadWriteFailureIsNotAFetch(t *testing.T) {
	if os.Getenv("KOLK_FSIZE_HELPER") == "1" {
		downloadPastTheFileSizeLimit(t)
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestDownloadWriteFailureIsNotAFetch$")
	cmd.Env = append(os.Environ(), "KOLK_FSIZE_HELPER=1")
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "write failure checked") {
		t.Fatalf("helper = %v:\n%s", err, out)
	}
}

func downloadPastTheFileSizeLimit(t *testing.T) {
	i, _, data := runtimeInstallFixture(t, "linux/amd64")
	const limit = 1024
	if len(data) <= 2*limit {
		t.Fatalf("fixture bundle is %d bytes; the limit must cut it mid-copy", len(data))
	}
	// Past the limit the kernel raises SIGXFSZ; ignored, the write fails.
	signal.Ignore(syscall.SIGXFSZ)
	var current syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_FSIZE, &current); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &syscall.Rlimit{Cur: limit, Max: current.Max}); err != nil {
		t.Fatal(err)
	}
	// The helper's testing runtime writes its coverage report after this test
	// returns. Restore the limit first or instrumentation itself fails EFBIG.
	t.Cleanup(func() {
		if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &current); err != nil {
			t.Errorf("restore file-size limit: %v", err)
		}
	})
	starter := &HostStarter{Provision: func(ctx context.Context, _ io.Writer) (Host, error) { return i.Ensure(ctx) }}
	_, err := starter.Ensure(context.Background())
	var setup *RuntimeSetupError
	if !errors.As(err, &setup) || !errors.Is(setup.err, syscall.EFBIG) {
		t.Fatalf("setup = %T %v; want a setup failure from the write cut mid-copy", err, err)
	}
	var fetch *runtimeFetchError
	if errors.As(setup.err, &fetch) || strings.Contains(err.Error(), "check the network") {
		t.Fatalf("a local write failure reads as a network failure: %v", err)
	}
	fmt.Println("write failure checked")
}
