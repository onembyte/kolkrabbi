package local

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/onembyte/kolkrabbi/internal/atomicfile"
	"github.com/onembyte/kolkrabbi/internal/diskspace"
	"github.com/onembyte/kolkrabbi/internal/lock"
	"github.com/onembyte/kolkrabbi/internal/shell"
)

// stagingPrefix names the directory each setup attempt downloads into.
const stagingPrefix = ".install-"

// sweepStaging removes staging directories a setup left behind. It runs under
// install.lock, so no other setup is using one: staging outlives its setup
// only when the process died before its deferred cleanup, and kept, its bytes
// would count against every later attempt's room check. Only directories are
// removed; a symlink that reads like staging is not kolk's and is left alone.
// Best effort: one that will not go is reported by path and setup carries on,
// so a later "not enough disk space" does not leave anyone guessing.
func (i *RuntimeInstaller) sweepStaging() {
	entries, err := os.ReadDir(i.Dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), stagingPrefix) {
			path := filepath.Join(i.Dir, entry.Name())
			if err := os.RemoveAll(path); err != nil {
				i.reportNote(fmt.Sprintf("could not remove %s, left by an interrupted setup: %v; it still takes disk space", path, err))
			}
		}
	}
}

// SweepStale sweeps for a start, which never waits on a setup: it takes
// install.lock only if it is free. A setup holding the lock sweeps under it
// itself. A start is where the space comes back when no setup follows (a
// complete runtime, or the user's own Ollama). Storage that does not exist,
// or is a symlink (which Ensure refuses), is left alone.
func (i *RuntimeInstaller) SweepStale() {
	if info, err := os.Lstat(i.Dir); err != nil || !info.IsDir() {
		return
	}
	held, err := lock.Try(filepath.Join(i.Dir, "install.lock"))
	if err != nil {
		return
	}
	defer func() { _ = held.Close() }()
	i.sweepStaging()
}

// RuntimeProgress reports bounded, observable setup stages. Download bytes refer
// to the runtime bundle, never to a model or an estimated amount of work.
type RuntimeProgress struct {
	Stage, Version   string
	Completed, Total int64
	// Bundle names the companion a download stage belongs to; empty is the
	// standard bundle.
	Bundle string
	// Note is the text of a "note" stage: something setup could not do,
	// said instead of failing, such as a companion that was not added.
	Note string
}

// RuntimeInstaller installs official native Ollama bundles into user storage.
// Find is read-only. Ensure is called only for an explicit local setup/use request.
// Runtime processes and cached model weights have separate owners/lifetimes.
type RuntimeInstaller struct {
	Dir       string
	Progress  func(RuntimeProgress)
	client    *http.Client
	platform  string
	freeSpace func(string) (uint64, bool)
	// Hardware is where companion detection reads this machine: /etc and
	// /sys, relative to a filesystem root. Nil means the real root, except
	// under a fixture platform, where it means no companion is needed.
	Hardware fs.FS
	// CPUOnly is the user's own choice of CPU (local.gpu_mode cpu): no
	// accelerator bundle is needed, fetched, reported missing or noted.
	CPUOnly bool
}

// acquisitionNeed is what setup acts on: the companion to fetch and the note
// to show. CPU chosen wants neither.
func (i *RuntimeInstaller) acquisitionNeed() runtimeCompanionNeed {
	if i.CPUOnly {
		return runtimeCompanionNeed{}
	}
	return i.companionNeed()
}

// companionNeed is detection over this installer's machine: what is true of
// the hardware, whatever the user chose.
func (i *RuntimeInstaller) companionNeed() runtimeCompanionNeed {
	root := i.Hardware
	if root == nil && i.platform == "" {
		root = os.DirFS("/")
	}
	return detectRuntimeCompanion(i.platformName(), root)
}

func (i *RuntimeInstaller) platformName() string {
	if i.platform != "" {
		return i.platform
	}
	return runtime.GOOS + "/" + runtime.GOARCH
}

