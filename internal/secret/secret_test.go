package secret

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const realKey = "sk-or-v1-0123456789abcdef0123456789abcdef"

// The whole point of the type. Every one of these is a path a key has actually
// escaped through in some tool: a print, a struct dump, a JSON encode.
func TestSecretNeverPrintsItself(t *testing.T) {
	s := New(realKey)

	type holder struct {
		Name string
		Key  Secret
	}
	h := holder{Name: "openrouter", Key: s}

	cases := map[string]string{
		"String()":      s.String(),
		"%v":            fmt.Sprintf("%v", s),
		"%s":            fmt.Sprintf("%s", s),
		"%q":            fmt.Sprintf("%q", s),
		"%+v":           fmt.Sprintf("%+v", s),
		"%#v":           fmt.Sprintf("%#v", s),
		"%d (bad verb)": fmt.Sprintf("%d", s),
		"nested %v":     fmt.Sprintf("%v", h),
		"nested %+v":    fmt.Sprintf("%+v", h),
		"nested %#v":    fmt.Sprintf("%#v", h),
		"Errorf %v":     fmt.Errorf("failed with %v", s).Error(),
		"Sprint":        fmt.Sprint(s),
		"Sprintln":      fmt.Sprintln(s),
		"pointer %v":    fmt.Sprintf("%v", &s),
		"slice of them": fmt.Sprintf("%v", []Secret{s}),
		"map value":     fmt.Sprintf("%v", map[string]Secret{"k": s}),
	}
	for name, got := range cases {
		if strings.Contains(got, realKey) {
			t.Errorf("%s leaked the whole key: %s", name, got)
		}
		if strings.Contains(got, "0123456789abcdef") {
			t.Errorf("%s leaked a usable portion of the key: %s", name, got)
		}
	}

	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), realKey) {
		t.Errorf("json.Marshal leaked the key: %s", b)
	}
	b, err = json.Marshal(h)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), realKey) {
		t.Errorf("json.Marshal of a containing struct leaked the key: %s", b)
	}
}

func TestRevealIsTheOnlyWayOut(t *testing.T) {
	s := New(realKey)
	if s.Reveal() != realKey {
		t.Errorf("Reveal() = %q, want the original key", s.Reveal())
	}
	// Pasting is how keys are entered, and a trailing newline from a heredoc or
	// a copied line otherwise produces a 401 with no explanation.
	if New("  "+realKey+"\n").Reveal() != realKey {
		t.Error("New did not trim surrounding whitespace")
	}
}

func TestNewRegistersAShapeLessLiteralForDurableScrubbing(t *testing.T) {
	value := "mistral-C4nary-7pQ9vX2"
	_ = New(value)
	output := Scrub("provider response repeated " + value)
	if strings.Contains(output, value) || !strings.Contains(output, "[redacted credential #") {
		t.Fatalf("New did not register the exact literal: %s", output)
	}
}

func TestZeroSecretIsAValidState(t *testing.T) {
	var s Secret
	if !s.IsZero() {
		t.Error("the zero Secret must mean 'no key'")
	}
	if got := s.String(); got != "(none)" {
		t.Errorf("String() = %q, want (none)", got)
	}
}

func TestRedact(t *testing.T) {
	cases := map[string]string{
		"":                          "(none)",
		"   ":                       "(none)",
		"short":                     "…",
		"eleven-chrs":               "…",
		realKey:                     "sk-or-v1-…cdef",
		"sk-ant-api03-abcdef012345": "sk-ant-…2345",
	}
	for in, want := range cases {
		if got := Redact(in); got != want {
			t.Errorf("Redact(%q) = %q, want %q", in, got, want)
		}
	}
}

