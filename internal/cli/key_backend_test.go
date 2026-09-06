package cli

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/keystore"
	"github.com/onembyte/kolkrabbi/internal/secret"
)

// playSecurity plays /usr/bin/security for the CLI: a login keychain, items
// by account, the -w read, the delete with its stderr sentence and exit 0.
type playSecurity struct {
	items map[string]string
	calls int
}

func (p *playSecurity) LookPath(name string) (string, error) { return "/usr/bin/" + name, nil }
func (p *playSecurity) Run(_ context.Context, argv []string, stdin string) ([]byte, int, error) {
	p.calls++
	if p.items == nil {
		p.items = map[string]string{}
	}
	account := func(f []string) string {
		for i, a := range f {
			if a == "-a" && i+1 < len(f) {
				return strings.Trim(f[i+1], `"`)
			}
		}
		return ""
	}
	if len(argv) > 1 && argv[1] == "-q" {
		f := strings.Fields(stdin)
		for i, a := range f {
			if a == "-w" && i+1 < len(f) {
				p.items[account(f)] = strings.Trim(f[i+1], `"`)
			}
		}
		return nil, 0, nil
	}
	switch argv[1] {
	case "login-keychain":
		return []byte("\"/Users/x/Library/Keychains/login.keychain-db\"\n"), 0, nil
	case "find-generic-password":
		v, ok := p.items[account(argv)]
		if !ok {
			return nil, 44, nil
		}
		for _, a := range argv {
			if a == "-w" {
				return []byte(v + "\n"), 0, nil
			}
		}
		return []byte("class: genp\n"), 0, nil
	case "delete-generic-password":
		if _, ok := p.items[account(argv)]; !ok {
			return nil, 44, nil
		}
		delete(p.items, account(argv))
		return nil, 0, nil
	}
	return nil, 1, nil
}

// V05.S.b: `kolk key --backend keychain` moves a stored key into the
// keychain — read, write, read back, route, delete — says once what the
// keychain buys and nothing more, and the chain then resolves the key from
// the keychain; `--backend file` brings it back and the keychain item goes.
func TestKeyBackendMovesACredentialBothWays(t *testing.T) {
	d := isolateHome(t)
	key := "sk-or-v1-" + strings.Repeat("m", 24)
	if err := keystore.NewFileStore(d.CredentialsFile()).Set(context.Background(), keystore.Ref{Provider: "openrouter"}, secret.New(key)); err != nil {
		t.Fatal(err)
	}
	play := &playSecurity{}
	a, out, errOut := newTestApp(t, "")
	a.keychainSpawner = play
	if code := runRetiredVerb(t, a, "key", "--backend", "keychain"); code != ExitOK {
		t.Fatalf("--backend keychain exit = %d: %s %s", code, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "moved to the keychain") || !strings.Contains(errOut.String(), "encrypts the credential at rest") || !strings.Contains(errOut.String(), "nothing against code running as you") {
		t.Fatalf("move: out %q err %q", out.String(), errOut.String())
	}
	entry, err := keystore.NewFileStore(d.CredentialsFile()).Probe(context.Background(), keystore.Ref{Provider: "openrouter"})
	if err != nil || entry.Backend != keystore.BackendKeychain {
		t.Fatalf("manifest after move = %+v, %v; want the keychain route", entry, err)
	}
	if v, ok := play.items["openrouter/default"]; !ok || !strings.HasPrefix(v, "kolk-b64:") {
		t.Fatalf("keychain holds %q, want the tagged base64", v)
	}
	if strings.Contains(string(mustRead(t, d.CredentialsFile())), "kolk-b64:") {
		t.Fatal("the file still holds a value for a keychain-routed row")
	}
	res, err := keystore.Resolve(context.Background(), keystore.Ref{Provider: "openrouter"}, func(string) string { return "" }, routedStore(d.CredentialsFile(), play))
	if err != nil || res.Value.Reveal() != key || res.Source != "store (keychain)" {
		t.Fatalf("resolve from the keychain = %+v, %v", res, err)
	}

	out.Reset()
	errOut.Reset()
	if code := runRetiredVerb(t, a, "key", "--backend", "file"); code != ExitOK {
		t.Fatalf("--backend file exit = %d: %s %s", code, out.String(), errOut.String())
	}
	entry, _ = keystore.NewFileStore(d.CredentialsFile()).Probe(context.Background(), keystore.Ref{Provider: "openrouter"})
	if entry.Backend != keystore.BackendFile {
		t.Fatalf("manifest after moving back = %+v", entry)
	}
	if _, ok := play.items["openrouter/default"]; ok {
		t.Fatal("the keychain item outlived the move back: an orphan")
	}
	back, err := keystore.NewFileStore(d.CredentialsFile()).Get(context.Background(), keystore.Ref{Provider: "openrouter"})
	if err != nil || back.Reveal() != key {
		t.Fatalf("file after moving back = %v, %v", back, err)
	}
	if strings.Contains(errOut.String(), "encrypts the credential") {
		t.Fatal("the keychain notice was said for a move to the file")
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
