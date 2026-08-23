package selfupdate

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/onembyte/kolkrabbi/internal/atomicfile"
	"github.com/onembyte/kolkrabbi/internal/buildinfo"
)

const requestTimeout = 2 * time.Minute

// Result describes one completed update check. Updated is false when the
// running build is already the latest release or newer than it.
type Result struct {
	Current string
	Latest  string
	Path    string
	Updated bool
	Warning string
}

type replaceFunc func(path string, data []byte, perm os.FileMode) error

type updater struct {
	client         *http.Client
	releasesURL    string
	currentVersion string
	goos           string
	goarch         string
	executable     func() (string, error)
	replace        replaceFunc
}

// Update checks the official latest stable release and atomically installs it
// over the running executable when it is newer.
func Update(ctx context.Context) (Result, error) {
	return productionUpdater().run(ctx)
}

func productionUpdater() updater {
	return updater{
		client:         &http.Client{Timeout: requestTimeout},
		releasesURL:    officialReleasesURL,
		currentVersion: buildinfo.Get().Version,
		goos:           runtime.GOOS,
		goarch:         runtime.GOARCH,
		executable:     os.Executable,
		replace:        atomicfile.Write,
	}
}

func (u updater) run(ctx context.Context) (Result, error) {
	current, err := parseStableVersion(u.currentVersion)
	if err != nil {
		return Result{}, fmt.Errorf("cannot update running build: %w", err)
	}
	target, err := resolveTarget(u.goos, u.goarch)
	if err != nil {
		return Result{}, err
	}
	if u.client == nil {
		return Result{}, fmt.Errorf("update HTTP client is not configured")
	}

	latest, err := discoverLatest(ctx, u.client, u.releasesURL)
	if err != nil {
		return Result{}, err
	}
	result := Result{Current: current.String(), Latest: latest.String()}
	if current.Compare(latest) >= 0 {
		return result, nil
	}
	if u.executable == nil {
		return Result{}, fmt.Errorf("executable resolver is not configured")
	}
	rawPath, err := u.executable()
	if err != nil {
		return Result{}, fmt.Errorf("locating the running executable: %w", err)
	}
	path, err := resolveExecutable(rawPath)
	if err != nil {
		return Result{}, err
	}

	binary, err := fetchVerifiedArtifact(ctx, u.client, u.releasesURL, latest, target)
	if err != nil {
		return Result{}, err
	}
	if err := ctx.Err(); err != nil {
		return Result{}, fmt.Errorf("update cancelled before replacement: %w", err)
	}
	if u.replace == nil {
		return Result{}, fmt.Errorf("executable replacement is not configured")
	}
	result.Path = path
	if err := u.replace(path, binary, 0o755); err != nil {
		var durability *atomicfile.DurabilityError
		if errors.As(err, &durability) {
			result.Updated = true
			result.Warning = durability.Error()
			return result, nil
		}
		return Result{}, fmt.Errorf("replacing %s: %w", path, err)
	}
	result.Updated = true
	return result, nil
}

func resolveExecutable(rawPath string) (string, error) {
	if rawPath == "" {
		return "", fmt.Errorf("running executable path is empty")
	}
	abs, err := filepath.Abs(rawPath)
	if err != nil {
		return "", fmt.Errorf("resolving executable path %q: %w", rawPath, err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("resolving running executable %s: %w", abs, err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("inspecting running executable %s: %w", resolved, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("running executable is not a regular file: %s", resolved)
	}
	return resolved, nil
}
