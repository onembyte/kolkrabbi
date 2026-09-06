package keystore

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/secret"
)

type fakeStore struct {
	values map[string]secret.Secret
	err    error
}

func (f fakeStore) Name() Backend                                 { return BackendFile }
func (f fakeStore) Available(context.Context) error               { return nil }
func (f fakeStore) Set(context.Context, Ref, secret.Secret) error { return nil }
func (f fakeStore) Del(context.Context, Ref) error                { return nil }
func (f fakeStore) List(context.Context) ([]Entry, error)         { return nil, nil }
func (f fakeStore) Probe(_ context.Context, ref Ref) (Entry, error) {
	if _, ok := f.values[ref.String()]; ok {
		return Entry{Ref: ref, Backend: BackendFile, Mask: "…"}, nil
	}
	return Entry{}, ErrNotFound
}
func (f fakeStore) Get(_ context.Context, ref Ref) (secret.Secret, error) {
	if f.err != nil {
		return secret.Secret{}, f.err
	}
	if v, ok := f.values[ref.String()]; ok {
		return v, nil
	}
	return secret.Secret{}, ErrNotFound
}

func env(m map[string]string) func(string) string {
	return func(name string) string { return m[name] }
}

// Plan 05 §1 step B, the chain: KOLK_API_KEY first, the provider's own
// variable second, the store third; first hit wins and the search stops; a
// hit records its source and the trace says what each other link held, so
// `kolk key --why` can show what was shadowed.
func TestResolveWalksTheChainAndRecordsTheFirstHit(t *testing.T) {
	ref := Ref{Provider: "openrouter", Profile: "default"}
	store := fakeStore{values: map[string]secret.Secret{"openrouter/default": secret.New("sk-or-v1-" + strings.Repeat("s", 20))}}

	got, err := Resolve(context.Background(), ref, env(map[string]string{"OPENROUTER_API_KEY": "sk-or-v1-" + strings.Repeat("e", 20)}), store)
	if err != nil {
		t.Fatal(err)
	}
	if got.Source != "OPENROUTER_API_KEY" || !strings.HasSuffix(got.Value.Reveal(), "eeee") {
		t.Fatalf("resolution = source %q value …%s, want the provider env", got.Source, got.Value.Reveal()[len(got.Value.Reveal())-4:])
	}
	if len(got.Trace) != 4 || got.Trace[0].Name != "flag" || got.Trace[1].Name != "KOLK_API_KEY" || got.Trace[2].Name != "OPENROUTER_API_KEY" || got.Trace[3].Name != "store" {
		t.Fatalf("trace = %+v", got.Trace)
	}
	if got.Trace[2].Outcome != "hit" || got.Trace[3].Outcome != "shadowed" || got.Trace[1].Outcome != "absent" || got.Trace[0].Outcome != "none" {
		t.Fatalf("outcomes = %+v", got.Trace)
	}

	got, err = Resolve(context.Background(), ref, env(nil), store)
	if err != nil || got.Source != "store (file)" || !strings.HasSuffix(got.Value.Reveal(), "ssss") {
		t.Fatalf("store hit = %+v, %v", got, err)
	}

	got, err = Resolve(context.Background(), ref, env(map[string]string{"KOLK_API_KEY": "sk-or-v1-" + strings.Repeat("k", 20)}), store)
	if err != nil || got.Source != "KOLK_API_KEY" {
		t.Fatalf("KOLK_API_KEY = %+v, %v; want it to win", got, err)
	}
}

// Three outcomes per link, never two: nothing found continues; a locked or
// unavailable backend stops with a named error and never falls through to
// "no credential"; nothing anywhere is ErrNotFound with the trace intact.
func TestResolveStopsOnALockedBackendAndContinuesPastNothing(t *testing.T) {
	ref := Ref{Provider: "anthropic", Profile: "default"}
	_, err := Resolve(context.Background(), ref, env(nil), fakeStore{err: ErrLocked})
	if !errors.Is(err, ErrLocked) || !strings.Contains(err.Error(), "anthropic/default") {
		t.Fatalf("locked backend: err = %v, want ErrLocked naming the credential", err)
	}
	got, err := Resolve(context.Background(), ref, env(nil), fakeStore{})
	if !errors.Is(err, ErrNotFound) || len(got.Trace) != 4 {
		t.Fatalf("nothing anywhere: err = %v trace = %+v", err, got.Trace)
	}
}

// KOLK_API_KEY with a shape that belongs to another provider warns and is
// still used for the provider asked; it is never re-routed.
func TestResolveWarnsOnAShapeMismatchAndDoesNotReroute(t *testing.T) {
	ref := Ref{Provider: "openrouter", Profile: "default"}
	got, err := Resolve(context.Background(), ref, env(map[string]string{"KOLK_API_KEY": "sk-ant-api03-" + strings.Repeat("a", 20)}), fakeStore{})
	if err != nil || got.Source != "KOLK_API_KEY" {
		t.Fatalf("resolution = %+v, %v", got, err)
	}
	if !strings.Contains(got.Warning, "anthropic") {
		t.Fatalf("no shape warning: %q", got.Warning)
	}
}

// The curated provider variables, and nothing derived from an id.
func TestProviderEnvIsACuratedList(t *testing.T) {
	for provider, want := range map[string]string{"openrouter": "OPENROUTER_API_KEY", "anthropic": "ANTHROPIC_API_KEY", "openai": "OPENAI_API_KEY", "google": "GEMINI_API_KEY", "xai": "XAI_API_KEY", "groq": "GROQ_API_KEY", "mistral": "MISTRAL_API_KEY", "deepseek": "DEEPSEEK_API_KEY", "together": "TOGETHER_API_KEY", "perplexity": "PERPLEXITY_API_KEY", "some_new_thing": ""} {
		if got := ProviderEnv(provider); got != want {
			t.Fatalf("ProviderEnv(%s) = %q, want %q", provider, got, want)
		}
	}
}