func (i *RuntimeInstaller) currentRecordName() string {
	return "current-" + strings.ReplaceAll(i.platformName(), "/", "-") + ".json"
}

func (i *RuntimeInstaller) report(stage, version string, completed, total int64) {
	i.reportBundle(stage, version, "", completed, total)
}

func (i *RuntimeInstaller) reportNote(note string) {
	if i.Progress != nil {
		i.Progress(RuntimeProgress{Stage: "note", Note: note})
	}
}

func (i *RuntimeInstaller) reportBundle(stage, version, bundle string, completed, total int64) {
	if i.Progress != nil {
		i.Progress(RuntimeProgress{Stage: stage, Version: version, Completed: completed, Total: total, Bundle: bundle})
	}
}

// Ensure reuses a completed installation without a network request, or fetches
// the latest official stable bundle. It never overwrites an installed version.
func (i *RuntimeInstaller) Ensure(ctx context.Context) (Host, error) {
	if err := ctx.Err(); err != nil {
		return Host{}, err
	}
	platform, ok := runtimePlatforms[i.platformName()]
	if !ok {
		return Host{}, fmt.Errorf("%w for %s; install Ollama separately and Kolk will discover it", errManagedSetupUnsupported, i.platformName())
	}
	if i.platform == "" {
		if err := shell.CheckOllamaPlatform(ctx); err != nil {
			return Host{}, err
		}
	}
	if !filepath.IsAbs(i.Dir) {
		return Host{}, fmt.Errorf("managed runtime storage must be an absolute path")
	}
	if err := os.MkdirAll(i.Dir, 0o700); err != nil {
		return Host{}, err
	}
	info, err := os.Lstat(i.Dir)
	if err != nil {
		return Host{}, err
	}
	if !info.IsDir() {
		return Host{}, fmt.Errorf("managed runtime storage is not a directory")
	}
	i.report("waiting", "", 0, 0)
	held, err := lock.Acquire(ctx, filepath.Join(i.Dir, "install.lock"))
	if err != nil {
		return Host{}, fmt.Errorf("locking native runtime setup: %w", err)
	}
	defer func() { _ = held.Close() }()
	i.sweepStaging()
	host, current, err := i.current()
	if err != nil {
		return Host{}, err
	}
	// A tree already carrying the needed companion, or any tree when none is
	// needed, is reused offline. A tree with a companion the hardware no
	// longer needs still runs, so it is kept rather than rebuilt.
	need := i.acquisitionNeed()
	if host.State == HostInstalled && (need.Name == "" || current.Companion == need.Name) {
		i.report("ready", host.Version, 0, 0)
		return host, nil
	}
	installed, err := i.install(ctx, platform, need, host.State == HostInstalled)
	if err != nil && host.State == HostInstalled && ctx.Err() == nil {
		// The companion only adds an accelerator and offline reuse is the
		// promise: keep the working runtime and say what was not added.
		i.reportNote(fmt.Sprintf("the %s for %s could not be added (%v); using the installed runtime without it", RuntimeBundleLabel(need.Name), need.Reason, err))
		i.report("ready", host.Version, 0, 0)
		return host, nil
	}
	return installed, err
}

