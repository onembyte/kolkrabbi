//go:build darwin || linux

package diskspace

import "testing"

func TestDiskFreeBytesMeasuresARealDirectory(t *testing.T) {
	free, ok := Free(t.TempDir())
	if !ok {
		t.Fatal("a directory that exists must be measurable")
	}
	if free == 0 {
		t.Fatal("a writable temp directory reported zero available bytes")
	}
}

func TestDiskFreeBytesIsUnknownForAPathThatIsNotThere(t *testing.T) {
	if _, ok := Free(t.TempDir() + "/definitely/not/here"); ok {
		t.Fatal("a missing path must be unknown, not a number")
	}
}
