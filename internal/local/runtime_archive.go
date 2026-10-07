package local

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/onembyte/kolkrabbi/internal/zstd"
)

type archiveLimits struct {
	Compressed, Expanded, File int64
	Members, Path              int
	// reserve checks free space with the archive and prior members still on disk.
	reserve func(int64) error
}

// These are ceilings, not allocation sizes. Both compressed and expanded data
// stream through bounded buffers. Metadata and tar padding count as expansion.
var runtimeArchiveLimits = archiveLimits{
	Compressed: 4 << 30, Expanded: 12 << 30, File: 8 << 30,
	Members: 20000, Path: 4096,
}

// extractRuntimeArchive writes only into a caller-owned, empty staging directory.
// The caller removes that entire directory on failure and publishes it only after
// digest, extraction and executable validation all succeed.
func extractRuntimeArchive(ctx context.Context, source io.Reader, format, dest string, limits archiveLimits) error {
	root, err := os.OpenRoot(dest)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	dir, err := root.Open(".")
	if err != nil {
		return err
	}
	entries, readErr := dir.Readdirnames(1)
	_ = dir.Close()
	if (readErr != nil && !errors.Is(readErr, io.EOF)) || len(entries) != 0 {
		return fmt.Errorf("runtime extraction needs an empty staging directory")
	}
	compressed := &archiveStream{ctx: ctx, r: source, remaining: limits.Compressed, label: "compressed archive"}
	var decoded io.Reader
	switch format {
	case "tgz":
		gz, err := gzip.NewReader(compressed)
		if err != nil {
			return fmt.Errorf("opening runtime gzip: %w", err)
		}
		defer func() { _ = gz.Close() }()
		decoded = gz
	case "tar.zst":
		decoded = zstd.NewReader(compressed)
	default:
		return fmt.Errorf("unsupported runtime archive format %q", format)
	}
	expanded := &archiveStream{ctx: ctx, r: decoded, remaining: limits.Expanded, label: "expanded archive"}
	tr := tar.NewReader(expanded)
	x := archiveExtractor{root: root, limits: limits, nodes: make(map[string]archiveNode)}
	for count := 0; ; count++ {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("reading runtime tar: %w", err)
		}
		if count >= limits.Members {
			return fmt.Errorf("runtime archive exceeds member limit")
		}
		if err := x.member(ctx, h, tr); err != nil {
			return err
		}
	}
	// tar stops at its end records. Finish the compressed stream to verify its
	// checksum/trailer and reject hidden trailing archives while allowing GNU padding.
	buf := make([]byte, 32<<10)
	zeros := make([]byte, len(buf))
	for {
		n, err := expanded.Read(buf)
		if !bytes.Equal(buf[:n], zeros[:n]) {
			return fmt.Errorf("runtime archive has nonzero trailing content")
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("finishing runtime archive: %w", err)
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := x.links(ctx); err != nil {
		return err
	}
	return syncRuntimeDirectories(ctx, root)
}

type archiveStream struct {
	ctx       context.Context
	r         io.Reader
	remaining int64
	label     string
}

func (r *archiveStream) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if r.remaining < 0 {
		return 0, fmt.Errorf("%s exceeds size limit", r.label)
	}
	if int64(len(p)) > r.remaining {
		p = p[:r.remaining+1]
	}
	n, err := r.r.Read(p)
	r.remaining -= int64(n)
	if cancelErr := r.ctx.Err(); cancelErr != nil {
		return 0, cancelErr
	}
	if r.remaining < 0 {
		return 0, fmt.Errorf("%s exceeds size limit", r.label)
	}
	return n, err
}

type archiveNode struct {
	kind   byte
	target string
}

type archiveExtractor struct {
	root    *os.Root
	limits  archiveLimits
	nodes   map[string]archiveNode
	written int64
}

func (x *archiveExtractor) member(ctx context.Context, h *tar.Header, r io.Reader) error {
	name, err := archivePath(h.Name, x.limits.Path, false)
	if err != nil {
		return err
	}
	if _, exists := x.nodes[name]; exists {
		return fmt.Errorf("duplicate runtime archive path %q", name)
	}
	if h.Typeflag == tar.TypeGNUSparse {
		return fmt.Errorf("sparse runtime archive members are unsupported")
	}
	for key := range h.PAXRecords {
		if strings.HasPrefix(key, "GNU.sparse.") {
			return fmt.Errorf("sparse runtime archive members are unsupported")
		}
	}
	if name == "." && h.Typeflag != tar.TypeDir {
		return fmt.Errorf("invalid runtime archive root member")
	}
	if h.Size < 0 || h.Size > x.limits.File || h.Size > x.limits.Expanded-x.written {
		return fmt.Errorf("runtime archive file %q exceeds size limit", name)
	}
	node := archiveNode{kind: h.Typeflag}
	switch h.Typeflag {
	case tar.TypeDir:
		if h.Size != 0 {
			return fmt.Errorf("runtime archive directory %q has data", name)
		}
		if err := x.root.MkdirAll(name, 0o700); err != nil {
			return err
		}
	case tar.TypeReg:
		if x.limits.reserve != nil {
			if err := x.limits.reserve(h.Size); err != nil {
				return err
			}
		}
		if err := x.root.MkdirAll(path.Dir(name), 0o700); err != nil {
			return err
		}
		mode := os.FileMode(0o600)
		if h.Mode&0o111 != 0 {
			mode = 0o700
		}
		f, err := x.root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
		if err != nil {
			return err
		}
		// CopyN also bounds synthetic output from malformed or sparse tar readers.
		n, copyErr := io.CopyN(f, &archiveStream{ctx: ctx, r: r, remaining: h.Size, label: "runtime file"}, h.Size)
		x.written += n
		if copyErr == nil {
			copyErr = f.Sync()
		}
		if err := errors.Join(copyErr, f.Close()); err != nil {
			return fmt.Errorf("extracting %q: %w", name, err)
		}
	case tar.TypeSymlink, tar.TypeLink:
		if h.Size != 0 {
			return fmt.Errorf("runtime archive link %q has data", name)
		}
		if err := validateArchiveText(h.Linkname, x.limits.Path); err != nil {
			return err
		}
		target := h.Linkname
		if h.Typeflag == tar.TypeSymlink {
			target = path.Join(path.Dir(name), target)
		}
		node.target, err = archivePath(target, x.limits.Path, true)
		if err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported runtime archive member %q (type %d)", name, h.Typeflag)
	}
	x.nodes[name] = node
	return nil
}

func validateArchiveText(name string, limit int) error {
	if name == "" || len(name) > limit || !utf8.ValidString(name) || strings.HasPrefix(name, "/") || strings.ContainsAny(name, "\\:") {
		return fmt.Errorf("unsafe runtime archive path %q", name)
	}
	for _, r := range name {
		if r < 32 || r == 127 {
			return fmt.Errorf("unsafe runtime archive path %q", name)
		}
	}
	return nil
}

func archivePath(name string, limit int, relativeLink bool) (string, error) {
	if err := validateArchiveText(name, limit); err != nil {
		return "", err
	}
	if !relativeLink {
		for _, part := range strings.Split(name, "/") {
			if part == ".." {
				return "", fmt.Errorf("unsafe runtime archive path %q", name)
			}
		}
	}
	clean := path.Clean(name)
	if strings.Count(clean, "/") >= 64 {
		return "", fmt.Errorf("runtime archive path exceeds directory depth limit")
	}
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("runtime archive path escapes staging: %q", name)
	}
	return clean, nil
}
