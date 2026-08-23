package keystore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/lock"
	"github.com/onembyte/kolkrabbi/internal/secret"
)

const testKey = "sk-or-v1-0123456789abcdef0123456789abcdef"

func TestFileStoreRoundTripsAProviderProfile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "credentials.json")
	store := NewFileStore(path)
	ref := Ref{Provider: " OpenRouter "}

	if _, err := store.Get(context.Background(), ref); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get before Set = %v, want ErrNotFound", err)
	}
	if err := store.Set(context.Background(), ref, secret.New(testKey)); err != nil {
		t.Fatal(err)
	}

	got, err := store.Get(context.Background(), Ref{Provider: "OPENROUTER", Profile: "default"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Reveal() != testKey {
		t.Errorf("Get returned %v, not the stored credential", got)
	}

	entries, err := store.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("List returned %d entries, want 1", len(entries))
	}
	entry := entries[0]
	if entry.Ref.String() != "openrouter/default" || entry.Backend != BackendFile {
		t.Errorf("entry = %+v", entry)
	}
	if entry.Created.IsZero() || entry.Mask == "" || entry.KeyHash == "" {
		t.Errorf("entry metadata is incomplete: %+v", entry)
	}
	probed, err := store.Probe(context.Background(), Ref{Provider: "openrouter"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(probed, entry) {
		t.Errorf("Probe = %+v, List entry = %+v", probed, entry)
	}

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), testKey) {
		t.Error("manifest contains the unencoded plaintext credential")
	}
	var disk struct {
		Version     int `json:"version"`
		Credentials map[string]struct {
			Backend string `json:"backend"`
			Value   string `json:"value"`
		} `json:"credentials"`
	}
	if err := json.Unmarshal(b, &disk); err != nil {
		t.Fatal(err)
	}
	if disk.Version != 1 {
		t.Errorf("manifest version = %d, want 1", disk.Version)
	}
	row, ok := disk.Credentials["openrouter/default"]
	if !ok || row.Backend != "file" || !strings.HasPrefix(row.Value, "kolk-b64:") {
		t.Errorf("disk row = %+v, present=%v", row, ok)
	}
}

func TestFileStoreCommitsWriteMetadataWithTheCredential(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")
	store := NewFileStore(path)
	verified := time.Date(2026, time.August, 23, 12, 34, 56, 0, time.FixedZone("test", -3*60*60))
	meta := WriteMetadata{Verified: verified, Source: "stdin", Note: "user supplied"}

	if err := store.SetWithMetadata(context.Background(), Ref{Provider: "openrouter"}, secret.New(testKey), meta); err != nil {
		t.Fatal(err)
	}
	entry, err := store.Probe(context.Background(), Ref{Provider: "openrouter"})
	if err != nil {
		t.Fatal(err)
	}
	if !entry.Verified.Equal(verified) || entry.Verified.Location() != time.UTC {
		t.Errorf("Verified = %v, want the same instant normalized to UTC", entry.Verified)
	}
	if entry.Source != meta.Source || entry.Note != meta.Note {
		t.Errorf("safe write metadata = %+v", entry)
	}
}

func TestFileStoreTaggedEncodingRoundTripsNonTextBytes(t *testing.T) {
	store := NewFileStore(filepath.Join(t.TempDir(), "credentials.json"))
	want := "prefix\x00\xff\ninside"
	if err := store.Set(context.Background(), Ref{Provider: "binary"}, secret.New(want)); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(context.Background(), Ref{Provider: "binary"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Reveal() != want {
		t.Errorf("round trip changed bytes: got %q, want %q", got.Reveal(), want)
	}
}

func TestFileStorePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows ACL validation belongs to migration step 13")
	}
	path := filepath.Join(t.TempDir(), "state", "credentials.json")
	if err := NewFileStore(path).Set(context.Background(), Ref{Provider: "openrouter"}, secret.New(testKey)); err != nil {
		t.Fatal(err)
	}
	for _, check := range []struct {
		path string
		want os.FileMode
	}{
		{filepath.Dir(path), 0o700},
		{path, 0o600},
		{path + ".lock", 0o600},
	} {
		info, err := os.Stat(check.path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != check.want {
			t.Errorf("%s mode = %04o, want %04o", check.path, got, check.want)
		}
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFileStore(path).List(context.Background()); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("repaired credentials mode = %04o, want 0600", got)
	}
	if err := os.Chmod(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := NewFileStore(path).Set(context.Background(), Ref{Provider: "anthropic"}, secret.New(testKey)); err != nil {
		t.Fatal(err)
	}
	info, err = os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Errorf("repaired credential directory mode = %04o, want 0700", got)
	}
}

func TestConcurrentStoresLoseNoCredentials(t *testing.T) {
	const writers = 40
	path := filepath.Join(t.TempDir(), "credentials.json")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	start := make(chan struct{})
	errs := make(chan error, writers)

	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			provider := fmt.Sprintf("provider-%02d", i)
			key := secret.New(fmt.Sprintf("sk-or-v1-%032d", i))
			errs <- NewFileStore(path).Set(ctx, Ref{Provider: provider}, key)
		}(i)
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("Set: %v", err)
		}
	}

	entries, err := NewFileStore(path).List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != writers {
		t.Fatalf("concurrent manifest contains %d credentials, want %d", len(entries), writers)
	}
	for i, entry := range entries {
		want := fmt.Sprintf("provider-%02d/default", i)
		if entry.Ref.String() != want {
			t.Errorf("entry %d = %s, want %s", i, entry.Ref, want)
		}
	}
}

