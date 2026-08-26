package redact

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

// ErrInvalidJSON is returned when input to ScrubJSON is not a valid JSON object.
var ErrInvalidJSON = errors.New("redact: invalid JSON object")

// ScrubJSON decodes and scrubs every string token inside a JSON object payload,
// replacing matching secrets with process-salted sentinels while preserving
// untouched outer bytes, whitespace, formatting, and non-string types exactly.
// It fails closed on empty, malformed, or non-object JSON.
func ScrubJSON(data []byte) ([]byte, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '{' || trimmed[len(trimmed)-1] != '}' || !json.Valid(trimmed) {
		return nil, ErrInvalidJSON
	}

	var out bytes.Buffer
	last := 0
	for i := 0; i < len(data); {
		if data[i] == '"' {
			start := i
			i++
			escaped := false
			for i < len(data) {
				if escaped {
					escaped = false
					i++
					continue
				}
				if data[i] == '\\' {
					escaped = true
					i++
					continue
				}
				if data[i] == '"' {
					break
				}
				i++
			}
			if i >= len(data) {
				return nil, ErrInvalidJSON
			}
			end := i + 1
			i++

			rawToken := data[start:end]
			var decoded string
			if err := json.Unmarshal(rawToken, &decoded); err != nil {
				return nil, fmt.Errorf("redact: unmarshal JSON string: %w", err)
			}
			scrubbed := Scrub(decoded)
			if scrubbed != decoded {
				replacement, err := json.Marshal(scrubbed)
				if err != nil {
					return nil, fmt.Errorf("redact: marshal scrubbed string: %w", err)
				}
				if out.Cap() == 0 {
					out.Grow(len(data))
				}
				out.Write(data[last:start])
				out.Write(replacement)
				last = end
			}
		} else {
			i++
		}
	}

	if out.Cap() == 0 {
		return bytes.Clone(data), nil
	}
	out.Write(data[last:])
	result := out.Bytes()
	if !json.Valid(result) {
		return nil, ErrInvalidJSON
	}
	return result, nil
}
