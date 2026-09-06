package keystore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/onembyte/kolkrabbi/internal/redact"
	"github.com/onembyte/kolkrabbi/internal/secret"
)

// BackendKeychain is the macOS login keychain, opt-in, per credential.
const BackendKeychain Backend = "keychain"

// Spawner is the one way this package reaches a process. os/exec belongs to
// the shell package, which supplies the implementation: a fixed argv, a
// cleared environment, a real stdin pipe, its own session so nothing inside
// can reach the terminal, and the caller's deadline. Run returns the exit
// code when the process ran, and an error only when it could not run or the
// context ended first.
type Spawner interface {
	LookPath(name string) (string, error)
	Run(ctx context.Context, argv []string, stdin string) (stdout []byte, exit int, err error)
}

// keychainLineLimit guards the assembled `security -i` line: the tool splits
// a line at 4096 bytes and parses the tail as a command, so kolk stops well
// short of it (plan 05 §3.6 rule 4).
const keychainLineLimit = 3800

// KeychainStore keeps one credential per manifest row in the macOS login
// keychain through /usr/bin/security, exactly as plan 05 §3.6 says: the
// whole write command on stdin, never a value in argv; the keychain path as
// the mandatory trailing positional, resolved once per call; the tagged
// base64 as the value; exit codes, never messages; a deadline on every call;
// a write proven by reading it back before the manifest row is written.
type KeychainStore struct {
	Manifest *FileStore
	Spawn    Spawner
	// Deadline bounds every security call; 2 s by plan, since 60 s is a hang.
	Deadline time.Duration
	// LineLimit is the guard on the assembled -i line, 3800 by plan.
	LineLimit int
	service   string
}

// NewKeychainStore routes rows through the manifest and processes through the
// spawner.
func NewKeychainStore(manifest *FileStore, spawn Spawner) *KeychainStore {
	return &KeychainStore{Manifest: manifest, Spawn: spawn, Deadline: 2 * time.Second, LineLimit: keychainLineLimit, service: "kolk"}
}

func (s *KeychainStore) Name() Backend { return BackendKeychain }

// Available is a write-time hint and nothing else: whether a `security`
// binary is on the path. Availability itself is an outcome of a call.
func (s *KeychainStore) Available(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := s.Spawn.LookPath("security"); err != nil {
		return fmt.Errorf("no security binary on this machine: %w", ErrUnavailable)
	}
	return nil
}

func (s *KeychainStore) run(ctx context.Context, argv []string, stdin string) ([]byte, int, error) {
	ctx, cancel := context.WithTimeout(ctx, s.Deadline)
	defer cancel()
	path, err := s.Spawn.LookPath("security")
	if err != nil {
		return nil, -1, fmt.Errorf("security: %w", ErrUnavailable)
	}
	out, code, err := s.Spawn.Run(ctx, append([]string{path}, argv...), stdin)
	if errors.Is(err, context.DeadlineExceeded) || (err != nil && ctx.Err() != nil) {
		return nil, -1, fmt.Errorf("security %s did not answer within %s: %w", firstWord(argv, stdin), s.Deadline, ErrTimeout)
	}
	if err != nil {
		return nil, -1, fmt.Errorf("security could not run: %w", ErrUnavailable)
	}
	return out, code, nil
}

func firstWord(argv []string, stdin string) string {
	if len(argv) > 0 && argv[0] != "-q" {
		return argv[0]
	}
	if f := strings.Fields(stdin); len(f) > 0 {
		return f[0]
	}
	return ""
}

// loginKeychain resolves the login keychain's path, the trailing positional
// every call must carry so nothing is written into whichever keychain
// happened to be default.
func (s *KeychainStore) loginKeychain(ctx context.Context) (string, error) {
	out, code, err := s.run(ctx, []string{"login-keychain"}, "")
	if err != nil {
		return "", err
	}
	if code != 0 {
		return "", fmt.Errorf("security login-keychain exited %d: %w", code, ErrUnavailable)
	}
	path := strings.Trim(strings.TrimSpace(string(out)), `"`)
	if path == "" {
		return "", fmt.Errorf("security login-keychain named no keychain: %w", ErrUnavailable)
	}
	return path, nil
}

