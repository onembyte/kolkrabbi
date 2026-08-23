// Package keystore is the only package that persists provider credentials.
// It routes a typed provider/profile reference to one named backend and never
// exposes plaintext through metadata operations.
package keystore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/onembyte/kolkrabbi/internal/secret"
)

const (
	manifestVersion = 1
	// MaxValueBytes is the tightest portable credential limit (Windows' blob
	// limit), applied on every platform so a credential remains movable later.
	MaxValueBytes = 2560
)

var (
	ErrNotFound          = errors.New("keystore: credential not found")
	ErrCorrupt           = errors.New("keystore: credential manifest is unreadable")
	ErrVersion           = errors.New("keystore: unsupported manifest version")
	ErrUnavailable       = errors.New("keystore: credential backend is unavailable")
	ErrTooLarge          = errors.New("keystore: credential exceeds the portable size limit")
	ErrInvalidRef        = errors.New("keystore: invalid credential reference")
	ErrEmpty             = errors.New("keystore: refusing an empty credential")
	ErrMigrationConflict = errors.New("keystore: legacy credential conflicts with the current manifest")
)

// Ref names one credential slot and is always safe to print.
type Ref struct {
	Provider string
	Profile  string
}

func (r Ref) String() string {
	provider := strings.ToLower(strings.TrimSpace(r.Provider))
	profile := strings.ToLower(strings.TrimSpace(r.Profile))
	if profile == "" {
		profile = "default"
	}
	return provider + "/" + profile
}

func canonicalRef(r Ref) (Ref, error) {
	r = Ref{
		Provider: strings.ToLower(strings.TrimSpace(r.Provider)),
		Profile:  strings.ToLower(strings.TrimSpace(r.Profile)),
	}
	if r.Profile == "" {
		r.Profile = "default"
	}
	if !validPart(r.Provider) || !validPart(r.Profile) {
		return Ref{}, fmt.Errorf("%q: %w", r.String(), ErrInvalidRef)
	}
	return r, nil
}

// NormalizeRef validates and canonicalizes a caller-supplied provider/profile
// before any backend or network operation uses it.
func NormalizeRef(r Ref) (Ref, error) { return canonicalRef(r) }

func parseRef(s string) (Ref, error) {
	provider, profile, ok := strings.Cut(s, "/")
	if !ok || strings.Contains(profile, "/") {
		return Ref{}, fmt.Errorf("%q: %w", s, ErrInvalidRef)
	}
	return canonicalRef(Ref{Provider: provider, Profile: profile})
}

func validPart(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for i, r := range s {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || i > 0 && (r == '-' || r == '_' || r == '.') {
			continue
		}
		return false
	}
	return true
}

type Backend string

const BackendFile Backend = "file"

// Store is the complete credential persistence capability. Callers that only
// need status use Probe or List and therefore cannot obtain a plaintext value.
type Store interface {
	Name() Backend
	Available(context.Context) error
	Get(context.Context, Ref) (secret.Secret, error)
	Set(context.Context, Ref, secret.Secret) error
	Del(context.Context, Ref) error
	Probe(context.Context, Ref) (Entry, error)
	List(context.Context) ([]Entry, error)
}

// WriteMetadata records safe facts known by the command that acquired a
// credential. It deliberately contains no value-shaped field.
type WriteMetadata struct {
	Verified time.Time
	Source   string
	Note     string
}

// Entry is metadata only. There is deliberately no field in which plaintext
// can live, so List and Probe are structurally unable to return a credential.
type Entry struct {
	Ref      Ref
	Backend  Backend
	Helper   string
	Mask     string
	KeyHash  string
	Machine  string
	Created  time.Time
	Verified time.Time
	Source   string
	Note     string
}
