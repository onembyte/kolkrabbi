package shell

import (
	"context"
	"strings"
	"testing"
	"time"
)

// A command that prints a great deal must not cost kolk that much memory. The
// tool layer cuts output to 12k characters for the model, but it can only cut
// what has already been read into memory; the bound has to be here, in the
// capture, before the bytes are kept. The first bytes are the ones kept -- a
// build log's beginning names the failing step -- and the count of what was
// dropped travels with the result so nothing downstream has to guess.
func TestChildOutputIsBoundedBeforeItIsKept(t *testing.T) {
	if _, err := New().Run(context.Background(), Cmd{Command: "command -v head >/dev/null && command -v tr >/dev/null", Timeout: 5 * time.Second}); err != nil {
		t.Skip("head/tr unavailable")
	}
	const total = 8 << 20 // 8 MiB, well past any sane bound
	res, err := New().Run(context.Background(), Cmd{
		Command: "printf 'FIRST-LINE\\n'; head -c 8388608 /dev/zero | tr '\\0' x",
		Timeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK() {
		t.Fatalf("command failed: %+v", res)
	}
	if len(res.Output) > maxCapture {
		t.Fatalf("captured %d bytes; the bound is %d", len(res.Output), maxCapture)
	}
	if !strings.HasPrefix(res.Output, "FIRST-LINE\n") {
		t.Fatalf("the beginning of the output was not what was kept: %q", res.Output[:min(40, len(res.Output))])
	}
	wantDropped := int64(len("FIRST-LINE\n")+total) - int64(len(res.Output))
	if res.Dropped != wantDropped {
		t.Fatalf("Dropped = %d, want %d", res.Dropped, wantDropped)
	}
}

// Ordinary output is untouched: no note, no count, byte for byte.
func TestSmallOutputIsNeitherBoundedNorCounted(t *testing.T) {
	res, err := New().Run(context.Background(), Cmd{Command: "printf 'hello'", Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if res.Output != "hello" || res.Dropped != 0 {
		t.Fatalf("res = %+v", res)
	}
}
