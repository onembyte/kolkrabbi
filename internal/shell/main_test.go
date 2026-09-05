package shell

import (
	"os"
	"testing"
)

// In production the Landlock child is kolk re-executed, and cli.Main hands it
// to MaybeRunAsLandlockChild. Under `go test` the re-executed program is this
// test binary, so it has to do the same, before testing.M parses os.Args and
// mistakes `bash -c ...` for flags. This is the standard shape for testing a
// program that re-executes itself, and it also proves the entry from the
// binary's own side.
func TestMain(m *testing.M) {
	if handled, code := MaybeRunAsLandlockChild(os.Args[1:], os.Stderr); handled {
		os.Exit(code)
	}
	os.Exit(m.Run())
}
