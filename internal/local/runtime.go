// Package local contains Kolk-managed local-model runtime contracts.
package local

import (
	"context"
	"fmt"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/onembyte/kolkrabbi/internal/paths"
	"github.com/onembyte/kolkrabbi/internal/shell"
)

// RuntimeSpec describes a sidecar before it is started.
type RuntimeSpec struct {
	Executable string
	ModelDir   string
	Host       string
	Env        []string
}

// Process is the lifecycle surface needed by the managed sidecar.
type Process interface {
	Close() error
}

// StartFunc starts one already-validated managed sidecar.
type StartFunc func(context.Context, string, []string, []string) (Process, error)

// Runtime owns one sidecar for its caller's lifetime.
type Runtime struct {
	spec    RuntimeSpec
	start   StartFunc
	mu      sync.Mutex
	process Process
	closed  bool
}

// NewRuntime creates a lifecycle owner. The starter is injected so tests never
// need to execute or contact an Ollama binary.
func NewRuntime(spec RuntimeSpec, start StartFunc) *Runtime {
	return &Runtime{spec: spec, start: start}
}

// NewManagedRuntime wires the lifecycle owner to Kolk's shell process
// primitive. It does not start the sidecar until Start is called.
func NewManagedRuntime(spec RuntimeSpec) *Runtime {
	return NewRuntime(spec, func(ctx context.Context, executable string, args, env []string) (Process, error) {
		return shell.StartManagedProcess(ctx, executable, args, env)
	})
}

// Start launches the sidecar at most once.
func (r *Runtime) Start(ctx context.Context) error {
	if r == nil {
		return fmt.Errorf("local runtime is nil")
	}
	if err := r.spec.Validate(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return fmt.Errorf("local runtime is closed")
	}
	if r.process != nil {
		return nil
	}
	if r.start == nil {
		return fmt.Errorf("local runtime starter is not configured")
	}
	process, err := r.start(ctx, r.spec.Executable, []string{"serve"}, append([]string(nil), r.spec.Env...))
	if err != nil {
		return fmt.Errorf("starting managed local runtime: %w", err)
	}
	r.process = process
	return nil
}

// Close stops the sidecar once and prevents later restarts.
func (r *Runtime) Close() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	r.closed = true
	if r.process == nil {
		return nil
	}
	return r.process.Close()
}

// NewRuntimeSpec creates a sidecar specification entirely inside Kolk state.
func NewRuntimeSpec(dirs paths.Dirs, version string) RuntimeSpec {
	runtimeDir := filepath.Join(dirs.LocalRuntimeDir(), version)
	modelDir := dirs.LocalModelsDir()
	host := "127.0.0.1:0"
	return RuntimeSpec{
		Executable: filepath.Join(runtimeDir, "ollama"),
		ModelDir:   modelDir,
		Host:       host,
		Env: []string{
			"OLLAMA_MODELS=" + modelDir,
			"OLLAMA_HOST=http://" + host,
		},
	}
}

// Validate rejects public endpoints and model locations outside Kolk's
// managed local-model directory.
func (s RuntimeSpec) Validate() error {
	host, port, err := net.SplitHostPort(s.Host)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		return fmt.Errorf("local runtime host must be loopback, got %q", s.Host)
	}
	if n, err := strconv.Atoi(port); err != nil || n < 0 || n > 65535 {
		return fmt.Errorf("local runtime port is invalid, got %q", port)
	}
	if !filepath.IsAbs(s.Executable) {
		return fmt.Errorf("local runtime executable must be absolute, got %q", s.Executable)
	}
	// An empty ModelDir means the host's own store (option E); a set one has
	// to be kolk's, because a managed store anywhere else is a store nobody
	// asked kolk to write into.
	if s.ModelDir != "" && (!filepath.IsAbs(s.ModelDir) || !strings.HasSuffix(filepath.Clean(s.ModelDir), string(filepath.Separator)+"local-models")) {
		return fmt.Errorf("local model directory is not Kolk-managed, got %q", s.ModelDir)
	}
	return nil
}
