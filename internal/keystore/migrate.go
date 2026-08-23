package keystore

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/onembyte/kolkrabbi/internal/atomicfile"
	"github.com/onembyte/kolkrabbi/internal/lock"
	"github.com/onembyte/kolkrabbi/internal/secret"
)

// MigrateLegacyConfig copies the prototype's config.json api_key into
// openrouter/default, then atomically rewrites config without that field.
//
// The order is deliberate: a failed manifest write leaves the only copy in
// config, while a failed config rewrite leaves two copies that a later run can
// safely recognize and finish. A different manifest value is never guessed
// over or overwritten.
func (s *FileStore) MigrateLegacyConfig(ctx context.Context, configPath string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	body, writePath, err := readLegacyConfig(configPath)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	var object map[string]json.RawMessage
	if err := json.Unmarshal(body, &object); err != nil {
		return false, fmt.Errorf("%s is not valid legacy config JSON: %w", configPath, err)
	}
	rawKey, present := object["api_key"]
	if !present {
		return false, nil
	}
	delete(object, "api_key")
	rewritten, err := json.MarshalIndent(object, "", "  ")
	if err != nil {
		return false, fmt.Errorf("encoding credential-free config %s: %w", configPath, err)
	}
	rewritten = append(rewritten, '\n')

	var legacyRaw string
	if string(rawKey) != "null" {
		if err := json.Unmarshal(rawKey, &legacyRaw); err != nil {
			return false, fmt.Errorf("%s has a non-string api_key; left it unchanged", configPath)
		}
	}
	legacy := secret.New(legacyRaw)
	if legacy.IsZero() {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		if err := atomicfile.Write(writePath, rewritten, 0o600); err != nil {
			return false, fmt.Errorf("removing empty api_key from %s: %w", configPath, err)
		}
		return true, nil
	}

	ref := Ref{Provider: "openrouter", Profile: "default"}
	row, err := s.newDiskEntry(legacy, WriteMetadata{Source: "legacy-config"})
	if err != nil {
		return false, fmt.Errorf("legacy api_key in %s cannot be migrated: %w", configPath, err)
	}
	if err := s.ensureDir(); err != nil {
		return false, err
	}
	held, err := lock.Acquire(ctx, s.Path+".lock")
	if err != nil {
		return false, err
	}
	defer func() { _ = held.Close() }()

	manifest, err := s.load()
	if err != nil {
		return false, err
	}
	if current, ok := manifest.Credentials[ref.String()]; ok {
		if current.Backend != BackendFile {
			return false, fmt.Errorf("credential backend for %s: %w", ref, ErrUnavailable)
		}
		value, decodeErr := decodeValue(current.Value)
		if decodeErr != nil {
			return false, decodeErr
		}
		if !sameCredential(value, legacy) {
			return false, fmt.Errorf(
				"%s already has a different credential; left %s unchanged: %w",
				ref, configPath, ErrMigrationConflict,
			)
		}
	} else {
		manifest.Credentials[ref.String()] = row
		if err := s.save(manifest); err != nil {
			return false, err
		}
	}

	if err := ctx.Err(); err != nil {
		return false, err
	}
	if err := atomicfile.Write(writePath, rewritten, 0o600); err != nil {
		return false, fmt.Errorf(
			"credential was copied to %s but removing api_key from %s failed: %w",
			s.Path, configPath, err,
		)
	}
	return true, nil
}

func readLegacyConfig(path string) ([]byte, string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, "", err
	}
	writePath := path
	if info.Mode()&os.ModeSymlink != 0 {
		writePath, err = filepath.EvalSymlinks(path)
		if err != nil {
			return nil, "", fmt.Errorf("resolving legacy config symlink %s: %w", path, err)
		}
		info, err = os.Stat(writePath)
		if err != nil {
			return nil, "", err
		}
	}
	if !info.Mode().IsRegular() {
		return nil, "", fmt.Errorf("legacy config %s is not a regular file", path)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, "", fmt.Errorf("reading legacy config %s: %w", path, err)
	}
	return body, writePath, nil
}

func sameCredential(a, b secret.Secret) bool {
	left, right := []byte(a.Reveal()), []byte(b.Reveal())
	return len(left) == len(right) && subtle.ConstantTimeCompare(left, right) == 1
}