// `echo $OPENROUTER_API_KEY` is a command a model will eventually run, and its
// output goes straight into a session transcript.
func TestScrubFindsKeysInArbitraryText(t *testing.T) {
	cases := []struct {
		name string
		text string
		leak string
	}{
		{"openrouter", "the key is " + realKey + " ok", realKey},
		{"anthropic", "ANTHROPIC_API_KEY=sk-ant-api03-abcdefghijklmnop0123", "sk-ant-api03-abcdefghijklmnop0123"},
		{"openai", "using sk-abcdefghijklmnopqrstuvwxyz01", "sk-abcdefghijklmnopqrstuvwxyz01"},
		{"groq", "gsk_abcdefghijklmnopqrstuvwxyz01", "gsk_abcdefghijklmnopqrstuvwxyz01"},
		{"google", "key=AIzaSyA0123456789abcdefghijklmnopqrstuv", "AIzaSyA0123456789abcdefghijklmnopqrstuv"},
		{"github", "ghp_0123456789abcdefghijklmnopqrstuvwxyz", "ghp_0123456789abcdefghijklmnopqrstuvwxyz"},
		{"bearer header", "Authorization: Bearer abcdefghijklmnopqrstuvwxyz0123", "abcdefghijklmnopqrstuvwxyz0123"},
		{"in json", `{"api_key":"` + realKey + `"}`, realKey},
		{"in a curl command", "curl -H 'Authorization: Bearer " + realKey + "' https://x", realKey},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Scrub(c.text)
			if strings.Contains(got, c.leak) {
				t.Errorf("Scrub left the key in place: %s", got)
			}
			if got == c.text {
				t.Errorf("Scrub changed nothing: %s", got)
			}
		})
	}
}

func TestScrubLeavesOrdinaryTextAlone(t *testing.T) {
	for _, text := range []string{
		"", "hello world",
		"func main() { fmt.Println(\"sk\") }",
		"the file is sk-notakey.txt",
		"git commit -m 'fix the bearer token handling'",
	} {
		if got := Scrub(text); got != text {
			t.Errorf("Scrub(%q) = %q, it should have changed nothing", text, got)
		}
	}
}

func TestScrubError(t *testing.T) {
	base := fmt.Errorf("HTTP 401: {\"error\":\"bad key %s\"}", realKey)
	wrapped := ScrubError(base)

	if strings.Contains(wrapped.Error(), realKey) {
		t.Errorf("ScrubError leaked the key: %v", wrapped)
	}
	if !errors.Is(wrapped, base) {
		t.Error("ScrubError must stay unwrappable, or errors.Is stops working above it")
	}
	if ScrubError(nil) != nil {
		t.Error("ScrubError(nil) must be nil")
	}
}

// The leak that defeats every other precaution here: a Secret redacts
// perfectly right up until it becomes an http.Header, which is a plain map.
func TestAuthTransportKeepsTheTokenOutOfTheCallersRequest(t *testing.T) {
	var seen string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("Authorization")
	}))
	defer srv.Close()

	client := &http.Client{Transport: &AuthTransport{
		Token: New(realKey),
		Extra: map[string]string{"X-Title": "kolk"},
	}}

	req, err := http.NewRequest(http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	// The server got it...
	if seen != "Bearer "+realKey {
		t.Errorf("the server received %q; the token did not arrive", Redact(seen))
	}
	// ...but the request the caller holds, and might print, never had it.
	for _, dump := range []string{
		fmt.Sprintf("%v", req),
		fmt.Sprintf("%+v", req),
		fmt.Sprintf("%#v", req.Header),
		fmt.Sprintf("%v", req.Header),
	} {
		if strings.Contains(dump, realKey) {
			t.Errorf("printing the caller's request leaked the key: %s", dump)
		}
	}
	if req.Header.Get("Authorization") != "" {
		t.Error("AuthTransport mutated the caller's request instead of a clone")
	}
	// And the transport itself is printable.
	if got := fmt.Sprintf("%+v", client.Transport); strings.Contains(got, realKey) {
		t.Errorf("printing the transport leaked the key: %s", got)
	}
}

func TestAuthTransportWithNoTokenSendsNoHeader(t *testing.T) {
	var had bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, had = r.Header["Authorization"]
	}))
	defer srv.Close()

	client := &http.Client{Transport: &AuthTransport{}}
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if had {
		t.Error("an empty token still sent an Authorization header")
	}
}
