package local

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/onembyte/kolkrabbi/internal/atomicfile"
	"github.com/onembyte/kolkrabbi/internal/lock"
	"github.com/onembyte/kolkrabbi/internal/shell"
)

type runtimeRecord struct {
	Project  string `json:"project"`
	Addr     string `json:"addr"`
	PID      int    `json:"pid"`
	Identity string `json:"identity"`
}

func (h *HostStarter) recordPath() string {
	return filepath.Join(h.StateDir, fmt.Sprintf("%x.json", sha256.Sum256([]byte(h.Project))))
}

func (h *HostStarter) lockRegistry(ctx context.Context) (*lock.File, error) {
	if !filepath.IsAbs(h.StateDir) || !filepath.IsAbs(h.Project) {
		return nil, fmt.Errorf("persistent Localia needs an absolute storage directory and project root")
	}
	if err := os.MkdirAll(h.StateDir, 0o700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(h.StateDir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("localia runtime storage is not a directory")
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	held, err := lock.Acquire(ctx, h.recordPath()+".lock")
	if err != nil {
		return nil, fmt.Errorf("persistent Localia needs runtime locking: %w; use /config set local.ephemeral on if locking is unavailable", err)
	}
	return held, nil
}

func (h *HostStarter) identity(ctx context.Context, pid int) (string, error) {
	identify := h.Identity
	if identify == nil {
		identify = shell.ProcessIdentity
	}
	return identify(ctx, pid)
}

// reusable checks project, loopback address, process lifetime and the Ollama
// heartbeat. A stale PID is never signalled; it only makes a record ineligible.
func (h *HostStarter) reusable(ctx context.Context) (string, error) {
	if h.StateDir == "" || h.Project == "" {
		return "", nil
	}
	info, err := os.Lstat(h.recordPath())
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() > 16384 {
		return "", fmt.Errorf("invalid Localia runtime record %s", h.recordPath())
	}
	f, err := os.Open(h.recordPath())
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	var record runtimeRecord
	if err := json.NewDecoder(io.LimitReader(f, 16385)).Decode(&record); err != nil {
		return "", fmt.Errorf("reading Localia runtime record: %w", err)
	}
	host, port, err := net.SplitHostPort(record.Addr)
	number, portErr := strconv.Atoi(port)
	if err != nil || portErr != nil || host != "127.0.0.1" || number < 1 || number > 65535 || record.Addr == DefaultHostAddr ||
		record.Project != h.Project || record.PID <= 0 || record.Identity == "" {
		return "", nil
	}
	identity, err := h.identity(ctx, record.PID)
	if err != nil || identity != record.Identity {
		return "", nil
	}
	ready := h.Ready
	if ready == nil {
		ready = func(ctx context.Context, addr string) bool { _, ok := probeHost(ctx, addr); return ok }
	}
	if !ready(ctx, record.Addr) {
		return "", &runtimeStalled{pid: record.PID, addr: record.Addr}
	}
	return record.Addr, nil
}

// RecordedRuntime looks up this project's kept runtime independently of
// default-server discovery. A stop hint is shown only for a process that
// passes the same identity, Ollama heartbeat and listener-owner checks as stop.
func (h *HostStarter) RecordedRuntime(ctx context.Context) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	record, err := h.stopRecord()
	if err != nil || h.verifyStopTarget(ctx, record) != nil {
		return ""
	}
	return record.Addr
}

// runtimeStalled is a recorded runtime whose process is still the one Kolk
// started but does not answer: hung, stopped, or still starting.
type runtimeStalled struct {
	pid  int
	addr string
}

func (e *runtimeStalled) Error() string {
	return fmt.Sprintf("managed Ollama process %d is still running but is not answering at %s; retry when it is ready", e.pid, e.addr)
}

// describe is the state alone, for a surface that gives its own advice.
func (e *runtimeStalled) describe() string {
	return fmt.Sprintf("Kolk's runtime for this project (process %d) is running but not answering at %s", e.pid, e.addr)
}

func (h *HostStarter) publish(ctx context.Context, addr string, process Process) error {
	pid := pidOf(process)
	if pid <= 0 {
		return fmt.Errorf("runtime did not report its process id")
	}
	identity, err := h.identity(ctx, pid)
	if err != nil {
		return err
	}
	if identity == "" {
		return fmt.Errorf("runtime process identity is empty")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	b, err := json.Marshal(runtimeRecord{Project: h.Project, Addr: addr, PID: pid, Identity: identity})
	if err != nil {
		return err
	}
	return atomicfile.Write(h.recordPath(), b, 0o600)
}
