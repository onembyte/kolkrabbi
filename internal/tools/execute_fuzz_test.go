package tools

import (
	"context"
	"strings"
	"testing"
)

// FuzzExecuteArguments drives tool dispatch with arbitrary names and arbitrary
// argument bytes — the second place where a third party's bytes become control
// flow (docs/plan/21-quality-testing-security.md).
//
// The invariants are about what must NOT happen, because the interesting
// failures here are silent ones:
//
//   - a name the catalogue does not have never runs anything;
//   - malformed arguments are refused, never decoded into zero values and acted
//     on — an edit whose path failed to parse would otherwise write to "";
//   - nothing runs without passing the guard, so a fuzzer that finds a path
//     around the permission check fails the test rather than deleting a file.
//
// The guard here refuses everything, so any tool that reports success has run
// without permission.
func FuzzExecuteArguments(f *testing.F) {
	for _, name := range []string{"bash", "read_file", "write_file", "edit_file", "list_files", "nonexistent", ""} {
		f.Add(name, `{"path":"a.go"}`)
		f.Add(name, `{"command":"ls"}`)
		f.Add(name, "")
		f.Add(name, "{")
		f.Add(name, "null")
		f.Add(name, `{"path":123}`)
	}

	root := f.TempDir()
	f.Fuzz(func(t *testing.T, name, arguments string) {
		var asked int
		out, err := Execute(context.Background(), name, arguments, Options{
			Root: root,
			Guard: func(Request) bool {
				asked++
				return false
			},
		})

		if err == nil && asked == 0 && out != "" {
			t.Fatalf("tool %q produced output without an error and without asking the guard: %q",
				name, out)
		}
		if !knownTool(name) && (err == nil || !strings.Contains(err.Error(), name)) && out != "" {
			t.Fatalf("unknown tool %q produced %q instead of an error naming it", name, out)
		}
	})
}

func knownTool(name string) bool {
	for _, d := range Definitions() {
		if d.Function.Name == name {
			return true
		}
	}
	return false
}
