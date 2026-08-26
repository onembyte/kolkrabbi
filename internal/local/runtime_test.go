package local

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/paths"
)

func TestRuntimeSpecUsesOnlyKolkOwnedPaths(t *testing.T) {
	dirs := paths.Dirs{Config: "/config", Data: "/data", Cache: "/cache"}
	spec := NewRuntimeSpec(dirs, "0.1.0")

	if !strings.HasPrefix(spec.Executable, filepath.Join(dirs.LocalRuntimeDir(), "0.1.0")) {
		t.Fatalf("executable = %q, not below managed runtime directory", spec.Executable)
	}
	if spec.ModelDir != dirs.LocalModelsDir() {
		t.Fatalf("model directory = %q, want %q", spec.ModelDir, dirs.LocalModelsDir())
	}
}

func TestRuntimeSpecBindsLoopbackAndUsesManagedModelEnvironment(t *testing.T) {
	spec := NewRuntimeSpec(paths.Dirs{Data: "/data"}, "0.1.0")
	if err := spec.Validate(); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(spec.Host, "127.0.0.1:") {
		t.Fatalf("host = %q, want loopback endpoint", spec.Host)
	}
	if strings.Contains(spec.Host, "0.0.0.0") {
		t.Fatalf("host = %q must not bind publicly", spec.Host)
	}
	if !containsEnv(spec.Env, "OLLAMA_MODELS="+spec.ModelDir) {
		t.Fatalf("environment does not pin model storage: %#v", spec.Env)
	}
	if !containsEnv(spec.Env, "OLLAMA_HOST=http://"+spec.Host) {
		t.Fatalf("environment does not pin private endpoint: %#v", spec.Env)
	}
}

func TestRuntimeSpecRejectsHostOrExternalModelConfiguration(t *testing.T) {
	spec := RuntimeSpec{Host: "0.0.0.0:11434", ModelDir: "/tmp/ollama"}
	if err := spec.Validate(); err == nil {
		t.Fatal("expected externally bound runtime or unmanaged model path to fail")
	}
}

func containsEnv(env []string, want string) bool {
	for _, value := range env {
		if value == want {
			return true
		}
	}
	return false
}
