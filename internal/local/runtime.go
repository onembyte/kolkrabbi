// Package local contains Kolk-managed local-model runtime contracts.
package local

import (
	"fmt"
	"net"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/onembyte/kolkrabbi/internal/paths"
)

// RuntimeSpec describes a sidecar before it is started.
type RuntimeSpec struct {
	Executable string
	ModelDir   string
	Host       string
	Env        []string
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
	if !filepath.IsAbs(s.ModelDir) || !strings.HasSuffix(filepath.Clean(s.ModelDir), string(filepath.Separator)+"local-models") {
		return fmt.Errorf("local model directory is not Kolk-managed, got %q", s.ModelDir)
	}
	return nil
}