func TestConcurrentProcessesLoseNoCredentials(t *testing.T) {
	const (
		pathEnv  = "KOLK_TEST_KEYSTORE_PATH"
		indexEnv = "KOLK_TEST_KEYSTORE_INDEX"
		writers  = 8
	)
	if path := os.Getenv(pathEnv); path != "" {
		i, err := strconv.Atoi(os.Getenv(indexEnv))
		if err != nil {
			t.Fatal(err)
		}
		provider := fmt.Sprintf("process-%02d", i)
		if err := NewFileStore(path).Set(context.Background(), Ref{Provider: provider}, secret.New(fmt.Sprintf("sk-or-v1-%032d", i))); err != nil {
			t.Fatal(err)
		}
		return
	}
	if runtime.GOOS == "windows" {
		t.Skip("the Windows lock implementation is an explicit step-13 stub")
	}

	path := filepath.Join(t.TempDir(), "credentials.json")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	errs := make(chan error, writers)
	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestConcurrentProcessesLoseNoCredentials$")
			cmd.Env = append(os.Environ(), pathEnv+"="+path, indexEnv+"="+strconv.Itoa(i))
			errs <- cmd.Run()
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("credential writer process: %v", err)
		}
	}

	entries, err := NewFileStore(path).List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != writers {
		t.Fatalf("cross-process manifest contains %d credentials, want %d", len(entries), writers)
	}
}

func TestFileStoreHonorsTheLockContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")
	if err := os.WriteFile(path, []byte("{\"version\":1,\"credentials\":{}}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	held, err := lock.Try(path + ".lock")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = held.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err = NewFileStore(path).Set(ctx, Ref{Provider: "openrouter"}, secret.New(testKey))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Set under contention = %v, want context.DeadlineExceeded", err)
	}
	b, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if strings.Contains(string(b), "openrouter") {
		t.Error("timed-out Set changed the manifest")
	}
}

func TestCanceledOperationsDoNotTouchDisk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not-created", "credentials.json")
	store := NewFileStore(path)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ref := Ref{Provider: "openrouter"}

	checks := []struct {
		name string
		call func() error
	}{
		{"available", func() error { return store.Available(ctx) }},
		{"get", func() error { _, err := store.Get(ctx, ref); return err }},
		{"set", func() error { return store.Set(ctx, ref, secret.New(testKey)) }},
		{"del", func() error { return store.Del(ctx, ref) }},
		{"probe", func() error { _, err := store.Probe(ctx, ref); return err }},
		{"list", func() error { _, err := store.List(ctx); return err }},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			if err := check.call(); !errors.Is(err, context.Canceled) {
				t.Errorf("operation = %v, want context.Canceled", err)
			}
		})
	}
	if _, err := os.Stat(filepath.Dir(path)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("canceled operation created its storage directory: %v", err)
	}
}

func TestFileStoreRejectsUnsafeOrUnknownData(t *testing.T) {
	dir := t.TempDir()
	fixtures := []struct {
		name string
		body string
		want error
	}{
		{"future version", `{"version":2,"credentials":{}}`, ErrVersion},
		{"unknown backend", `{"version":1,"credentials":{"openrouter/default":{"backend":"` + testKey + `","value":"kolk-b64:eA=="}}}`, ErrUnavailable},
		{"corrupt json", `{"version":1,"credentials":{"openrouter/default":"` + testKey, ErrCorrupt},
		{"empty file", "\n", ErrCorrupt},
		{"missing value tag", `{"version":1,"credentials":{"openrouter/default":{"backend":"file","value":"` + testKey + `"}}}`, ErrCorrupt},
		{"invalid base64", `{"version":1,"credentials":{"openrouter/default":{"backend":"file","value":"kolk-b64:***"}}}`, ErrCorrupt},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			path := filepath.Join(dir, strings.ReplaceAll(fixture.name, " ", "-")+".json")
			if err := os.WriteFile(path, []byte(fixture.body), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := NewFileStore(path).Get(context.Background(), Ref{Provider: "openrouter"})
			if !errors.Is(err, fixture.want) {
				t.Errorf("Get = %v, want %v", err, fixture.want)
			}
			if err != nil && strings.Contains(err.Error(), testKey) {
				t.Errorf("error quoted credential contents: %v", err)
			}
		})
	}
}

