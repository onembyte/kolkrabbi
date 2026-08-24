package term

import (
	"errors"
	"testing"
)

func TestEnterRawRestoresTheExactStateOnce(t *testing.T) {
	state := &struct{ name string }{name: "original"}
	makeCalls := 0
	restoreCalls := 0
	restore := func(fd int, got any) error {
		restoreCalls++
		if fd != 42 || got != state {
			t.Fatalf("restore(%d, %#v), want fd 42 and original state", fd, got)
		}
		return nil
	}

	cleanup, err := enterRawWith(42, func(fd int) (any, error) {
		makeCalls++
		if fd != 42 {
			t.Fatalf("makeRaw fd = %d", fd)
		}
		return state, nil
	}, restore)
	if err != nil {
		t.Fatal(err)
	}
	if err := cleanup(); err != nil {
		t.Fatal(err)
	}
	if err := cleanup(); err != nil {
		t.Fatal(err)
	}
	if makeCalls != 1 || restoreCalls != 1 {
		t.Fatalf("make/restore calls = %d/%d, want 1/1", makeCalls, restoreCalls)
	}
}

func TestEnterRawReturnsSetupAndRestoreErrors(t *testing.T) {
	setupErr := errors.New("make raw")
	cleanup, err := enterRawWith(1, func(int) (any, error) {
		return nil, setupErr
	}, func(int, any) error {
		t.Fatal("restore called after failed setup")
		return nil
	})
	if !errors.Is(err, setupErr) || cleanup != nil {
		t.Fatalf("failed setup = (cleanup present: %v, error: %v), want nil cleanup and wrapped setup error",
			cleanup != nil, err)
	}

	restoreErr := errors.New("restore raw")
	cleanup, err = enterRawWith(1, func(int) (any, error) {
		return "state", nil
	}, func(int, any) error { return restoreErr })
	if err != nil {
		t.Fatal(err)
	}
	if err := cleanup(); !errors.Is(err, restoreErr) {
		t.Fatalf("cleanup error = %v, want %v", err, restoreErr)
	}
	if err := cleanup(); !errors.Is(err, restoreErr) {
		t.Fatalf("idempotent cleanup error = %v, want the first restore result", err)
	}
}

func TestTerminalSizeUsesProbeAndSafeFallbacks(t *testing.T) {
	width, height := sizeFor(42, func(fd int) (int, int, error) {
		if fd != 42 {
			t.Fatalf("size probe fd = %d", fd)
		}
		return 132, 43, nil
	}, 80)
	if width != 132 || height != 43 {
		t.Fatalf("probed size = %dx%d", width, height)
	}

	width, height = sizeFor(42, func(int) (int, int, error) {
		return 0, 0, errors.New("not a terminal")
	}, 96)
	if width != 96 || height != 24 {
		t.Fatalf("fallback size = %dx%d, want 96x24", width, height)
	}
}