// statusError maps an exit code to the outcome plan 05 §3.6 rule 5 gives.
// 44 is "not found" and also "keychain file missing" and "locked and not the
// default" — indistinguishable — so it is ErrNotFound only when the manifest
// says nothing should be there, and ErrUnavailable otherwise.
func statusError(code int, expected bool, op string) error {
	switch code {
	case 0:
		return nil
	case 36, 128:
		return fmt.Errorf("security %s: the login keychain is locked and cannot prompt here (exit %d): %w", op, code, ErrLocked)
	case 44:
		if expected {
			return fmt.Errorf("security %s: the item the manifest records is not readable — keychain missing, locked and not the default, or the item gone (exit 44): %w", op, ErrUnavailable)
		}
		return fmt.Errorf("security %s: %w", op, ErrNotFound)
	case 45:
		return fmt.Errorf("security %s: duplicate item (exit 45): %w", op, ErrUnavailable)
	case 53:
		return fmt.Errorf("security %s: keychain not available (exit 53): %w", op, ErrUnavailable)
	}
	return fmt.Errorf("security %s exited %d: %w", op, code, ErrUnavailable)
}

func (s *KeychainStore) account(ref Ref) string { return ref.String() }

func (s *KeychainStore) read(ctx context.Context, ref Ref, keychain string, expected bool) (secret.Secret, error) {
	argv := []string{"find-generic-password", "-s", s.service, "-a", s.account(ref), "-w", keychain}
	out, code, err := s.run(ctx, argv, "")
	if err != nil {
		return secret.Secret{}, err
	}
	if err := statusError(code, expected, "find-generic-password"); err != nil {
		return secret.Secret{}, err
	}
	return decodeValue(strings.TrimRight(string(out), "\r\n"))
}

// Get reads the value for a row the manifest routes here. A row routed
// elsewhere or absent is ErrNotFound; the keychain's own refusals are named.
func (s *KeychainStore) Get(ctx context.Context, ref Ref) (secret.Secret, error) {
	ref, err := canonicalRef(ref)
	if err != nil {
		return secret.Secret{}, err
	}
	entry, err := s.Manifest.Probe(ctx, ref)
	if err != nil {
		return secret.Secret{}, err
	}
	if entry.Backend != BackendKeychain {
		return secret.Secret{}, fmt.Errorf("%s is kept in %s, not the keychain: %w", ref, entry.Backend, ErrNotFound)
	}
	keychain := entry.Note
	if keychain == "" {
		if keychain, err = s.loginKeychain(ctx); err != nil {
			return secret.Secret{}, err
		}
	}
	return s.read(ctx, ref, keychain, true)
}

// Set writes the value, proves it by reading it back, and only then records
// the route in the manifest (rule 6: backend first, manifest last).
func (s *KeychainStore) Set(ctx context.Context, ref Ref, value secret.Secret) error {
	return s.SetWithMetadata(ctx, ref, value, WriteMetadata{})
}

