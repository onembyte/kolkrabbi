// Package redact owns credential-shape facts that are safe to use without
// importing a credential type. The same data drives provider inference and
// masking; neither result retains the input value.
package redact

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
)

//go:embed keyshapes.json
var shapeData []byte

type Denial string

const (
	DenyNone               Denial = ""
	DenyClaudeSubscription Denial = "claude_subscription"
	DenyGitHub             Denial = "github"
	DenyAWS                Denial = "aws"
	DenySlack              Denial = "slack"
	DenyPrivateKey         Denial = "private_key"
)

// Classification contains safe facts only. In particular it never retains
// the input, even for an unknown or denied shape.
type Classification struct {
	Provider  string
	Denial    Denial
	Ambiguous bool
}

type shapeTable struct {
	Version int         `json:"version"`
	Infer   []inferRule `json:"infer"`
	Deny    []denyRule  `json:"deny"`
}

type inferRule struct {
	Prefix         string `json:"prefix"`
	Provider       string `json:"provider"`
	MaskPrefix     string `json:"mask_prefix"`
	ExactLength    int    `json:"exact_length,omitempty"`
	MinSuffix      int    `json:"min_suffix,omitempty"`
	ScrubMinSuffix int    `json:"scrub_min_suffix,omitempty"`
	Alphabet       string `json:"alphabet,omitempty"`
}

type denyRule struct {
	Prefix    string `json:"prefix,omitempty"`
	Contains  string `json:"contains,omitempty"`
	Kind      Denial `json:"kind"`
	MinSuffix int    `json:"min_suffix,omitempty"`
	Alphabet  string `json:"alphabet,omitempty"`
}

var keyShapes = mustLoadShapes()

func mustLoadShapes() shapeTable {
	var table shapeTable
	if err := json.Unmarshal(shapeData, &table); err != nil {
		panic(fmt.Sprintf("redact: embedded key shape table is invalid: %v", err))
	}
	if table.Version != 1 || len(table.Infer) == 0 || len(table.Deny) == 0 {
		panic("redact: embedded key shape table has an unsupported or empty schema")
	}
	seen := map[string]bool{}
	for _, rule := range table.Infer {
		if rule.Prefix == "" || rule.Provider == "" || seen[rule.Prefix] {
			panic("redact: embedded inference rule is empty or duplicated")
		}
		if rule.ExactLength == 0 && rule.MinSuffix <= 0 || rule.ScrubMinSuffix < 0 {
			panic("redact: embedded inference rule has no positive length bound")
		}
		if rule.Alphabet != "" && rule.Alphabet != "alnum" && rule.Alphabet != "key" {
			panic("redact: embedded inference rule has an unknown alphabet")
		}
		seen[rule.Prefix] = true
	}
	for _, rule := range table.Deny {
		if (rule.Prefix == "") == (rule.Contains == "") || rule.Kind == DenyNone {
			panic("redact: embedded denial rule must have one matcher and a kind")
		}
		if rule.Alphabet != "" && rule.Alphabet != "alnum" && rule.Alphabet != "key" {
			panic("redact: embedded denial rule has an unknown alphabet")
		}
		if rule.Prefix != "" && rule.MinSuffix <= 0 {
			panic("redact: embedded prefix denial has no positive length bound")
		}
	}
	return table
}

// Classify applies the deny list before considering any provider. Among valid
// inference rows, only matches with the longest prefix survive.
func Classify(value string) Classification {
	return classify(strings.TrimSpace(value), keyShapes.Infer, keyShapes.Deny)
}

func classify(value string, infer []inferRule, deny []denyRule) Classification {
	for _, rule := range deny {
		if rule.Prefix != "" && strings.HasPrefix(value, rule.Prefix) ||
			rule.Contains != "" && strings.Contains(value, rule.Contains) {
			return Classification{Denial: rule.Kind}
		}
	}

	longest := -1
	provider := ""
	matches := 0
	for _, rule := range infer {
		if !rule.matches(value) {
			continue
		}
		switch {
		case len(rule.Prefix) > longest:
			longest = len(rule.Prefix)
			provider = rule.Provider
			matches = 1
		case len(rule.Prefix) == longest:
			matches++
		}
	}
	if matches == 1 {
		return Classification{Provider: provider}
	}
	return Classification{Ambiguous: matches > 1}
}

func (r inferRule) matches(value string) bool {
	if !strings.HasPrefix(value, r.Prefix) {
		return false
	}
	if r.ExactLength > 0 && len(value) != r.ExactLength {
		return false
	}
	suffix := value[len(r.Prefix):]
	if len(suffix) < r.MinSuffix {
		return false
	}
	for _, c := range []byte(suffix) {
		switch r.Alphabet {
		case "alnum":
			if !isAlphaNumeric(c) {
				return false
			}
		case "key":
			if !isAlphaNumeric(c) && c != '-' && c != '_' {
				return false
			}
		}
	}
	return true
}

func isAlphaNumeric(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}