func TestFileStoreRefusesSymlinksEmptyInputsAndOversizedValues(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.json")
	if err := os.WriteFile(target, []byte(`{"version":1,"credentials":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "credentials.json")
	if err := os.Symlink(target, link); err == nil {
		if err := NewFileStore(link).Set(context.Background(), Ref{Provider: "openrouter"}, secret.New(testKey)); !errors.Is(err, ErrCorrupt) {
			t.Errorf("Set through symlink = %v, want ErrCorrupt", err)
		}
	} else {
		t.Logf("symlinks unavailable: %v", err)
	}
	if err := NewFileStore(dir).Set(context.Background(), Ref{Provider: "openrouter"}, secret.New(testKey)); !errors.Is(err, ErrCorrupt) {
		t.Errorf("Set to non-regular path = %v, want ErrCorrupt", err)
	}

	store := NewFileStore(filepath.Join(dir, "safe.json"))
	for _, ref := range []Ref{{}, {Provider: "   "}, {Provider: "open/router"}} {
		if err := store.Set(context.Background(), ref, secret.New(testKey)); !errors.Is(err, ErrInvalidRef) {
			t.Errorf("Set(%+v) = %v, want ErrInvalidRef", ref, err)
		}
	}
	if err := store.Set(context.Background(), Ref{Provider: "openrouter"}, secret.Secret{}); !errors.Is(err, ErrEmpty) {
		t.Errorf("Set(empty) = %v, want ErrEmpty", err)
	}
	tooLarge := secret.New(strings.Repeat("x", MaxValueBytes+1))
	if err := store.Set(context.Background(), Ref{Provider: "openrouter"}, tooLarge); !errors.Is(err, ErrTooLarge) {
		t.Errorf("Set(oversized) = %v, want ErrTooLarge", err)
	}
}

func TestDelIsIdempotentAndScoped(t *testing.T) {
	store := NewFileStore(filepath.Join(t.TempDir(), "credentials.json"))
	ctx := context.Background()
	openrouter := Ref{Provider: "openrouter"}
	anthropic := Ref{Provider: "anthropic"}
	if err := store.Del(ctx, openrouter); err != nil {
		t.Fatal(err)
	}
	if err := store.Set(ctx, openrouter, secret.New(testKey)); err != nil {
		t.Fatal(err)
	}
	if err := store.Set(ctx, anthropic, secret.New("sk-ant-api03-0123456789abcdef0123456789abcdef")); err != nil {
		t.Fatal(err)
	}
	if err := store.Del(ctx, openrouter); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(ctx, openrouter); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleted credential survived: %v", err)
	}
	if _, err := store.Get(ctx, anthropic); err != nil {
		t.Errorf("Delete damaged another slot: %v", err)
	}
}

func TestProbeDoesNotDecodeTheCredentialValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")
	body := `{"version":1,"credentials":{"openrouter/default":{"backend":"file","value":"not-an-encoding","mask":"sk-or-v1-…cdef","key_hash":"safe-metadata"}}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewFileStore(path)
	entry, err := store.Probe(context.Background(), Ref{Provider: "openrouter"})
	if err != nil {
		t.Fatalf("Probe decoded the value: %v", err)
	}
	if entry.Mask != "sk-or-v1-…cdef" || entry.KeyHash != "safe-metadata" {
		t.Errorf("Probe metadata = %+v", entry)
	}
	if _, err := store.Get(context.Background(), Ref{Provider: "openrouter"}); !errors.Is(err, ErrCorrupt) {
		t.Errorf("Get with invalid encoded value = %v, want ErrCorrupt", err)
	}
}

func TestFileStoreImplementsStore(t *testing.T) {
	var store Store = NewFileStore(filepath.Join(t.TempDir(), "credentials.json"))
	if err := store.Available(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestEntryHasNoPlaintextField(t *testing.T) {
	typeOf := reflect.TypeOf(Entry{})
	for i := 0; i < typeOf.NumField(); i++ {
		name := strings.ToLower(typeOf.Field(i).Name)
		for _, banned := range []string{"value", "key", "token", "secret", "credential"} {
			if name == banned {
				t.Errorf("Entry has plaintext-capable field %q", typeOf.Field(i).Name)
			}
		}
	}

	entries := []Entry{{Ref: Ref{Provider: "z", Profile: "default"}}, {Ref: Ref{Provider: "a", Profile: "default"}}}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Ref.String() < entries[j].Ref.String() })
	if entries[0].Ref.Provider != "a" {
		t.Error("test assumption: refs do not sort canonically")
	}
}
