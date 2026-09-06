package keystore

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/onembyte/kolkrabbi/internal/redact"
	"github.com/onembyte/kolkrabbi/internal/secret"
)

// The two backend outcomes plan 05 §1.2 names beside the ones above: a
// backend that holds the value but will not give it now, and one that did
// not answer in time. Both stop the chain with a named remedy; neither ever
// reads as "no credential".
var (
	ErrLocked  = errors.New("keystore: credential backend is locked")
	ErrTimeout = errors.New("keystore: credential backend timed out")
)

// providerEnv is the curated list (plan 05 §1 link 2). Nothing is derived
// from a provider id: an id with an underscore would be ambiguous, and the
// bash tool's deny list has to be knowable.
var providerEnv = map[string]string{
	"openrouter": "OPENROUTER_API_KEY",
	"anthropic":  "ANTHROPIC_API_KEY",
	"openai":     "OPENAI_API_KEY",
	"google":     "GEMINI_API_KEY",
	"groq":       "GROQ_API_KEY",
	"xai":        "XAI_API_KEY",
	"mistral":    "MISTRAL_API_KEY",
	"together":   "TOGETHER_API_KEY",
	"deepseek":   "DEEPSEEK_API_KEY",
	"perplexity": "PERPLEXITY_API_KEY",
}

// ProviderEnv is the provider's own environment variable, or empty for a
// provider not on the curated list.
func ProviderEnv(provider string) string {
	return providerEnv[strings.ToLower(strings.TrimSpace(provider))]
}

// Link is one rung of the chain as the trace reports it.
type Link struct {
	Rank    int
	Name    string
	Outcome string // none | absent | hit | shadowed | error
	Detail  string
}

// Resolution is a resolved credential with where it came from and what every
// other link held, so `kolk key --why` can show what was shadowed.
type Resolution struct {
	Value   secret.Secret
	Source  string
	Warning string
	Trace   []Link
}

// Resolve walks plan 05 §1 step B: a secret-bearing flag (structurally none,
// printed as such), KOLK_API_KEY, the provider's own variable, then the
// store. First hit wins and the search stops; a link with nothing continues;
// a backend that is locked, unavailable or slow stops the chain with a named
// error, never falling through to "no credential". After a hit the remaining
// links are probed, never read, so the trace can say what they held.
func Resolve(ctx context.Context, ref Ref, getenv func(string) string, store Store) (Resolution, error) {
	ref, err := canonicalRef(ref)
	if err != nil {
		return Resolution{}, err
	}
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	res := Resolution{Trace: []Link{{Rank: 0, Name: "flag", Outcome: "none", Detail: "by design; a secret in argv is world-readable"}}}
	hit := func(rank int, name, value string) {
		res.Value, res.Source = secret.New(value), name
	}

	// 1. KOLK_API_KEY, its provider from KOLK_PROVIDER or the key's shape;
	// a shape that belongs elsewhere warns and is still used as asked.
	if v := strings.TrimSpace(getenv("KOLK_API_KEY")); v != "" {
		link := Link{Rank: 1, Name: "KOLK_API_KEY", Outcome: "hit", Detail: redact.Mask(v)}
		if shape := redact.Classify(v).Provider; shape != "" && shape != ref.Provider {
			res.Warning = fmt.Sprintf("KOLK_API_KEY looks like a %s key and is being used for %s; kolk does not re-route a secret by its shape", shape, ref.Provider)
			link.Detail += " (shape: " + shape + ")"
		}
		res.Trace = append(res.Trace, link)
		hit(1, "KOLK_API_KEY", v)
	} else {
		res.Trace = append(res.Trace, Link{Rank: 1, Name: "KOLK_API_KEY", Outcome: "absent"})
	}

	// 2. The provider's own variable, from the curated list.
	if name := ProviderEnv(ref.Provider); name != "" {
		v := strings.TrimSpace(getenv(name))
		switch {
		case v == "":
			res.Trace = append(res.Trace, Link{Rank: 2, Name: name, Outcome: "absent"})
		case res.Source != "":
			res.Trace = append(res.Trace, Link{Rank: 2, Name: name, Outcome: "shadowed", Detail: redact.Mask(v)})
		default:
			res.Trace = append(res.Trace, Link{Rank: 2, Name: name, Outcome: "hit", Detail: redact.Mask(v)})
			hit(2, name, v)
		}
	} else {
		res.Trace = append(res.Trace, Link{Rank: 2, Name: "provider env", Outcome: "none", Detail: ref.Provider + " has no curated variable"})
	}

	// 3. The store: a lookup, not a cascade. After a hit it is probed, never read.
	if store == nil {
		res.Trace = append(res.Trace, Link{Rank: 3, Name: "store", Outcome: "none", Detail: "no store configured"})
	} else if res.Source != "" {
		entry, err := store.Probe(ctx, ref)
		switch {
		case err == nil:
			res.Trace = append(res.Trace, Link{Rank: 3, Name: "store", Outcome: "shadowed", Detail: string(entry.Backend) + " " + entry.Mask})
		case errors.Is(err, ErrNotFound):
			res.Trace = append(res.Trace, Link{Rank: 3, Name: "store", Outcome: "absent"})
		default:
			res.Trace = append(res.Trace, Link{Rank: 3, Name: "store", Outcome: "error", Detail: err.Error()})
		}
	} else {
		value, err := store.Get(ctx, ref)
		switch {
		case err == nil:
			source := "store (" + string(store.Name()) + ")"
			if entry, perr := store.Probe(ctx, ref); perr == nil && entry.Backend != "" {
				source = "store (" + string(entry.Backend) + ")"
			}
			res.Trace = append(res.Trace, Link{Rank: 3, Name: "store", Outcome: "hit", Detail: source})
			res.Value, res.Source = value, source
		case errors.Is(err, ErrNotFound):
			res.Trace = append(res.Trace, Link{Rank: 3, Name: "store", Outcome: "absent"})
		default:
			// Locked, unavailable, timed out, corrupt, foreign: stop and say so.
			res.Trace = append(res.Trace, Link{Rank: 3, Name: "store", Outcome: "error", Detail: err.Error()})
			return res, fmt.Errorf("%s: the store could not answer for %s and kolk will not guess another source: %w", ref.String(), ref.String(), err)
		}
	}
	if res.Source == "" {
		return res, ErrNotFound
	}
	return res, nil
}
