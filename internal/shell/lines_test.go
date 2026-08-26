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
