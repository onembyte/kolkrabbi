package local

import (
	"context"
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

type fakeProcess struct {
	closed int
}

func (p *fakeProcess) Close() error {
	p.closed++
	return nil
}

func TestRuntimeStartsOneManagedProcessAndClosesItOnce(t *testing.T) {
	spec := NewRuntimeSpec(paths.Dirs{Data: "/data"}, "0.1.0")
	process := &fakeProcess{}
	starts := 0
	var gotExecutable string
	var gotArgs []string
	var gotEnv []string
	runtime := NewRuntime(spec, func(_ context.Context, executable string, args, env []string) (Process, error) {
		starts++
		gotExecutable, gotArgs, gotEnv = executable, args, env
		return process, nil
	})

	if err := runtime.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if starts != 1 {
		t.Fatalf("sidecar starts = %d, want one", starts)
	}
	if gotExecutable != spec.Executable || len(gotArgs) != 1 || gotArgs[0] != "serve" {
		t.Fatalf("launch = %q %#v, want managed executable and serve", gotExecutable, gotArgs)
	}
	if !containsEnv(gotEnv, "OLLAMA_MODELS="+spec.ModelDir) {
		t.Fatalf("launch environment = %#v, missing managed model path", gotEnv)
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
	if process.closed != 1 {
		t.Fatalf("process closes = %d, want one", process.closed)
	}
}

func TestRuntimeCannotStartAfterClose(t *testing.T) {
	spec := NewRuntimeSpec(paths.Dirs{Data: "/data"}, "0.1.0")
	starts := 0
	runtime := NewRuntime(spec, func(context.Context, string, []string, []string) (Process, error) {
		starts++
		return &fakeProcess{}, nil
	})
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Start(context.Background()); err == nil {
		t.Fatal("starting a closed runtime must fail")
	}
	if starts != 0 {
		t.Fatalf("sidecar starts = %d after close, want zero", starts)
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
