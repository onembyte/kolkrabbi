//go:build !windows

package shell

import (
	"context"
	"strings"
	"testing"
)

func TestRunLinesDeliversCompleteLines(t *testing.T) {
	var got []string
	err := RunLines(context.Background(), "printf", []string{"alpha\\nbeta\\n"}, nil, func(line []byte) error {
		got = append(got, string(line))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != "alpha,beta" {
		t.Fatalf("lines = %q", got)
	}
}

func TestRunLinesAcceptsAProviderLineLargerThanOneMiB(t *testing.T) {
	const size = 12 * 1024 * 1024
	var got int
	err := RunLines(context.Background(), "sh", []string{"-c", `head -c 12582912 /dev/zero | tr '\000' x; printf '\n'`}, nil, func(line []byte) error {
		got = len(line)
		return nil
	})
	if err != nil {
		t.Fatalf("RunLines rejected a %d-byte provider line: %v", size, err)
	}
	if got != size {
		t.Fatalf("line length = %d, want %d", got, size)
	}
}

func TestRunLinesRejectsAnUnboundedProviderLine(t *testing.T) {
	err := RunLines(context.Background(), "sh", []string{"-c", `head -c 16777217 /dev/zero | tr '\000' x; printf '\n'`}, nil, func([]byte) error {
		t.Fatal("an oversized provider line was delivered")
		return nil
	})
	if err == nil {
		t.Fatal("RunLines accepted a provider line above its bound")
	}
	if !strings.Contains(err.Error(), "provider output line exceeds 16 MiB") {
		t.Fatalf("error = %v, want the bounded provider-line diagnosis", err)
	}
	if strings.Contains(err.Error(), "token too long") {
		t.Fatalf("error = %v, leaked Scanner's opaque failure", err)
	}
}
