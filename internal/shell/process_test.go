package shell

import (
	"context"
	"testing"
)

func TestStartManagedProcessUsesExplicitEnvironmentAndCloses(t *testing.T) {
	process, err := StartManagedProcess(context.Background(), "echo", []string{"kolk-local-runtime"}, []string{"KOLK_TEST_VAR=managed"})
	if err != nil {
		t.Fatal(err)
	}
	if err := process.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := process.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}
