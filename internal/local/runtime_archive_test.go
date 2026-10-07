package local

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type archiveMember struct {
	name, body, target string
	kind               byte
	mode               int64
}

func runtimeTar(t *testing.T, members ...archiveMember) []byte {
	t.Helper()
	var b bytes.Buffer
	w := tar.NewWriter(&b)
	for _, m := range members {
		kind := m.kind
		if kind == 0 {
			kind = tar.TypeReg
		}
		h := &tar.Header{Name: m.name, Typeflag: kind, Linkname: m.target, Mode: m.mode}
		if kind == tar.TypeReg {
			h.Size = int64(len(m.body))
		}
		if err := w.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(m.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func runtimeCompressed(t *testing.T, format string, payload []byte) []byte {
	t.Helper()
	if format == "tgz" {
		var b bytes.Buffer
		w := gzip.NewWriter(&b)
		if _, err := w.Write(payload); err != nil {
			t.Fatal(err)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		return b.Bytes()
	}
	// A real Zstandard frame with uncompressed blocks, without an external tool.
	// The decoder's upstream fixtures separately exercise compressed blocks.
	b := []byte{0x28, 0xb5, 0x2f, 0xfd, 0xa0}
	b = binary.LittleEndian.AppendUint32(b, uint32(len(payload)))
	for len(payload) > 0 {
		n := min(len(payload), 128<<10)
		header := n << 3
		if n == len(payload) {
			header |= 1
		}
		b = append(b, byte(header), byte(header>>8), byte(header>>16))
		b = append(b, payload[:n]...)
		payload = payload[n:]
	}
	return b
}

func smallArchiveLimits() archiveLimits {
	return archiveLimits{Compressed: 1 << 20, Expanded: 2 << 20, File: 1 << 20, Members: 50, Path: 512}
}

func TestRuntimeArchivePreservesBundle(t *testing.T) {
	for _, format := range []string{"tgz", "tar.zst"} {
		t.Run(format, func(t *testing.T) {
			dir := t.TempDir()
			payload := runtimeTar(t,
				archiveMember{name: "./", kind: tar.TypeDir, mode: 0o7777},
				archiveMember{name: "lib/alias.so", kind: tar.TypeSymlink, target: "lib.so"},
				archiveMember{name: "lib/normalized.so", kind: tar.TypeSymlink, target: "absent/../lib.so"},
				archiveMember{name: "lib/normalized-file.so", kind: tar.TypeSymlink, target: "lib.so/../lib.so"},
				archiveMember{name: "lib/hard.so", kind: tar.TypeLink, target: "lib/alias.so"},
				archiveMember{name: "bin/ollama", body: "executable", mode: 0o6755},
				archiveMember{name: "bin/llama-server", body: "companion", mode: 0o755},
				archiveMember{name: "lib/lib.so", body: "library", mode: 0o644},
				archiveMember{name: "NOTICE", body: "license", mode: 0o666})
			// GNU tar can append additional zero records after its terminator.
			payload = append(payload, make([]byte, 4096)...)
			err := extractRuntimeArchive(context.Background(), bytes.NewReader(runtimeCompressed(t, format, payload)), format, dir, smallArchiveLimits())
			if err != nil {
				t.Fatal(err)
			}
			for name, want := range map[string]string{"bin/ollama": "executable", "bin/llama-server": "companion", "lib/alias.so": "library", "lib/normalized.so": "library", "lib/normalized-file.so": "library", "lib/hard.so": "library", "NOTICE": "license"} {
				got, err := os.ReadFile(filepath.Join(dir, name))
				if err != nil || string(got) != want {
					t.Fatalf("%s = %q, %v", name, got, err)
				}
			}
			info, err := os.Stat(filepath.Join(dir, "bin/ollama"))
			if err != nil || info.Mode() != 0o700 {
				t.Fatalf("executable mode: %v, %v", info, err)
			}
			info, err = os.Stat(filepath.Join(dir, "NOTICE"))
			if err != nil || info.Mode() != 0o600 {
				t.Fatalf("notice mode: %v, %v", info, err)
			}
		})
	}
}

func TestRuntimeArchiveRejectsUnsafeMembers(t *testing.T) {
	tests := map[string][]archiveMember{
		"escape":            {{name: "../escape", body: "bad"}},
		"absolute":          {{name: "/tmp/escape", body: "bad"}},
		"windows":           {{name: `C:\escape`, body: "bad"}},
		"backslash":         {{name: `foo\bar`, body: "bad"}},
		"control":           {{name: "line\nbreak", body: "bad"}},
		"duplicate":         {{name: "bin/ollama", body: "a"}, {name: "./bin/ollama", body: "b"}},
		"directory-as-file": {{name: "bin/child", body: "a"}, {name: "bin", body: "b"}},
		"outside-link":      {{name: "lib/link", kind: tar.TypeSymlink, target: "../../escape"}},
		"absolute-link":     {{name: "link", kind: tar.TypeSymlink, target: "/etc/passwd"}},
		"outside-hardlink":  {{name: "link", kind: tar.TypeLink, target: "../escape"}},
		"dangling":          {{name: "link", kind: tar.TypeSymlink, target: "missing"}},
		"cycle":             {{name: "a", kind: tar.TypeSymlink, target: "b"}, {name: "b", kind: tar.TypeSymlink, target: "a"}},
		"directory-link":    {{name: "dir", kind: tar.TypeDir}, {name: "link", kind: tar.TypeSymlink, target: "dir"}},
		"link-parent":       {{name: "alias", kind: tar.TypeSymlink, target: "dir"}, {name: "alias/child", body: "bad"}},
		"fifo":              {{name: "fifo", kind: tar.TypeFifo}},
		"device":            {{name: "device", kind: tar.TypeChar}},
	}
	for name, members := range tests {
		t.Run(name, func(t *testing.T) {
			err := extractRuntimeArchive(context.Background(), bytes.NewReader(runtimeCompressed(t, "tgz", runtimeTar(t, members...))), "tgz", t.TempDir(), smallArchiveLimits())
			if err == nil {
				t.Fatal("unsafe archive accepted")
			}
		})
	}
}

func TestRuntimeArchiveBoundsAndDamage(t *testing.T) {
	payload := runtimeTar(t, archiveMember{name: "file", body: strings.Repeat("payload", 1000)})
	for _, format := range []string{"tgz", "tar.zst"} {
		for _, damage := range []string{"compressed limit", "expanded limit", "file limit", "member limit", "path limit", "truncated", "trailing content", "checksum"} {
			t.Run(format+"/"+damage, func(t *testing.T) {
				limits := smallArchiveLimits()
				data := runtimeCompressed(t, format, payload)
				switch damage {
				case "compressed limit":
					limits.Compressed = int64(len(data) - 1)
				case "expanded limit":
					limits.Expanded = int64(len(payload) - 1)
				case "file limit":
					limits.File = 10
				case "member limit":
					limits.Members = 0
				case "path limit":
					limits.Path = 2
				case "truncated":
					data = data[:len(data)-1]
				case "trailing content":
					data = runtimeCompressed(t, format, append(bytes.Clone(payload), 'x'))
				case "checksum":
					if format != "tgz" {
						t.Skip("Zstandard checksum coverage is in internal/zstd")
					}
					data[len(data)-8] ^= 1
				}
				if err := extractRuntimeArchive(context.Background(), bytes.NewReader(data), format, t.TempDir(), limits); err == nil {
					t.Fatal("damaged or oversized archive accepted")
				}
			})
		}
	}
}

type cancelArchiveReader struct {
	r      io.Reader
	cancel context.CancelFunc
	after  int
}

func (r *cancelArchiveReader) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	r.after -= n
	if r.after <= 0 {
		r.cancel()
	}
	return n, err
}

func TestRuntimeArchiveCancellationAndOccupiedDestination(t *testing.T) {
	data := runtimeCompressed(t, "tar.zst", runtimeTar(t, archiveMember{name: "file", body: strings.Repeat("x", 128<<10)}))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err := extractRuntimeArchive(ctx, &cancelArchiveReader{r: bytes.NewReader(data), cancel: cancel, after: 2000}, "tar.zst", t.TempDir(), smallArchiveLimits())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation = %v", err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "existing"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := extractRuntimeArchive(context.Background(), bytes.NewReader(data), "tar.zst", dir, smallArchiveLimits()); err == nil {
		t.Fatal("occupied staging accepted")
	}
	if b, err := os.ReadFile(filepath.Join(dir, "existing")); err != nil || string(b) != "keep" {
		t.Fatalf("existing file changed: %q, %v", b, err)
	}
}