// install fetches, verifies and publishes a new tree for the latest release,
// with the companion need names, and points the platform record at it.
//
// A content failure (verification, unpacking, merging) while upgrading a
// working tree is remembered for the tree it would have built; the next
// upgrade does not download that release again, since it would fail the same
// way. A fresh installation always tries, and a new release retries.
func (i *RuntimeInstaller) install(ctx context.Context, platform runtimePlatform, need runtimeCompanionNeed, upgrading bool) (installed Host, err error) {
	i.report("checking", "", 0, 0)
	companion := runtimeCompanions[i.platformName()][need.Name]
	release, err := i.releaseFor(ctx, platform, companion)
	if err != nil && companion.Asset != "" && !upgrading && ctx.Err() == nil {
		// A first install is not held hostage by its accelerator bundle: a
		// release that does not offer it still installs the runtime.
		if plain, plainErr := i.releaseFor(ctx, platform, runtimePlatform{}); plainErr == nil {
			i.reportNote(fmt.Sprintf("the %s for %s could not be added (%v); installing the runtime without it", RuntimeBundleLabel(need.Name), need.Reason, err))
			release, err = plain, nil
		}
	}
	if err != nil {
		return Host{}, err
	}
	record := runtimeInstallation{Version: release.Tag, Platform: i.platformName(), Digest: release.Asset.Digest, Binary: platform.Binary}
	if release.Companion != nil {
		record.Companion, record.CompanionDigest = need.Name, release.Companion.Digest
	}
	defer func() {
		var content *runtimeContentError
		if upgrading && err != nil && ctx.Err() == nil && errors.As(err, &content) {
			i.rememberFailure(record, err)
		}
	}()
	data, err := json.Marshal(record)
	if err != nil {
		return Host{}, err
	}
	dest := filepath.Join(i.Dir, record.directory())
	// dropCompanion installs a first runtime without a bundle that failed,
	// the way the official installer leaves a working standard install. An
	// upgrade keeps its working tree instead, and a cancellation stops.
	dropCompanion := func(cause error) bool {
		if upgrading || release.Companion == nil || ctx.Err() != nil {
			return false
		}
		i.reportNote(fmt.Sprintf("the %s for %s could not be added (%v); installing the runtime without it", RuntimeBundleLabel(need.Name), need.Reason, cause))
		release.Companion = nil
		record.Companion, record.CompanionDigest = "", ""
		data, _ = json.Marshal(record)
		dest = filepath.Join(i.Dir, record.directory())
		return true
	}
	// Recover a completed tree if a crash interrupted publication of current.json.
	// Verify the record before reuse; never overwrite an existing runtime tree.
	if _, err := os.Lstat(dest); err == nil {
		if err := i.validateRecord(record); err != nil {
			return Host{}, err
		}
		if err := ctx.Err(); err != nil {
			return Host{}, err
		}
		if err := atomicfile.Write(filepath.Join(i.Dir, i.currentRecordName()), data, 0o600); err != nil {
			return Host{}, err
		}
		i.report("ready", release.Tag, 0, 0)
		return Host{State: HostInstalled, Managed: true, Version: release.Tag, Binary: filepath.Join(dest, platform.Binary)}, nil
	} else if !os.IsNotExist(err) {
		return Host{}, err
	}
	if upgrading {
		if reason, failed := i.failedBefore(record); failed {
			return Host{}, fmt.Errorf("an earlier attempt with %s failed (%s); a new Ollama release retries it", release.Tag, reason)
		}
	}
	needed := release.Asset.Size
	if release.Companion != nil {
		needed += release.Companion.Size
	}
	if err := i.room(needed); err != nil {
		return Host{}, err
	}
	stage, err := os.MkdirTemp(i.Dir, stagingPrefix)
	if err != nil {
		return Host{}, err
	}
	defer func() { _ = os.RemoveAll(stage) }()
	archive := filepath.Join(stage, "bundle")
	if err := i.download(ctx, release.Tag, release.Asset, "", archive); err != nil {
		return Host{}, err
	}
	companionArchive := filepath.Join(stage, "companion-bundle")
	if release.Companion != nil {
		if err := i.download(ctx, release.Tag, *release.Companion, record.Companion, companionArchive); err != nil && !dropCompanion(err) {
			return Host{}, err
		}
	}
	bundle := filepath.Join(stage, "runtime")
	i.report("extracting", release.Tag, 0, 0)
	if err := i.extractStandard(ctx, archive, platform, bundle); err != nil {
		return Host{}, err
	}
	if release.Companion != nil {
		if err := i.addCompanion(ctx, companionArchive, companion.Format, filepath.Join(stage, "companion"), bundle); err != nil {
			if !dropCompanion(err) {
				return Host{}, err
			}
			// A failed merge may have moved part of the bundle in: rebuild
			// the standard tree from its verified archive.
			if err := i.extractStandard(ctx, archive, platform, bundle); err != nil {
				return Host{}, err
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return Host{}, err
	}
	if err := atomicfile.Write(filepath.Join(bundle, ".kolk-runtime.json"), data, 0o600); err != nil {
		return Host{}, err
	}
	// The immutable directory name includes platform + artifact digest. A second
	// project waits on the same installation lock, then reuses the complete record.
	if _, err := os.Lstat(dest); err == nil {
		// A previous attempt may have published the directory and lost the final
		// pointer. Validate it before reuse; never remove or replace running code.
		if err := i.validateRecord(record); err != nil {
			return Host{}, err
		}
	} else if !os.IsNotExist(err) {
		return Host{}, err
	} else {
		if err := os.Rename(bundle, dest); err != nil {
			return Host{}, err
		}
	}
	if err := atomicfile.Write(filepath.Join(i.Dir, i.currentRecordName()), data, 0o600); err != nil {
		return Host{}, err
	}
	i.report("ready", release.Tag, release.Asset.Size, release.Asset.Size)
	return Host{State: HostInstalled, Managed: true, Version: release.Tag, Binary: filepath.Join(dest, platform.Binary)}, nil
}

func (i *RuntimeInstaller) room(bytes int64) error {
	free := i.freeSpace
	if free == nil {
		free = diskspace.Free
	}
	available, known := free(i.Dir)
	if !known {
		return fmt.Errorf("cannot measure free space for Localia at %s", i.Dir)
	}
	if bytes < 0 || uint64(bytes) > available || available-uint64(bytes) < 64<<20 {
		return fmt.Errorf("%w: need %s plus 64 MiB free at %s", errRuntimeNoRoom, HumanBytes(uint64(max(bytes, 0))), i.Dir)
	}
	return nil
}

// extractStandard unpacks the verified standard archive into an empty bundle
// directory, replacing any earlier attempt, and checks its runtime binary.
func (i *RuntimeInstaller) extractStandard(ctx context.Context, archive string, platform runtimePlatform, bundle string) error {
	if err := os.RemoveAll(bundle); err != nil {
		return err
	}
	if err := os.Mkdir(bundle, 0o700); err != nil {
		return err
	}
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	limits := runtimeArchiveLimits
	limits.reserve = i.room
	err = extractRuntimeArchive(ctx, f, platform.Format, bundle, limits)
	if err = errors.Join(err, f.Close()); err != nil {
		return runtimeContentFailure(fmt.Errorf("unpacking native runtime: %w", err))
	}
	if err := validateRuntimeBinary(bundle, platform.Binary); err != nil {
		return &runtimeContentError{err}
	}
	return nil
}

// addCompanion extracts a verified companion archive into its own empty
// staging directory, merges it into the standard bundle, then flushes the
// combined tree again before anything is published.
func (i *RuntimeInstaller) addCompanion(ctx context.Context, archive, format, staging, bundle string) error {
	if err := os.Mkdir(staging, 0o700); err != nil {
		return err
	}
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	limits := runtimeArchiveLimits
	limits.reserve = i.room
	err = extractRuntimeArchive(ctx, f, format, staging, limits)
	if err = errors.Join(err, f.Close()); err != nil {
		return runtimeContentFailure(fmt.Errorf("unpacking runtime companion: %w", err))
	}
	if err := mergeRuntimeTree(bundle, staging, "."); err != nil {
		return err
	}
	root, err := os.OpenRoot(bundle)
	if err != nil {
		return err
	}
	return errors.Join(syncRuntimeDirectories(ctx, root), root.Close())
}

// runtimeContentError is a failure in what was downloaded, not in getting it:
// a digest mismatch, an archive that will not unpack or merge, a bad binary.
// For the same release it fails the same way every time.
type runtimeContentError struct{ err error }

func (e *runtimeContentError) Error() string { return e.err.Error() }
func (e *runtimeContentError) Unwrap() error { return e.err }

// errManagedSetupUnsupported is a platform with no official bundle Kolk
// installs. Its message names the remedy; no retry changes the platform.
var errManagedSetupUnsupported = errors.New("managed Localia setup is unavailable")

// errRuntimeNoRoom is too little disk space, before or during unpacking. The
// user can free space, so it is never remembered as a failure of the release.
var errRuntimeNoRoom = errors.New("not enough disk space for Localia")

// runtimeContentFailure classifies an unpacking error: running out of room is
// the machine's state, anything else is what was downloaded.
func runtimeContentFailure(err error) error {
	if errors.Is(err, errRuntimeNoRoom) || errors.Is(err, syscall.ENOSPC) {
		return err
	}
	return &runtimeContentError{err}
}

// failureRecordName is where a content failure is remembered: by release
// tag and the identity of the tree it would have built (platform, digests
// and companion), so any new release is tried afresh.
func failureRecordName(record runtimeInstallation) string {
	return "failed-" + record.Version + "-" + record.directory() + ".json"
}

func (i *RuntimeInstaller) rememberFailure(record runtimeInstallation, err error) {
	reason := err.Error()
	if len(reason) > 512 {
		reason = reason[:512]
	}
	data, _ := json.Marshal(map[string]string{"error": reason})
	_ = atomicfile.Write(filepath.Join(i.Dir, failureRecordName(record)), data, 0o600)
}

// failedBefore reports a remembered content failure for record's tree.
func (i *RuntimeInstaller) failedBefore(record runtimeInstallation) (string, bool) {
	root, err := os.OpenRoot(i.Dir)
	if err != nil {
		return "", false
	}
	defer func() { _ = root.Close() }()
	return readFailure(root, failureRecordName(record))
}

// readFailure reads one failure marker: a small regular file with a reason.
func readFailure(root *os.Root, name string) (string, bool) {
	info, err := root.Lstat(name)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 4096 {
		return "", false
	}
	data, err := root.ReadFile(name)
	if err != nil {
		return "", false
	}
	var failure struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(data, &failure) != nil || failure.Error == "" {
		return "", false
	}
	return failure.Error, true
}

// rememberedFailure is the newest remembered failure to add companion on this
// platform, from any release; empty when there is none. Read-only, offline.
func (i *RuntimeInstaller) rememberedFailure(companion string) string {
	root, err := os.OpenRoot(i.Dir)
	if err != nil {
		return ""
	}
	defer func() { _ = root.Close() }()
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return ""
	}
	platform := "-" + strings.ReplaceAll(i.platformName(), "/", "-") + "-"
	reason, newest := "", time.Time{}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, "failed-") || !strings.HasSuffix(name, ".json") ||
			!strings.Contains(name, platform) || !strings.Contains(name, "-"+companion+"-") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if why, ok := readFailure(root, name); ok && info.ModTime().After(newest) {
			reason, newest = why, info.ModTime()
		}
	}
	return reason
}

