package buildinfo

import (
	"runtime"
	"strings"
	"testing"
)

func TestGetNeverReturnsAnUnknownVersion(t *testing.T) {
	i := Get()
	if i.Version == "" {
		t.Error("Version is empty; an unstamped build must still say 'dev'")
	}
	if i.Version == "(devel)" {
		t.Error(`Version leaked the toolchain's "(devel)" instead of "dev"`)
	}
	if i.Go != runtime.Version() || i.OS != runtime.GOOS || i.Arch != runtime.GOARCH {
		t.Errorf("Get() = %+v, does not match the running toolchain", i)
	}
}

func TestStringIsOneLineAndNamesTheBinary(t *testing.T) {
	got := Get().String()
	if strings.Contains(got, "\n") {
		t.Errorf("version line contains a newline: %q", got)
	}
	if !strings.HasPrefix(got, "kolk ") {
		t.Errorf("version line does not start with the binary name: %q", got)
	}
	for _, want := range []string{runtime.Version(), runtime.GOOS + "/" + runtime.GOARCH} {
		if !strings.Contains(got, want) {
			t.Errorf("version line %q omits %q", got, want)
		}
	}
}

func TestStringWithStampedValues(t *testing.T) {
	i := Info{Version: "v0.1.0", Commit: "abc123def456", Date: "2026-08-22T10:00:00Z", Go: "go1.25.0", OS: "linux", Arch: "amd64"}
	want := "kolk v0.1.0 (abc123def456, 2026-08-22T10:00:00Z) go1.25.0 linux/amd64"
	if got := i.String(); got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

func TestStringWithoutCommit(t *testing.T) {
	i := Info{Version: "dev", Go: "go1.25.0", OS: "darwin", Arch: "arm64"}
	want := "kolk dev go1.25.0 darwin/arm64"
	if got := i.String(); got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

func TestShortCommit(t *testing.T) {
	if got := shortCommit("0123456789abcdef0123"); got != "0123456789ab" {
		t.Errorf("shortCommit = %q", got)
	}
	if got := shortCommit("abc"); got != "abc" {
		t.Errorf("shortCommit = %q, short revisions must pass through", got)
	}
}
