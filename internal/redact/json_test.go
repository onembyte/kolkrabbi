package redact

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestScrubJSONCatchesPlainAndEscapedCanariesInValuesAndKeys(t *testing.T) {
	apiKey := "sk-ant-api03-abcdefghijklmnopqrstuvwxyz0123456789"
	escapedKey := `sk\u002dant\u002dapi03\u002dabcdefghijklmnopqrstuvwxyz0123456789`
	bearerToken := "Bearer abcdefghijklmnopqrstuvwxyz0123456789"

	input := `{
  "secret_value": "` + apiKey + `",
  "` + apiKey + `": "key_was_secret",
  "escaped": "` + escapedKey + `",
  "nested": {
    "list": [
      123.456000,
      true,
      null,
      "` + bearerToken + `"
    ]
  },
  "safe_number": 1e-05,
  "safe_text": "hello world"
}`

	scrubbed, err := ScrubJSON([]byte(input))
	if err != nil {
		t.Fatalf("ScrubJSON failed unexpectedly: %v", err)
	}

	if !json.Valid(scrubbed) {
		t.Fatalf("ScrubJSON produced invalid JSON: %s", string(scrubbed))
	}

	scrubbedStr := string(scrubbed)
	if strings.Contains(scrubbedStr, apiKey) {
		t.Fatalf("ScrubJSON leaked raw API key: %s", scrubbedStr)
	}
	if strings.Contains(scrubbedStr, bearerToken) {
		t.Fatalf("ScrubJSON leaked Bearer token: %s", scrubbedStr)
	}

	// Verify preservation of safe numbers, booleans, nulls, text, and structure
	if !strings.Contains(scrubbedStr, "123.456000") {
		t.Fatalf("ScrubJSON altered float formatting: %s", scrubbedStr)
	}
	if !strings.Contains(scrubbedStr, "1e-05") {
		t.Fatalf("ScrubJSON altered scientific notation: %s", scrubbedStr)
	}
	if !strings.Contains(scrubbedStr, "true") || !strings.Contains(scrubbedStr, "null") {
		t.Fatalf("ScrubJSON altered boolean/null values: %s", scrubbedStr)
	}
	if !strings.Contains(scrubbedStr, `"safe_text": "hello world"`) {
		t.Fatalf("ScrubJSON altered safe string: %s", scrubbedStr)
	}

	// Verify idempotency
	reScrubbed, err := ScrubJSON(scrubbed)
	if err != nil {
		t.Fatalf("Second ScrubJSON failed: %v", err)
	}
	if !bytes.Equal(scrubbed, reScrubbed) {
		t.Fatalf("ScrubJSON is not idempotent:\nFirst:  %s\nSecond: %s", string(scrubbed), string(reScrubbed))
	}
}

func TestScrubJSONPreservesUntouchedBytesWhenNoSecretPresent(t *testing.T) {
	input := "{\n  \"key\": \"value\",\n  \"count\": 10,\n  \"nested\": {\"ok\": true}\n}\n"
	scrubbed, err := ScrubJSON([]byte(input))
	if err != nil {
		t.Fatalf("ScrubJSON failed: %v", err)
	}
	if string(scrubbed) != input {
		t.Fatalf("ScrubJSON altered harmless JSON:\nExpected: %q\nGot:      %q", input, string(scrubbed))
	}
}

func TestScrubJSONFailsClosedOnMalformedAndNonObjectInput(t *testing.T) {
	invalidCases := map[string]string{
		"empty":        "",
		"whitespace":   "   \n\t  ",
		"malformed":    `{"unclosed": "string`,
		"bad syntax":   `{key: "no quotes"}`,
		"number":       `42`,
		"boolean":      `true`,
		"null":         `null`,
		"plain string": `"sk-ant-api03-abcdefghijklmnopqrstuvwxyz0123456789"`,
		"array":        `["sk-ant-api03-abcdefghijklmnopqrstuvwxyz0123456789"]`,
	}

	for name, input := range invalidCases {
		t.Run(name, func(t *testing.T) {
			_, err := ScrubJSON([]byte(input))
			if err == nil {
				t.Fatalf("ScrubJSON should fail on %s input %q, but succeeded", name, input)
			}
		})
	}
}

func FuzzScrubJSON(f *testing.F) {
	f.Add([]byte(`{"key": "value"}`))
	f.Add([]byte(`{"key": "sk-ant-api03-abcdefghijklmnopqrstuvwxyz0123456789"}`))
	f.Add([]byte(`{"nested": {"a": [1, true, null, "Bearer abcdefghijklmnopqrstuvwxyz0123456789"]}}`))
	f.Add([]byte(`not json`))
	f.Add([]byte(`{"unclosed": "string`))
	f.Add([]byte(``))

	f.Fuzz(func(t *testing.T, data []byte) {
		scrubbed, err := ScrubJSON(data)
		if err != nil {
			return
		}
		if !json.Valid(scrubbed) {
			t.Fatalf("ScrubJSON produced invalid JSON from %q: %q", string(data), string(scrubbed))
		}
		// Idempotency: Scrubbing again must produce the exact same JSON bytes
		reScrubbed, err := ScrubJSON(scrubbed)
		if err != nil {
			t.Fatalf("Second ScrubJSON failed on valid output: %v", err)
		}
		if !bytes.Equal(scrubbed, reScrubbed) {
			t.Fatalf("ScrubJSON not idempotent:\n1st: %q\n2nd: %q", string(scrubbed), string(reScrubbed))
		}
	})
}
