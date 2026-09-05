package bench

import (
	"errors"
	"testing"
)

func TestParsePort(t *testing.T) {
	if n, err := ParsePort("8080"); err != nil || n != 8080 {
		t.Fatalf("ParsePort(8080) = %d, %v", n, err)
	}
	if _, err := ParsePort(""); !errors.Is(err, ErrEmpty) {
		t.Errorf("empty: want ErrEmpty, got %v", err)
	}
	if _, err := ParsePort("not-a-number"); err == nil {
		t.Errorf("garbage input: want an error, got nil")
	}
}
