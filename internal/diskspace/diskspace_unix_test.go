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

func TestFreeMeasuresTheFilesystemAPathWillLiveOn(t *testing.T) {
	// Kolkrabbi's managed model directory does not exist until the first pull,
	// and reporting it as unmeasurable would refuse every pull forever.
	base := t.TempDir()
	deep, ok := Free(base + "/local-models/blobs")
	if !ok {
		t.Fatal("a path whose ancestor exists must be measurable")
	}
	here, _ := Free(base)
	if deep != here {
		t.Fatalf("nested path measured %d, its existing ancestor %d", deep, here)
	}
}
