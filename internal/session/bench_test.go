package session

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

// benchSession fills a session with assistant/user turns until its transcript
// is about size bytes, which is what Save later marshals and fsyncs whole.
func benchSession(dir string, size int) *Session {
	s := New(dir, "bench/model")
	s.Title = "benchmark transcript"
	const chunk = 4 << 10
	for written := 0; written < size; written += chunk {
		s.AppendMessage(provider.Message{Role: "user", Content: strings.Repeat("q", 64)})
		s.AppendMessage(provider.Message{Role: "assistant", Content: strings.Repeat("a", chunk)})
	}
	return s
}

// BenchmarkSave is the O0.3 baseline for OPTIMIZATION_PLAN.md O3: one
// MarshalIndent of the whole transcript plus atomicfile.Write's temp file,
// file fsync, rename and directory fsync -- paid at least three times a turn
// (agent.go:1488/1577/1634), so multiply by three for the per-turn cost.
func BenchmarkSave(b *testing.B) {
	for _, tc := range []struct {
		name string
		size int
	}{
		{name: "100KB", size: 100 << 10},
		{name: "1MB", size: 1 << 20},
		{name: "5MB", size: 5 << 20},
	} {
		b.Run(tc.name, func(b *testing.B) {
			s := benchSession(b.TempDir(), tc.size)
			if err := s.Save(); err != nil {
				b.Fatalf("Save: %v", err)
			}
			info, err := os.Stat(filepath.Join(s.dir, s.ID+".json"))
			if err != nil {
				b.Fatalf("stat session: %v", err)
			}
			b.SetBytes(info.Size())
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := s.Save(); err != nil {
					b.Fatalf("Save: %v", err)
				}
			}
		})
	}
}

// BenchmarkLatestForDir is the O0.5 baseline for OPTIMIZATION_PLAN.md O6:
// `kolk -r` reads and unmarshals every session file in the directory to pick
// the one it will resume. Each session here is about 200 KB, so 200 sessions
// is the 40 MB a long-lived install makes it parse at startup.
func BenchmarkLatestForDir(b *testing.B) {
	const perSession = 200 << 10
	for _, count := range []int{10, 200} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			dir := b.TempDir()
			cwd := filepath.Join(dir, "project")
			if err := os.MkdirAll(cwd, 0o700); err != nil {
				b.Fatalf("mkdir project: %v", err)
			}
			for i := 0; i < count; i++ {
				s := benchSession(dir, perSession)
				s.CWD = cwd
				if err := s.Save(); err != nil {
					b.Fatalf("Save: %v", err)
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				s, err := LatestForDir(dir, cwd)
				if err != nil {
					b.Fatalf("LatestForDir: %v", err)
				}
				if s == nil {
					b.Fatal("LatestForDir found no session")
				}
			}
		})
	}
}
