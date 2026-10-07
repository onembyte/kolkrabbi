package local

import (
	"archive/tar"
	"context"
	"fmt"
	"path"
	"path/filepath"
)

// Links are created after every regular file. Only links resolving to an
// extracted regular file are accepted; directory links, dangling links, cycles
// and paths through other links are refused. Official shared-library aliases
// retain their relative spelling; hard links bind the verified regular target.
func (x *archiveExtractor) links(ctx context.Context) error {
	resolved := make(map[string]string)
	var resolve func(string, int) (string, error)
	resolve = func(name string, depth int) (string, error) {
		if target, ok := resolved[name]; ok {
			return target, nil
		}
		if depth > 64 {
			return "", fmt.Errorf("runtime archive link cycle or excessive depth at %q", name)
		}
		node, ok := x.nodes[name]
		if !ok {
			return "", fmt.Errorf("runtime archive link target %q is missing", name)
		}
		switch node.kind {
		case tar.TypeReg:
			return name, nil
		case tar.TypeLink, tar.TypeSymlink:
			target, err := resolve(node.target, depth+1)
			if err != nil {
				return "", err
			}
			resolved[name] = target
			return target, nil
		default:
			return "", fmt.Errorf("runtime archive link target %q is not a regular file", name)
		}
	}
	for name, node := range x.nodes {
		if err := ctx.Err(); err != nil {
			return err
		}
		for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
			if p, ok := x.nodes[parent]; ok && p.kind != tar.TypeDir {
				return fmt.Errorf("runtime archive parent %q is not a directory", parent)
			}
		}
		if node.kind == tar.TypeLink || node.kind == tar.TypeSymlink {
			if _, err := resolve(name, 0); err != nil {
				return err
			}
		}
	}
	for name, target := range resolved {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := x.root.MkdirAll(path.Dir(name), 0o700); err != nil {
			return err
		}
		node := x.nodes[name]
		var err error
		if node.kind == tar.TypeLink {
			err = x.root.Link(target, name)
		} else {
			// Create the validated canonical relationship. Keeping raw a/../b
			// would require a to exist as a directory, although validation already
			// reduced it to b; it could leave a dangling or non-directory link.
			var relative string
			relative, err = filepath.Rel(path.Dir(name), node.target)
			if err == nil {
				err = x.root.Symlink(filepath.ToSlash(relative), name)
			}
		}
		if err != nil {
			return fmt.Errorf("creating runtime archive link %q: %w", name, err)
		}
	}
	return nil
}
