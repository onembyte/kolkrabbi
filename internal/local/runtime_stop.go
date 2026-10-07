package local

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"

	"github.com/onembyte/kolkrabbi/internal/shell"
)

var ErrNoKeptRuntime = errors.New("no kept Localia runtime for this project")

// StopKept is the only path that stops a persisted runtime. It serializes
// with publication, refuses a stale PID or another listener, and never uses
// the session's ordinary Close policy as authority to stop a kept process.
func (h *HostStarter) StopKept(ctx context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	held, err := h.lockRegistry(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = held.Close() }()
	record, err := h.stopRecord()
	if err != nil {
		return err
	}
	if err := h.verifyStopTarget(ctx, record); err != nil {
		return err
	}
	// Recheck immediately before the signal: a PID whose owner changed during
	// the listener probe must never be signalled from a saved record.
	if identity, err := h.identity(ctx, record.PID); err != nil || identity != record.Identity {
		return fmt.Errorf("refusing to stop Localia: recorded process identity changed")
	}
	signal := h.Signal
	if signal == nil {
		signal = shell.SignalManagedProcessGroup
	}
	if err := signal(ctx, record.PID); err != nil {
		return fmt.Errorf("stopping kept Localia runtime: %w", err)
	}
	if err := os.Remove(h.recordPath()); err != nil {
		return fmt.Errorf("removing stopped Localia runtime record: %w", err)
	}
	if h.addr == record.Addr {
		h.process, h.addr, h.managed, h.kept = nil, "", false, false
	}
	return nil
}

func (h *HostStarter) stopRecord() (runtimeRecord, error) {
	path := h.recordPath()
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return runtimeRecord{}, ErrNoKeptRuntime
	}
	if err != nil {
		return runtimeRecord{}, err
	}
	if !info.Mode().IsRegular() || info.Size() > 16384 {
		return runtimeRecord{}, fmt.Errorf("refusing invalid Localia runtime record %s", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return runtimeRecord{}, err
	}
	defer func() { _ = f.Close() }()
	opened, err := f.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return runtimeRecord{}, fmt.Errorf("refusing Localia runtime record changed while opening")
	}
	var record runtimeRecord
	decoder := json.NewDecoder(io.LimitReader(f, 16385))
	if err := decoder.Decode(&record); err != nil {
		return runtimeRecord{}, fmt.Errorf("reading Localia runtime record: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return runtimeRecord{}, fmt.Errorf("refusing Localia runtime record with trailing data")
	}
	host, port, err := net.SplitHostPort(record.Addr)
	number, portErr := strconv.Atoi(port)
	if err != nil || portErr != nil || host != "127.0.0.1" || number < 1 || number > 65535 || record.Addr == DefaultHostAddr ||
		record.Project != h.Project || record.PID <= 0 || record.Identity == "" {
		return runtimeRecord{}, fmt.Errorf("refusing Localia runtime record with unverified project or address")
	}
	return record, nil
}

func (h *HostStarter) verifyStopTarget(ctx context.Context, record runtimeRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	identity, err := h.identity(ctx, record.PID)
	if err != nil || identity != record.Identity {
		return fmt.Errorf("refusing to stop Localia: recorded process is not running with its original identity")
	}
	ready := h.Ready
	if ready == nil {
		ready = func(ctx context.Context, addr string) bool { _, ok := probeHost(ctx, addr); return ok }
	}
	if !ready(ctx, record.Addr) {
		return fmt.Errorf("refusing to stop Localia: recorded address %s is not answering as Ollama", record.Addr)
	}
	owner := h.EndpointOwner
	if owner == nil {
		owner = shell.ProcessOwnsLoopbackListener
	}
	ok, err := owner(ctx, record.PID, record.Addr)
	if err != nil {
		return fmt.Errorf("refusing to stop Localia: cannot confirm process %d owns %s: %w", record.PID, record.Addr, err)
	}
	if !ok {
		return fmt.Errorf("refusing to stop Localia: process %d does not own %s", record.PID, record.Addr)
	}
	return nil
}
