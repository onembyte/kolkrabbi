package local

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// RuntimeRelease pins exactly which sidecar bytes Kolkrabbi is willing to run.
//
// SHA256 is not optional. Kolkrabbi starts this binary itself, so installing
// whatever a URL happened to return would make the managed runtime an
// unreviewed execution path in a tool whose whole local story is "Kolkrabbi
// owns its own runtime".
type RuntimeRelease struct {
	Version string
	URL     string
	SHA256  string
	// Size, when known, bounds the download. It is a guard against an endless
	// or substituted body, not a substitute for the checksum.
	Size uint64
}

// Fetcher opens the bytes for one release. It is injected so installation can
// be tested exactly, including its failures, without a network.
type Fetcher func(ctx context.Context, url string) (io.ReadCloser, error)

// maxRuntimeBytes bounds a release with no declared size. A managed inference
// runtime is tens of megabytes; anything approaching this is not one.
const maxRuntimeBytes = 1 << 30

// InstallRuntime places a pinned, verified sidecar at dest.
//
// Nothing is executed here, and nothing reaches dest until its bytes have been
// checked: the download lands in a temporary file beside the destination and is
// renamed only after the checksum matches. A rejected download leaves nothing
// behind.
func InstallRuntime(ctx context.Context, release RuntimeRelease, dest string, fetch Fetcher) error {
	if strings.TrimSpace(release.SHA256) == "" {
		// Refuse before fetching: with nothing to verify against there would be
		// no way to judge what came back.
		return fmt.Errorf("runtime %s has no pinned checksum, so its bytes cannot be verified", release.Version)
	}
	if fetch == nil {
		return fmt.Errorf("no way to fetch runtime %s", release.Version)
	}
	if installed, err := runtimeMatches(dest, release.SHA256); err != nil {
		return err
	} else if installed {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return fmt.Errorf("creating the managed runtime directory: %w", err)
	}
	body, err := fetch(ctx, release.URL)
	if err != nil {
		return fmt.Errorf("downloading runtime %s: %w", release.Version, err)
	}
	defer func() { _ = body.Close() }()

	temp, err := os.CreateTemp(filepath.Dir(dest), ".runtime-*")
	if err != nil {
		return fmt.Errorf("staging runtime %s: %w", release.Version, err)
	}
	staged := temp.Name()
	defer func() {
		_ = temp.Close()
		_ = os.Remove(staged)
	}()

	limit := int64(maxRuntimeBytes)
	if release.Size > 0 {
		limit = int64(release.Size)
	}
	digest := sha256.New()
	// limit+1 so a body that exceeds what was promised is detected rather than
	// silently truncated into a checksum failure with a misleading reason.
	written, err := io.Copy(io.MultiWriter(temp, digest), io.LimitReader(body, limit+1))
	if err != nil {
		return fmt.Errorf("downloading runtime %s: %w", release.Version, err)
	}
	if written > limit {
		return fmt.Errorf("runtime %s sent more than the %d bytes it declared", release.Version, limit)
	}
	if got := hex.EncodeToString(digest.Sum(nil)); !strings.EqualFold(got, release.SHA256) {
		return fmt.Errorf("runtime %s failed its checksum: got %s, pinned %s", release.Version, got, release.SHA256)
	}
	if err := temp.Chmod(0o755); err != nil {
		return fmt.Errorf("marking runtime %s executable: %w", release.Version, err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("staging runtime %s: %w", release.Version, err)
	}
	if err := os.Rename(staged, dest); err != nil {
		return fmt.Errorf("installing runtime %s: %w", release.Version, err)
	}
	return nil
}

// runtimeMatches reports whether the installed binary is already the pinned
// one, so a correct install is never downloaded again.
func runtimeMatches(dest, want string) (bool, error) {
	file, err := os.Open(dest)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("reading the installed runtime: %w", err)
	}
	defer func() { _ = file.Close() }()
	digest := sha256.New()
	if _, err := io.Copy(digest, io.LimitReader(file, maxRuntimeBytes)); err != nil {
		return false, fmt.Errorf("reading the installed runtime: %w", err)
	}
	return strings.EqualFold(hex.EncodeToString(digest.Sum(nil)), want), nil
}

// pinnedRuntime is the release Kolkrabbi is willing to install and run.
//
// It is deliberately empty. Filling it in means choosing a specific upstream
// build and recording the checksum of the exact bytes that were reviewed, and
// nobody has done that yet. A plausible-looking URL with an unverified digest
// would be worse than nothing here: it would turn "verified" into a word rather
// than a property, in the one code path that installs an executable Kolkrabbi
// then runs itself.
//
// To enable managed local models: verify an upstream release, record its
// version, URL and SHA-256 below, and the install path is already tested.
var pinnedRuntime = RuntimeRelease{}

// PinnedRuntime returns the release this build may install, and whether there
// is one at all.
func PinnedRuntime() (RuntimeRelease, bool) {
	if pinnedRuntime.Version == "" || pinnedRuntime.URL == "" || pinnedRuntime.SHA256 == "" {
		return RuntimeRelease{}, false
	}
	return pinnedRuntime, true
}
