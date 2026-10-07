package local

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
)

// File Sync alone does not commit entries inside bin/ or lib/. Flush each
// directory bottom-up, after deferred links and before the completion record.
func syncRuntimeDirectories(ctx context.Context, root *os.Root) error {
	var dirs []string
	if err := fs.WalkDir(root.FS(), ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() {
			dirs = append(dirs, name)
		}
		return nil
	}); err != nil {
		return err
	}
	for n := len(dirs) - 1; n >= 0; n-- {
		if err := ctx.Err(); err != nil {
			return err
		}
		dir, err := root.Open(dirs[n])
		if err != nil {
			return err
		}
		if err := errors.Join(dir.Sync(), dir.Close()); err != nil {
			return fmt.Errorf("flushing runtime directory %q: %w", dirs[n], err)
		}
	}
	return nil
}
