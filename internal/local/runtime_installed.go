package local

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Find reads a completed installation. It never creates storage, downloads,
// starts a server, or changes a cached model. Invalid records are surfaced so
// setup cannot silently overwrite a directory a running process may use.
func (i *RuntimeInstaller) Find() (Host, error) {
	host, record, err := i.current()
	if err != nil {
		return host, err
	}
	if _, ok := runtimePlatforms[i.platformName()]; !ok {
		return host, nil
	}
	// Reading the machine is as read-only as reading the record: it names
	// what setup would add, and adds nothing.
	need := i.companionNeed()
	if need.Name != "" && record.Companion != need.Name {
		host.requiredCompanion = RuntimeBundleLabel(need.Name) + " for " + need.Reason
		host.requiredFailure = i.rememberedFailure(need.Name)
		host.requiredVendor = companionVendors[need.Name]
	}
	switch {
	case need.Name == "" || record.Companion == need.Name:
	case i.CPUOnly:
		// CPU chosen: the bundle is not wanted, so nothing is missing. The
		// card it would serve stays unserved by this tree, which fit plans
		// and the picker still have to know.
		host.UnservedVendor = companionVendors[need.Name]
	default:
		host.MissingCompanion, host.CompanionFailure, host.CompanionVendor = host.companionFacts()
	}
	if !i.CPUOnly {
		host.AcceleratorNote = need.Note
	}
	if host.UnservedVendor == "" {
		host.UnservedVendor = need.Unserved
	}
	return host, nil
}

// current is Find with the validated record behind the installed host.
func (i *RuntimeInstaller) current() (Host, runtimeInstallation, error) {
	if _, ok := runtimePlatforms[i.platformName()]; !ok {
		return Host{}, runtimeInstallation{}, nil
	}
	if !filepath.IsAbs(i.Dir) {
		return Host{}, runtimeInstallation{}, fmt.Errorf("managed runtime storage must be an absolute path")
	}
	info, err := os.Lstat(i.Dir)
	if os.IsNotExist(err) {
		return Host{}, runtimeInstallation{}, nil
	}
	if err != nil {
		return Host{}, runtimeInstallation{}, err
	}
	if !info.IsDir() {
		return Host{}, runtimeInstallation{}, fmt.Errorf("managed runtime storage is not a directory")
	}
	root, err := os.OpenRoot(i.Dir)
	if err != nil {
		return Host{}, runtimeInstallation{}, err
	}
	defer func() { _ = root.Close() }()
	record, err := readRuntimeRecord(root, i.currentRecordName())
	if os.IsNotExist(err) {
		return Host{}, runtimeInstallation{}, nil
	}
	if err != nil {
		return Host{}, runtimeInstallation{}, fmt.Errorf("reading managed runtime installation: %w", err)
	}
	if err := i.validateRecord(record); err != nil {
		return Host{}, runtimeInstallation{}, err
	}
	return Host{State: HostInstalled, Managed: true, Version: record.Version, Binary: filepath.Join(i.Dir, record.directory(), record.Binary)}, record, nil
}

func readRuntimeRecord(root *os.Root, name string) (runtimeInstallation, error) {
	info, err := root.Lstat(name)
	if err != nil {
		return runtimeInstallation{}, err
	}
	if !info.Mode().IsRegular() || info.Size() > 16384 {
		return runtimeInstallation{}, fmt.Errorf("invalid runtime installation record %q", name)
	}
	f, err := root.Open(name)
	if err != nil {
		return runtimeInstallation{}, err
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, 16385))
	if err != nil {
		return runtimeInstallation{}, err
	}
	if len(data) > 16384 {
		return runtimeInstallation{}, fmt.Errorf("runtime installation record exceeds size limit")
	}
	var record runtimeInstallation
	if err := json.Unmarshal(data, &record); err != nil {
		return runtimeInstallation{}, err
	}
	return record, nil
}

func (i *RuntimeInstaller) validateRecord(record runtimeInstallation) error {
	platform, ok := runtimePlatforms[record.Platform]
	digest, err := hex.DecodeString(record.Digest)
	if !ok || record.Platform != i.platformName() || !validRuntimeTag(record.Version) || err != nil || len(digest) != 32 || record.Digest != strings.ToLower(record.Digest) || record.Binary != platform.Binary {
		return fmt.Errorf("invalid managed runtime installation metadata")
	}
	// A companion is named only by an official one for this platform, with
	// its own archive digest; the two fields are both set or both empty.
	if record.Companion != "" || record.CompanionDigest != "" {
		_, official := runtimeCompanions[record.Platform][record.Companion]
		companionDigest, err := hex.DecodeString(record.CompanionDigest)
		if !official || err != nil || len(companionDigest) != 32 || record.CompanionDigest != strings.ToLower(record.CompanionDigest) {
			return fmt.Errorf("invalid managed runtime companion metadata")
		}
	}
	dir := filepath.Join(i.Dir, record.directory())
	info, err := os.Lstat(dir)
	if err != nil {
		return fmt.Errorf("managed runtime installation is incomplete: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("managed runtime installation is not a directory")
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	installed, err := readRuntimeRecord(root, ".kolk-runtime.json")
	if err != nil || installed != record {
		return fmt.Errorf("managed runtime completion record is missing or inconsistent")
	}
	return validateRuntimeBinary(dir, record.Binary)
}

func validateRuntimeBinary(dir, name string) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	info, err := root.Lstat(name)
	if err != nil {
		return fmt.Errorf("native runtime bundle is missing %s: %w", name, err)
	}
	if !info.Mode().IsRegular() || info.Size() == 0 || info.Mode().Perm()&0o100 == 0 {
		return fmt.Errorf("native runtime bundle has no executable regular %s", name)
	}
	return nil
}