// ForgetFailures clears every remembered failure, so the next upgrade tries
// each release again. An explicit setup calls it: a person asking to set up
// again is the retry a remembered failure otherwise withholds.
func (i *RuntimeInstaller) ForgetFailures(ctx context.Context) error {
	if _, err := os.Lstat(i.Dir); os.IsNotExist(err) {
		return nil
	}
	held, err := lock.Acquire(ctx, filepath.Join(i.Dir, "install.lock"))
	if err != nil {
		return fmt.Errorf("locking native runtime setup: %w", err)
	}
	defer func() { _ = held.Close() }()
	entries, err := os.ReadDir(i.Dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if name := entry.Name(); strings.HasPrefix(name, "failed-") && strings.HasSuffix(name, ".json") {
			if err := os.Remove(filepath.Join(i.Dir, name)); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}
	return nil
}

// mergeRuntimeTree moves a companion's extracted tree into the standard one.
// Directories merge; a file or link already present refuses the companion,
// because an official companion only adds its own accelerator directory.
func mergeRuntimeTree(dst, src, rel string) error {
	entries, err := os.ReadDir(filepath.Join(src, rel))
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := filepath.Join(rel, entry.Name())
		target := filepath.Join(dst, name)
		info, err := os.Lstat(target)
		switch {
		case os.IsNotExist(err):
			if err := os.Rename(filepath.Join(src, name), target); err != nil {
				return err
			}
		case err != nil:
			return err
		case entry.IsDir() && info.IsDir():
			if err := mergeRuntimeTree(dst, src, name); err != nil {
				return err
			}
		default:
			return &runtimeContentError{fmt.Errorf("runtime companion overlaps the standard bundle at %s", filepath.ToSlash(name))}
		}
	}
	return nil
}

