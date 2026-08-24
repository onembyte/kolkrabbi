package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type errorPolicyExpectation struct {
	code      ErrorCode
	http      int
	exit      int
	retryable bool
}

func TestErrorEntityMatchesSchemaGoldenAndMapping(t *testing.T) {
	assertErrorSchema(t)
	assertErrorMappingDocument(t)

	wantJSON, err := os.ReadFile(filepath.Join("..", "spec", "testdata", "entities", "error.json"))
	if err != nil {
		t.Fatal(err)
	}
	wantJSON = bytes.TrimSpace(wantJSON)
	if err := validateErrorEntity(wantJSON); err != nil {
		t.Fatalf("validateErrorEntity(golden): %v", err)
	}

	var got Error
	if err := json.Unmarshal(wantJSON, &got); err != nil {
		t.Fatal(err)
	}
	want := Error{
		Code: ErrorCodeRateLimited, Message: "provider capacity is temporarily limited",
		RetryAfterMilliseconds: int64Pointer(4000),
		Remedy:                 "retry later or select another model with /model",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("typed entity = %#v, want %#v", got, want)
	}
	if got.Error() != want.Message {
		t.Errorf("Error() = %q, want safe message %q", got.Error(), want.Message)
	}
	if got.HTTPStatus() != 429 || got.ExitCode() != 1 || !got.Retryable() {
		t.Errorf("golden policy = HTTP %d, exit %d, retryable %t", got.HTTPStatus(), got.ExitCode(), got.Retryable())
	}
	roundTrip, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(roundTrip, wantJSON) {
		t.Errorf("round trip drifted\n got: %s\nwant: %s", roundTrip, wantJSON)
	}
}

func TestErrorPolicyIsExhaustiveAndFailsClosed(t *testing.T) {
	want := errorPolicyExpectations()
	wantCodes := make([]ErrorCode, 0, len(want))
	seen := make(map[ErrorCode]struct{}, len(want))
	for _, row := range want {
		if _, duplicate := seen[row.code]; duplicate {
			t.Fatalf("duplicate expected policy for %q", row.code)
		}
		seen[row.code] = struct{}{}
		wantCodes = append(wantCodes, row.code)
		if !validErrorCode(row.code) {
			t.Errorf("validErrorCode(%q) = false", row.code)
		}
		if got := row.code.HTTPStatus(); got != row.http {
			t.Errorf("%s HTTP status = %d, want %d", row.code, got, row.http)
		}
		if got := row.code.ExitCode(); got != row.exit {
			t.Errorf("%s exit code = %d, want %d", row.code, got, row.exit)
		}
		if got := row.code.Retryable(); got != row.retryable {
			t.Errorf("%s retryable = %t, want %t", row.code, got, row.retryable)
		}
	}
	if got := allErrorCodes(); !reflect.DeepEqual(got, wantCodes) {
		t.Errorf("error codes = %v, want %v", got, wantCodes)
	}
	if len(errorPolicies) != len(want) {
		t.Errorf("error policy rows = %d, want %d", len(errorPolicies), len(want))
	}

	invalid := ErrorCode("future_unregistered_failure")
	if validErrorCode(invalid) || invalid.HTTPStatus() != 500 || invalid.ExitCode() != 1 || invalid.Retryable() {
		t.Errorf("invalid code did not fail closed: HTTP %d, exit %d, retryable %t",
			invalid.HTTPStatus(), invalid.ExitCode(), invalid.Retryable())
	}
}

func TestErrorEntityRejectsInvalidRequiredFields(t *testing.T) {
	for _, field := range []string{"code", "message"} {
		for name, value := range map[string]any{
			"empty": "", "null": nil, "non-string": 1,
		} {
			t.Run(field+"/"+name, func(t *testing.T) {
				data := validErrorEntity()
				data[field] = value
				if err := validateErrorEntity(errorEntityJSON(t, data)); err == nil {
					t.Errorf("accepted invalid %s: %#v", field, data)
				}
			})
		}
		t.Run(field+"/missing", func(t *testing.T) {
			data := validErrorEntity()
			delete(data, field)
			if err := validateErrorEntity(errorEntityJSON(t, data)); err == nil {
				t.Errorf("accepted missing %s", field)
			}
		})
	}

	data := validErrorEntity()
	data["code"] = "future_unregistered_failure"
	if err := validateErrorEntity(errorEntityJSON(t, data)); err == nil {
		t.Error("accepted unknown error code")
	}
}

func TestErrorEntityOptionalFieldsValidateWhenPresent(t *testing.T) {
	for name, value := range map[string]any{
		"zero": 0, "negative": -1, "fractional": 1.5, "string": "1", "null": nil,
	} {
		t.Run("retry_after_ms/"+name, func(t *testing.T) {
			data := validErrorEntity()
			data["retry_after_ms"] = value
			if err := validateErrorEntity(errorEntityJSON(t, data)); err == nil {
				t.Errorf("accepted invalid retry delay: %#v", data)
			}
		})
	}

	for name, value := range map[string]any{"empty": "", "non-string": 1, "null": nil} {
		t.Run("remedy/"+name, func(t *testing.T) {
			data := validErrorEntity()
			data["remedy"] = value
			if err := validateErrorEntity(errorEntityJSON(t, data)); err == nil {
				t.Errorf("accepted invalid remedy: %#v", data)
			}
		})
	}

	for _, field := range []string{"retry_after_ms", "remedy"} {
		t.Run(field+"/omitted", func(t *testing.T) {
			data := validErrorEntity()
			delete(data, field)
			if err := validateErrorEntity(errorEntityJSON(t, data)); err != nil {
				t.Fatalf("rejected omitted %s: %v", field, err)
			}
		})
	}

	data := validErrorEntity()
	data["future"] = map[string]any{"kept": true}
	if err := validateErrorEntity(errorEntityJSON(t, data)); err != nil {
		t.Fatalf("rejected additive field: %v", err)
	}
}

