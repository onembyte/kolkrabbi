//go:build darwin || linux

package lock

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestLockRefusesAliasesAndSpecialFilesBeforeMutation(t *testing.T) {
	for _, kind := range []string{"symlink", "dangling symlink", "hardlink", "fifo", "directory"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(dir, "unrelated")
			name := filepath.Join(dir, "install.lock")
			if kind != "dangling symlink" {
				if err := os.WriteFile(target, []byte("keep"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			var err error
			switch kind {
			case "symlink", "dangling symlink":
				err = os.Symlink(target, name)
			case "hardlink":
				err = os.Link(target, name)
			case "fifo":
				err = syscall.Mkfifo(name, 0o600)
			case "directory":
				err = os.Mkdir(name, 0o700)
			}
			if err != nil {
				t.Fatal(err)
			}
			if held, err := Try(name); err == nil {
				held.Close()
				t.Fatal("unsafe lock accepted")
			}
			if kind == "dangling symlink" {
				if _, err := os.Stat(target); !os.IsNotExist(err) {
					t.Fatal("created symlink target")
				}
				return
			}
			if data, err := os.ReadFile(target); err != nil || string(data) != "keep" {
				t.Fatalf("target changed: %q, %v", data, err)
			}
			if info, err := os.Stat(target); err != nil || info.Mode().Perm() != 0o644 {
				t.Fatalf("target mode changed: %v, %v", info, err)
			}
		})
	}
}