func (i *RuntimeInstaller) download(ctx context.Context, tag string, asset runtimeAsset, bundle, dest string) error {
	i.reportBundle("downloading", tag, bundle, 0, asset.Size)
	release := selectedRuntimeRelease{Tag: tag, Asset: asset}
	response, err := i.get(ctx, release.Asset.URL)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.ContentLength >= 0 && response.ContentLength != release.Asset.Size {
		return fmt.Errorf("native runtime download size differs from official metadata")
	}
	f, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	hash := sha256.New()
	p := &runtimeDownloadProgress{installer: i, version: release.Tag, bundle: bundle, total: release.Asset.Size}
	source := &archiveStream{ctx: ctx, r: fetchReader{response.Body}, remaining: release.Asset.Size, label: "runtime download"}
	n, err := io.Copy(io.MultiWriter(f, hash, p), source)
	if err != nil {
		return fmt.Errorf("downloading native runtime: %w", err)
	}
	if n != release.Asset.Size {
		return &runtimeFetchError{fmt.Errorf("native runtime download is incomplete (%d of %d bytes)", n, release.Asset.Size)}
	}
	if got := fmt.Sprintf("%x", hash.Sum(nil)); got != release.Asset.Digest {
		return &runtimeContentError{fmt.Errorf("native runtime SHA-256 verification failed; retry setup")}
	}
	if err := f.Sync(); err != nil {
		return err
	}
	return f.Close()
}

type runtimeDownloadProgress struct {
	installer        *RuntimeInstaller
	version, bundle  string
	completed, total int64
	last             time.Time
}

func (p *runtimeDownloadProgress) Write(b []byte) (int, error) {
	p.completed += int64(len(b))
	if p.completed == p.total || time.Since(p.last) >= time.Second {
		p.installer.reportBundle("downloading", p.version, p.bundle, p.completed, p.total)
		p.last = time.Now()
	}
	return len(b), nil
}

type runtimeInstallation struct {
	Version  string `json:"version"`
	Platform string `json:"platform"`
	Digest   string `json:"sha256"`
	Binary   string `json:"binary"`
	// Companion is the official accelerator bundle merged into this tree, by
	// detection name, with its archive digest; both empty for none.
	Companion       string `json:"companion,omitempty"`
	CompanionDigest string `json:"companion_sha256,omitempty"`
}

// directory is the tree's immutable identity: a companion makes a different
// tree from the standard-only one, so adding one never touches a tree in use.
func (r runtimeInstallation) directory() string {
	name := strings.ReplaceAll(r.Platform, "/", "-") + "-" + r.Digest
	if r.Companion != "" {
		name += "-" + r.Companion + "-" + r.CompanionDigest
	}
	return name
}