func errorPolicyExpectations() []errorPolicyExpectation {
	return []errorPolicyExpectation{
		{ErrorCodeUnknown, 500, 1, true},
		{ErrorCodeInvalidArgument, 400, 2, false},
		{ErrorCodeCredentialRequired, 401, 2, false},
		{ErrorCodeCancelled, 499, 130, false},
		{ErrorCodeStalled, 504, 1, true},
		{ErrorCodeAuthenticationFailed, 401, 1, false},
		{ErrorCodePermissionDenied, 403, 1, false},
		{ErrorCodeCreditsExhausted, 402, 1, false},
		{ErrorCodeRateLimited, 429, 1, true},
		{ErrorCodeQuotaExhausted, 429, 1, false},
		{ErrorCodeProviderOverloaded, 503, 1, true},
		{ErrorCodeProviderUnavailable, 502, 1, true},
		{ErrorCodeTimeout, 504, 1, true},
		{ErrorCodeTransport, 502, 1, true},
		{ErrorCodeContextOverflow, 413, 1, false},
		{ErrorCodeOutputLimit, 422, 1, false},
		{ErrorCodeTruncated, 502, 1, true},
		{ErrorCodeModelNotFound, 404, 1, false},
		{ErrorCodeNoEndpoints, 503, 1, false},
		{ErrorCodeInvalidRequest, 500, 1, false},
		{ErrorCodeModeration, 403, 1, false},
		{ErrorCodeRefusal, 422, 1, false},
		{ErrorCodeToolsUnsupported, 422, 1, false},
		{ErrorCodeBudgetExhausted, 429, 3, false},
		{ErrorCodeBackendMissing, 503, 1, false},
		{ErrorCodeBackendLoginRequired, 401, 1, false},
		{ErrorCodeServer, 500, 1, true},
		{ErrorCodeCursorExpired, 410, 1, false},
	}
}

func validErrorEntity() map[string]any {
	return map[string]any{
		"code": ErrorCodeRateLimited, "message": "provider capacity is temporarily limited",
		"retry_after_ms": 4000, "remedy": "retry later or select another model with /model",
	}
}

func errorEntityJSON(t *testing.T, data map[string]any) []byte {
	t.Helper()
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func assertErrorMappingDocument(t *testing.T) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "spec", "errors.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	rows := 0
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "| `") {
			rows++
		}
	}
	if rows != len(errorPolicyExpectations()) {
		t.Fatalf("mapping rows = %d, want %d", rows, len(errorPolicyExpectations()))
	}
	for _, row := range errorPolicyExpectations() {
		prefix := fmt.Sprintf("| `%s` | %d | %d | %t |", row.code, row.http, row.exit, row.retryable)
		if strings.Count(text, prefix) != 1 {
			t.Errorf("mapping must contain exactly one row beginning %q", prefix)
		}
	}
}

func assertErrorSchema(t *testing.T) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "spec", "schemas", "entities", "error.json"))
	if err != nil {
		t.Fatal(err)
	}
	type property struct {
		Type      string   `json:"type"`
		MinLength *int     `json:"minLength"`
		Minimum   *int64   `json:"minimum"`
		Enum      []string `json:"enum"`
	}
	var schema struct {
		Dialect              string              `json:"$schema"`
		ID                   string              `json:"$id"`
		Title                string              `json:"title"`
		Type                 string              `json:"type"`
		Required             []string            `json:"required"`
		Properties           map[string]property `json:"properties"`
		AdditionalProperties bool                `json:"additionalProperties"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("schema is not JSON: %v", err)
	}
	wantID := "https://kolkrabbi.francomichetti.com/spec/0/schemas/entities/error.json"
	if schema.Dialect != "https://json-schema.org/draft/2020-12/schema" || schema.ID != wantID ||
		schema.Title != "error entity" || schema.Type != "object" || !schema.AdditionalProperties {
		t.Error("schema root does not define the forward-compatible error entity")
	}
	if !reflect.DeepEqual(schema.Required, []string{"code", "message"}) || len(schema.Properties) != 4 {
		t.Errorf("schema fields = required %v, properties %v", schema.Required, schema.Properties)
	}
	wantCodes := make([]string, 0, len(errorPolicyExpectations()))
	for _, row := range errorPolicyExpectations() {
		wantCodes = append(wantCodes, string(row.code))
	}
	if got := schema.Properties["code"]; got.Type != "string" || !reflect.DeepEqual(got.Enum, wantCodes) {
		t.Errorf("code schema = %#v", got)
	}
	for _, field := range []string{"message", "remedy"} {
		got := schema.Properties[field]
		if got.Type != "string" || got.MinLength == nil || *got.MinLength != 1 {
			t.Errorf("%s schema = %#v, want non-empty string", field, got)
		}
	}
	if got := schema.Properties["retry_after_ms"]; got.Type != "integer" || got.Minimum == nil || *got.Minimum != 1 {
		t.Errorf("retry_after_ms schema = %#v, want positive integer", got)
	}
}
