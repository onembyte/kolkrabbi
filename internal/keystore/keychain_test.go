package keystore

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/secret"
)

// fakeSecurity plays /usr/bin/security by the exit-code table plan 05 §3.6
// gives, recording every argv and stdin line so the shape can be pinned.
type fakeSecurity struct {
	calls  [][]string
	stdins []string
	items  map[string]string // account → stored -w value
	exit   map[string]int    // subcommand → forced exit code
	slow   bool
}

func (f *fakeSecurity) LookPath(name string) (string, error) { return "/usr/bin/" + name, nil }

func (f *fakeSecurity) Run(ctx context.Context, argv []string, stdin string) ([]byte, int, error) {
	f.calls = append(f.calls, argv)
	f.stdins = append(f.stdins, stdin)
	if f.slow {
		<-ctx.Done()
		return nil, -1, ctx.Err()
	}
	if f.items == nil {
		f.items = map[string]string{}
	}
	sub := ""
	if len(argv) > 1 && argv[1] != "-q" {
		sub = argv[1]
	} else if stdin != "" {
		sub = strings.Fields(stdin)[0]
	}
	if code, forced := f.exit[sub]; forced {
		return nil, code, nil
	}
	account := func(args []string) string {
		for i, a := range args {
			if a == "-a" && i+1 < len(args) {
				return strings.Trim(args[i+1], `"`)
			}
		}
		return ""
	}
	switch sub {
	case "login-keychain":
		return []byte("    \"/Users/x/Library/Keychains/login.keychain-db\"\n"), 0, nil
	case "add-generic-password":
		fields := strings.Fields(stdin)
		acct := account(fields)
		for i, a := range fields {
			if a == "-w" && i+1 < len(fields) {
				f.items[acct] = strings.Trim(fields[i+1], `"`)
			}
		}
		return nil, 0, nil
	case "find-generic-password":
		v, ok := f.items[account(argv)]
		if !ok {
			return nil, 44, nil
		}
		for _, a := range argv {
			if a == "-w" {
				return []byte(v + "\n"), 0, nil
			}
		}
		return []byte("keychain: ...\nclass: \"genp\"\n"), 0, nil
	case "delete-generic-password":
		delete(f.items, account(argv))
		return []byte("password has been deleted.\n"), 0, nil
	}
	return nil, 1, nil
}

func keychainUnderTest(t *testing.T, spawn Spawner) (*KeychainStore, *FileStore) {
	t.Helper()
	manifest := NewFileStore(filepath.Join(t.TempDir(), "credentials.json"))
	return NewKeychainStore(manifest, spawn), manifest
}

// The write: the whole command travels on stdin to `security -q -i`, never a
// value in argv; the keychain path is the mandatory trailing positional,
// resolved once through `security login-keychain`; the value is the tagged
// base64; the write is proven by reading it back; and the manifest records
// the route with no value.
func TestKeychainWritesThroughStdinAndProvesItByReadingBack(t *testing.T) {
	sec := &fakeSecurity{}
	store, manifest := keychainUnderTest(t, sec)
	ref := Ref{Provider: "openrouter", Profile: "default"}
	key := secret.New("sk-or-v1-" + strings.Repeat("k", 24))
	if err := store.Set(context.Background(), ref, key); err != nil {
		t.Fatal(err)
	}
	var addCall []string
	var addLine string
	for i, call := range sec.calls {
		if strings.Contains(sec.stdins[i], "add-generic-password") {
			addCall, addLine = call, sec.stdins[i]
		}
	}
	if addLine == "" || strings.Join(addCall, " ") != "/usr/bin/security -q -i" {
		t.Fatalf("add ran as %v with stdin %q; want `security -q -i` with the command on stdin", addCall, addLine)
	}
	for _, want := range []string{"add-generic-password -U -s kolk -a \"openrouter/default\" -D \"kolk credential\" -w \"kolk-b64:", "\"/Users/x/Library/Keychains/login.keychain-db\""} {
		if !strings.Contains(addLine, want) {
			t.Fatalf("stdin line %q lacks %q", addLine, want)
		}
	}
	if strings.Contains(addLine, key.Reveal()) {
		t.Fatal("the raw value travelled; only the tagged base64 may")
	}
	if !strings.HasSuffix(strings.TrimSpace(addLine), `login.keychain-db"`) {
		t.Fatalf("the keychain path is not the trailing positional: %q", addLine)
	}
	readBacks := 0
	for _, call := range sec.calls {
		if len(call) > 1 && call[1] == "find-generic-password" {
			readBacks++
			if strings.HasSuffix(strings.Join(call, " "), "-w") {
				t.Fatalf("a value-taking flag is last in %v", call)
			}
		}
	}
	if readBacks != 1 {
		t.Fatalf("read-backs = %d, want the write proven exactly once", readBacks)
	}
	entry, err := manifest.Probe(context.Background(), ref)
	if err != nil || entry.Backend != BackendKeychain || entry.Mask == "" || entry.KeyHash == "" {
		t.Fatalf("manifest row = %+v, %v; want the keychain route with mask and hash", entry, err)
	}
	if _, err := manifest.Get(context.Background(), ref); err == nil {
		t.Fatal("the file store handed back a value for a keychain-routed row")
	}
	got, err := store.Get(context.Background(), ref)
	if err != nil || got.Reveal() != key.Reveal() {
		t.Fatalf("Get = %v, %v", got, err)
	}
}