// SetWithMetadata is Set with the manifest's metadata.
func (s *KeychainStore) SetWithMetadata(ctx context.Context, ref Ref, value secret.Secret, meta WriteMetadata) error {
	ref, err := canonicalRef(ref)
	if err != nil {
		return err
	}
	encoded, err := encodeValue(value)
	if err != nil {
		return err
	}
	keychain, err := s.loginKeychain(ctx)
	if err != nil {
		return err
	}
	line := fmt.Sprintf("add-generic-password -U -s %s -a %q -D %q -w %q %q", s.service, s.account(ref), "kolk credential", encoded, keychain)
	if len(line) >= s.LineLimit {
		return fmt.Errorf("the keychain command would be %d bytes, past the %d-byte line security -i can take: %w", len(line), s.LineLimit, ErrTooLarge)
	}
	_, code, err := s.run(ctx, []string{"-q", "-i"}, line+"\n")
	if err != nil {
		return err
	}
	if err := statusError(code, false, "add-generic-password"); err != nil {
		return err
	}
	back, err := s.read(ctx, ref, keychain, true)
	if err != nil {
		return fmt.Errorf("the keychain write could not be read back, so it is not trusted: %w", err)
	}
	if back.Reveal() != value.Reveal() {
		return fmt.Errorf("the keychain read back a different value than was written; the write is not trusted: %w", ErrCorrupt)
	}
	return s.Manifest.SetRouted(ctx, ref, BackendKeychain, redact.Mask(value.Reveal()), hashValue(value), keychain, meta)
}

// Del removes the item and then the row. `security delete-generic-password`
// says "password has been deleted." on stderr with exit 0; only the exit
// code is read.
func (s *KeychainStore) Del(ctx context.Context, ref Ref) error {
	ref, err := canonicalRef(ref)
	if err != nil {
		return err
	}
	entry, err := s.Manifest.Probe(ctx, ref)
	if err != nil {
		return err
	}
	if entry.Backend != BackendKeychain {
		return fmt.Errorf("%s is kept in %s, not the keychain: %w", ref, entry.Backend, ErrNotFound)
	}
	keychain := entry.Note
	if keychain == "" {
		if keychain, err = s.loginKeychain(ctx); err != nil {
			return err
		}
	}
	_, code, err := s.run(ctx, []string{"delete-generic-password", "-s", s.service, "-a", s.account(ref), keychain}, "")
	if err != nil {
		return err
	}
	if err := statusError(code, true, "delete-generic-password"); err != nil {
		return err
	}
	return s.Manifest.removeRouted(ctx, ref, BackendKeychain)
}

// Probe reads the manifest row and, when the row is here, asks the keychain
// for the item's attributes only — no -w, no -g, no -d — which cannot prompt.
func (s *KeychainStore) Probe(ctx context.Context, ref Ref) (Entry, error) {
	entry, err := s.Manifest.Probe(ctx, ref)
	if err != nil {
		return Entry{}, err
	}
	if entry.Backend != BackendKeychain {
		return entry, nil
	}
	keychain := entry.Note
	if keychain != "" {
		if _, code, err := s.run(ctx, []string{"find-generic-password", "-s", s.service, "-a", s.account(ref), keychain}, ""); err == nil && code == 44 {
			entry.Note += " (item missing: an orphaned row; re-add the key)"
		}
	}
	return entry, nil
}

// List is the manifest's list: the rows routed here, attributes only.
func (s *KeychainStore) List(ctx context.Context) ([]Entry, error) {
	all, err := s.Manifest.List(ctx)
	if err != nil {
		return nil, err
	}
	var out []Entry
	for _, e := range all {
		if e.Backend == BackendKeychain {
			out = append(out, e)
		}
	}
	return out, nil
}

// DeleteItem removes the keychain item alone, for a migration whose manifest
// row already points elsewhere; the keychain path comes from the login
// keychain since the row no longer records it. A missing item is not an
// error: there is nothing to orphan.
func (s *KeychainStore) DeleteItem(ctx context.Context, ref Ref) error {
	ref, err := canonicalRef(ref)
	if err != nil {
		return err
	}
	keychain, err := s.loginKeychain(ctx)
	if err != nil {
		return err
	}
	_, code, err := s.run(ctx, []string{"delete-generic-password", "-s", s.service, "-a", s.account(ref), keychain}, "")
	if err != nil {
		return err
	}
	if code == 44 {
		return nil
	}
	return statusError(code, true, "delete-generic-password")
}
