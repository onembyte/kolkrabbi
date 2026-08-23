package redact

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestClassifyInfersEverySupportedShape(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		provider string
	}{
		{"openrouter", "sk-or-v1-0123456789abcdef", "openrouter"},
		{"anthropic", "sk-ant-api03-0123456789abcdef", "anthropic"},
		{"openai project", "sk-proj-0123456789abcdef", "openai"},
		{"openai service", "sk-svcacct-0123456789abcdef", "openai"},
		{"openai admin", "sk-admin-0123456789abcdef", "openai"},
		{"google", "AIza" + strings.Repeat("a", 35), "google"},
		{"groq", "gsk_0123456789abcdef", "groq"},
		{"xai", "xai-0123456789abcdef", "xai"},
		{"perplexity", "pplx-0123456789abcdef", "perplexity"},
		{"fireworks", "fw_0123456789abcdef", "fireworks"},
		{"cerebras", "csk-0123456789abcdef", "cerebras"},
		{"nvidia", "nvapi-0123456789abcdef", "nvidia"},
		{"replicate", "r8_0123456789abcdef", "replicate"},
		{"huggingface", "hf_0123456789abcdef", "huggingface"},
		{"openai legacy", "sk-" + strings.Repeat("a", 40), "openai"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Classify("  " + tt.key + "\n")
			if got.Provider != tt.provider || got.Denial != DenyNone || got.Ambiguous {
				t.Errorf("Classify = %+v, want provider %q", got, tt.provider)
			}
			if strings.Contains(fmt.Sprintf("%+v", got), tt.key) {
				t.Error("classification retained the credential")
			}
		})
	}
}

func TestClassifyRejectsEveryForbiddenShapeBeforeInference(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want Denial
	}{
		{"claude access token", "sk-ant-oat01-0123456789abcdef", DenyClaudeSubscription},
		{"claude refresh token", "sk-ant-ort01-0123456789abcdef", DenyClaudeSubscription},
		{"github classic", "ghp_0123456789abcdefghijklmnopqrstuvwxyz", DenyGitHub},
		{"github oauth", "gho_0123456789abcdefghijklmnopqrstuvwxyz", DenyGitHub},
		{"github server", "ghs_0123456789abcdefghijklmnopqrstuvwxyz", DenyGitHub},
		{"github user", "ghu_0123456789abcdefghijklmnopqrstuvwxyz", DenyGitHub},
		{"github refresh", "ghr_0123456789abcdefghijklmnopqrstuvwxyz", DenyGitHub},
		{"github fine grained", "github_pat_0123456789abcdefghijklmnopqrstuvwxyz", DenyGitHub},
		{"aws permanent", "AKIA0123456789ABCDEF", DenyAWS},
		{"aws temporary", "ASIA0123456789ABCDEF", DenyAWS},
		{"slack bot", "xoxb-0123456789-abcdef", DenySlack},
		{"slack user", "xoxp-0123456789-abcdef", DenySlack},
		{"slack app", "xoxa-0123456789-abcdef", DenySlack},
		{"slack session", "xoxs-0123456789-abcdef", DenySlack},
		{"private key", "noise -----BEGIN PRIVATE KEY----- more", DenyPrivateKey},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Classify(tt.key)
			if got.Denial != tt.want || got.Provider != "" || got.Ambiguous {
				t.Errorf("Classify = %+v, want denial %q", got, tt.want)
			}
		})
	}
}

func TestClassifyHonorsLengthAndAlphabetConstraints(t *testing.T) {
	for _, key := range []string{
		"",
		"not-a-known-key",
		"AIza" + strings.Repeat("a", 34),
		"AIza" + strings.Repeat("a", 36),
		"sk-" + strings.Repeat("a", 39),
		"sk-" + strings.Repeat("a", 39) + "-",
	} {
		if got := Classify(key); got.Provider != "" || got.Denial != DenyNone || got.Ambiguous {
			t.Errorf("Classify(%q) = %+v, want unknown", key, got)
		}
	}
}

func TestClassifyUsesLongestPrefixAndReportsATie(t *testing.T) {
	rules := []inferRule{
		{Prefix: "sk-", Provider: "short"},
		{Prefix: "sk-long-", Provider: "long"},
	}
	if got := classify("sk-long-value", rules, nil); got.Provider != "long" || got.Ambiguous {
		t.Errorf("longest-prefix classification = %+v", got)
	}
	rules = append(rules, inferRule{Prefix: "sk-long-", Provider: "tie"})
	if got := classify("sk-long-value", rules, nil); got.Provider != "" || !got.Ambiguous {
		t.Errorf("equal-prefix classification = %+v, want ambiguity", got)
	}
}

func TestMaskNeverExposesOverlappingSlices(t *testing.T) {
	if got := Mask("sk-or-v1-0123456789abcdef0123456789abcdef"); got != "sk-or-v1-…cdef" {
		t.Errorf("OpenRouter mask = %q", got)
	}
	if got := Mask("abcdefghijklmnopqrst"); got != "abcd…qrst" {
		t.Errorf("unknown mask = %q", got)
	}
	for n := 0; n <= 128; n++ {
		raw := strings.Repeat("x", n)
		got := Mask(raw)
		parts := strings.Split(got, "…")
		if len(parts) != 2 {
			t.Errorf("Mask(len=%d) = %q, want exactly one ellipsis", n, got)
			continue
		}
		visible := len(parts[0]) + len(parts[1])
		if visible > n-8 && visible != 0 {
			t.Errorf("Mask(len=%d) exposes %d bytes; at least 8 must stay hidden", n, visible)
		}
	}
}

func TestClassificationHasNoCredentialField(t *testing.T) {
	typeOf := reflect.TypeOf(Classification{})
	for i := 0; i < typeOf.NumField(); i++ {
		name := strings.ToLower(typeOf.Field(i).Name)
		for _, banned := range []string{"key", "input", "raw", "secret", "token", "credential"} {
			if name == banned {
				t.Errorf("Classification retains input in field %q", typeOf.Field(i).Name)
			}
		}
	}
}
