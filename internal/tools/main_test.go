package tools

import (
	"os"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/shell"
)

// On linux a sandboxed command re-executes the running program as the confined
// child, and under `go test` the running program is this binary. Without this
// intercept it would run the whole suite again inside the child -- which is
// exactly what happened on CI on 2026-09-05, a dozen levels deep, before the
// tools test was made to refuse in the parent. The intercept stays regardless:
// any future tools test that sandboxes a command on linux would recurse without it.
func TestMain(m *testing.M) {
	if handled, code := shell.MaybeRunAsLandlockChild(os.Args[1:], os.Stderr); handled {
		os.Exit(code)
	}
	os.Exit(m.Run())
}
