package keystore

import (
	"context"
	"fmt"

	"github.com/onembyte/kolkrabbi/internal/secret"
)

// Routed is the store the chain reads: the manifest says which backend holds
// a credential, and exactly that backend is asked — a lookup, never a
// cascade (plan 05 §1.1). A row routed to a backend this machine has no
// implementation for is unavailable by name, not "no key".
type Routed struct {
	Manifest *FileStore
	Backends map[Backend]Store
}

// NewRouted routes through the manifest to the file store and whatever
// other backends are supplied.
func NewRouted(manifest *FileStore, others ...Store) *Routed {
	r := &Routed{Manifest: manifest, Backends: map[Backend]Store{BackendFile: manifest}}
	for _, s := range others {
		if s != nil {
			r.Backends[s.Name()] = s
		}
	}
	return r
}

func (r *Routed) Name() Backend { return "routed" }

func (r *Routed) Available(ctx context.Context) error { return r.Manifest.Available(ctx) }

// backendFor is the one decision: one manifest read, one backend named.
func (r *Routed) backendFor(ctx context.Context, ref Ref) (Store, Entry, error) {
	entry, err := r.Manifest.Probe(ctx, ref)
	if err != nil {
		return nil, Entry{}, err
	}
	store, ok := r.Backends[entry.Backend]
	if !ok {
		return nil, entry, fmt.Errorf("%s is kept in %s, which this machine cannot open: %w", ref, entry.Backend, ErrUnavailable)
	}
	return store, entry, nil
}

func (r *Routed) Get(ctx context.Context, ref Ref) (secret.Secret, error) {
	store, _, err := r.backendFor(ctx, ref)
	if err != nil {
		return secret.Secret{}, err
	}
	return store.Get(ctx, ref)
}

// Set writes to the file store; another backend is chosen explicitly through
// its own store (kolk key --backend), never by a read-time cascade.
func (r *Routed) Set(ctx context.Context, ref Ref, value secret.Secret) error {
	return r.Manifest.Set(ctx, ref, value)
}

func (r *Routed) Del(ctx context.Context, ref Ref) error {
	store, _, err := r.backendFor(ctx, ref)
	if err != nil {
		return err
	}
	return store.Del(ctx, ref)
}

func (r *Routed) Probe(ctx context.Context, ref Ref) (Entry, error) {
	store, entry, err := r.backendFor(ctx, ref)
	if err != nil {
		return entry, err
	}
	return store.Probe(ctx, ref)
}

func (r *Routed) List(ctx context.Context) ([]Entry, error) { return r.Manifest.List(ctx) }