// The exit-code table, never the message: 36 and 128 are locked, 53 is
// unavailable, 44 on a row the manifest says exists is unavailable (the file
// may be missing or the keychain locked and non-default), a deadline is a
// timeout; and a delete that says "password has been deleted." on stderr
// with exit 0 is a success.
func TestKeychainBranchesOnExitCodesNotMessages(t *testing.T) {
	ref := Ref{Provider: "openrouter", Profile: "default"}
	for code, want := range map[int]error{36: ErrLocked, 128: ErrLocked, 53: ErrUnavailable, 44: ErrUnavailable, 51: ErrUnavailable} {
		sec := &fakeSecurity{items: map[string]string{"openrouter/default": "kolk-b64:c2s="}}
		store, manifest := keychainUnderTest(t, sec)
		if err := manifest.SetRouted(context.Background(), ref, BackendKeychain, "sk-…", "hash", "login.keychain-db", WriteMetadata{Source: "test"}); err != nil {
			t.Fatal(err)
		}
		sec.exit = map[string]int{"find-generic-password": code}
		_, err := store.Get(context.Background(), ref)
		if !errors.Is(err, want) {
			t.Fatalf("exit %d → %v, want %v", code, err, want)
		}
	}
	sec := &fakeSecurity{slow: true}
	store, manifest := keychainUnderTest(t, sec)
	store.Deadline = 20 * time.Millisecond
	_ = manifest.SetRouted(context.Background(), ref, BackendKeychain, "sk-…", "hash", "login.keychain-db", WriteMetadata{})
	if _, err := store.Get(context.Background(), ref); !errors.Is(err, ErrTimeout) {
		t.Fatalf("a slow security = %v, want ErrTimeout", err)
	}
	sec = &fakeSecurity{}
	store, manifest = keychainUnderTest(t, sec)
	if err := store.Set(context.Background(), ref, secret.New("sk-or-v1-"+strings.Repeat("d", 24))); err != nil {
		t.Fatal(err)
	}
	if err := store.Del(context.Background(), ref); err != nil {
		t.Fatalf("delete with the stderr sentence and exit 0 = %v, want success", err)
	}
	if _, err := manifest.Probe(context.Background(), ref); !errors.Is(err, ErrNotFound) {
		t.Fatal("the manifest row outlived the delete")
	}
}

// A line at or past 3800 bytes is refused before it reaches `security -i`,
// which splits at 4096 and runs the tail as a bogus command; and a write the
// read-back cannot confirm is an error, not a success.
func TestKeychainGuardsTheLineAndTheReadBack(t *testing.T) {
	ref := Ref{Provider: "openrouter", Profile: "default"}
	sec := &fakeSecurity{}
	store, _ := keychainUnderTest(t, sec)
	// The largest portable value fits under the guard by design (2560 raw →
	// ~3.4 KB tagged base64 plus the command); the guard itself is proven
	// with a lower limit.
	if err := store.Set(context.Background(), ref, secret.New(strings.Repeat("x", MaxValueBytes))); err != nil {
		t.Fatalf("the largest portable value was refused: %v", err)
	}
	store.LineLimit = 100
	if err := store.Set(context.Background(), ref, secret.New("sk-or-v1-"+strings.Repeat("y", 24))); err == nil || !errors.Is(err, ErrTooLarge) {
		t.Fatalf("a line past the limit = %v, want ErrTooLarge before security runs", err)
	}
	lying := &fakeSecurity{}
	lying.exit = map[string]int{}
	store2, _ := keychainUnderTest(t, lying)
	lying.items = map[string]string{}
	// The add "succeeds" but the read-back finds nothing.
	lying.exit["find-generic-password"] = 44
	err := store2.Set(context.Background(), ref, secret.New("sk-or-v1-"+strings.Repeat("r", 24)))
	if err == nil || !strings.Contains(err.Error(), "read back") {
		t.Fatalf("an unproven write = %v, want a read-back error", err)
	}
}
