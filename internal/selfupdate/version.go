// Package selfupdate verifies and installs Kolkrabbi releases from the one
// official GitHub release origin.
package selfupdate

import (
	"fmt"
	"strconv"
	"strings"
)

type stableVersion struct {
	major uint64
	minor uint64
	patch uint64
}

func parseStableVersion(raw string) (stableVersion, error) {
	original := raw
	raw = strings.TrimPrefix(raw, "v")
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return stableVersion{}, fmt.Errorf("version %q is not a stable semantic version", original)
	}
	values := make([]uint64, len(parts))
	for i, part := range parts {
		if part == "" || (len(part) > 1 && part[0] == '0') {
			return stableVersion{}, fmt.Errorf("version %q is not a stable semantic version", original)
		}
		for _, digit := range part {
			if digit < '0' || digit > '9' {
				return stableVersion{}, fmt.Errorf("version %q is not a stable semantic version", original)
			}
		}
		value, err := strconv.ParseUint(part, 10, 64)
		if err != nil {
			return stableVersion{}, fmt.Errorf("version %q is not a stable semantic version: %w", original, err)
		}
		values[i] = value
	}
	return stableVersion{major: values[0], minor: values[1], patch: values[2]}, nil
}

func (v stableVersion) String() string {
	return fmt.Sprintf("%d.%d.%d", v.major, v.minor, v.patch)
}

func (v stableVersion) Compare(other stableVersion) int {
	left := [...]uint64{v.major, v.minor, v.patch}
	right := [...]uint64{other.major, other.minor, other.patch}
	for i := range left {
		switch {
		case left[i] < right[i]:
			return -1
		case left[i] > right[i]:
			return 1
		}
	}
	return 0
}

type releaseTarget struct {
	goos   string
	goarch string
}

func resolveTarget(goos, goarch string) (releaseTarget, error) {
	if (goos != "darwin" && goos != "linux") || (goarch != "amd64" && goarch != "arm64") {
		return releaseTarget{}, fmt.Errorf("unsupported update target %s/%s (supported: darwin/linux on amd64/arm64)", goos, goarch)
	}
	return releaseTarget{goos: goos, goarch: goarch}, nil
}

func (t releaseTarget) archiveName(version stableVersion) string {
	return fmt.Sprintf("kolk_%s_%s_%s.tar.gz", version, t.goos, t.goarch)
}
