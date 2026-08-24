package protocol

import (
	"encoding/json"
	"fmt"
)

// ErrorCode is Kolkrabbi's closed, provider-independent failure vocabulary.
type ErrorCode string

const (
	ErrorCodeUnknown              ErrorCode = "unknown"
	ErrorCodeInvalidArgument      ErrorCode = "invalid_argument"
	ErrorCodeCredentialRequired   ErrorCode = "credential_required"
	ErrorCodeCancelled            ErrorCode = "cancelled"
	ErrorCodeStalled              ErrorCode = "stalled"
	ErrorCodeAuthenticationFailed ErrorCode = "authentication_failed"
	ErrorCodePermissionDenied     ErrorCode = "permission_denied"
	ErrorCodeCreditsExhausted     ErrorCode = "credits_exhausted"
	ErrorCodeRateLimited          ErrorCode = "rate_limited"
	ErrorCodeQuotaExhausted       ErrorCode = "quota_exhausted"
	ErrorCodeProviderOverloaded   ErrorCode = "provider_overloaded"
	ErrorCodeProviderUnavailable  ErrorCode = "provider_unavailable"
	ErrorCodeTimeout              ErrorCode = "timeout"
	ErrorCodeTransport            ErrorCode = "transport"
	ErrorCodeContextOverflow      ErrorCode = "context_overflow"
	ErrorCodeOutputLimit          ErrorCode = "output_limit"
	ErrorCodeTruncated            ErrorCode = "truncated"
	ErrorCodeModelNotFound        ErrorCode = "model_not_found"
	ErrorCodeNoEndpoints          ErrorCode = "no_endpoints"
	ErrorCodeInvalidRequest       ErrorCode = "invalid_request"
	ErrorCodeModeration           ErrorCode = "moderation"
	ErrorCodeRefusal              ErrorCode = "refusal"
	ErrorCodeToolsUnsupported     ErrorCode = "tools_unsupported"
	ErrorCodeBudgetExhausted      ErrorCode = "budget_exhausted"
	ErrorCodeBackendMissing       ErrorCode = "backend_missing"
	ErrorCodeBackendLoginRequired ErrorCode = "backend_login_required"
	ErrorCodeServer               ErrorCode = "server_error"
	ErrorCodeCursorExpired        ErrorCode = "cursor_expired"
)

var errorCodes = []ErrorCode{
	ErrorCodeUnknown,
	ErrorCodeInvalidArgument,
	ErrorCodeCredentialRequired,
	ErrorCodeCancelled,
	ErrorCodeStalled,
	ErrorCodeAuthenticationFailed,
	ErrorCodePermissionDenied,
	ErrorCodeCreditsExhausted,
	ErrorCodeRateLimited,
	ErrorCodeQuotaExhausted,
	ErrorCodeProviderOverloaded,
	ErrorCodeProviderUnavailable,
	ErrorCodeTimeout,
	ErrorCodeTransport,
	ErrorCodeContextOverflow,
	ErrorCodeOutputLimit,
	ErrorCodeTruncated,
	ErrorCodeModelNotFound,
	ErrorCodeNoEndpoints,
	ErrorCodeInvalidRequest,
	ErrorCodeModeration,
	ErrorCodeRefusal,
	ErrorCodeToolsUnsupported,
	ErrorCodeBudgetExhausted,
	ErrorCodeBackendMissing,
	ErrorCodeBackendLoginRequired,
	ErrorCodeServer,
	ErrorCodeCursorExpired,
}

type errorPolicy struct {
	httpStatus int
	exitCode   int
	retryable  bool
}

var errorPolicies = map[ErrorCode]errorPolicy{
	ErrorCodeUnknown:              {httpStatus: 500, exitCode: 1, retryable: true},
	ErrorCodeInvalidArgument:      {httpStatus: 400, exitCode: 2},
	ErrorCodeCredentialRequired:   {httpStatus: 401, exitCode: 2},
	ErrorCodeCancelled:            {httpStatus: 499, exitCode: 130},
	ErrorCodeStalled:              {httpStatus: 504, exitCode: 1, retryable: true},
	ErrorCodeAuthenticationFailed: {httpStatus: 401, exitCode: 1},
	ErrorCodePermissionDenied:     {httpStatus: 403, exitCode: 1},
	ErrorCodeCreditsExhausted:     {httpStatus: 402, exitCode: 1},
	ErrorCodeRateLimited:          {httpStatus: 429, exitCode: 1, retryable: true},
	ErrorCodeQuotaExhausted:       {httpStatus: 429, exitCode: 1},
	ErrorCodeProviderOverloaded:   {httpStatus: 503, exitCode: 1, retryable: true},
	ErrorCodeProviderUnavailable:  {httpStatus: 502, exitCode: 1, retryable: true},
	ErrorCodeTimeout:              {httpStatus: 504, exitCode: 1, retryable: true},
	ErrorCodeTransport:            {httpStatus: 502, exitCode: 1, retryable: true},
	ErrorCodeContextOverflow:      {httpStatus: 413, exitCode: 1},
	ErrorCodeOutputLimit:          {httpStatus: 422, exitCode: 1},
	ErrorCodeTruncated:            {httpStatus: 502, exitCode: 1, retryable: true},
	ErrorCodeModelNotFound:        {httpStatus: 404, exitCode: 1},
	ErrorCodeNoEndpoints:          {httpStatus: 503, exitCode: 1},
	ErrorCodeInvalidRequest:       {httpStatus: 500, exitCode: 1},
	ErrorCodeModeration:           {httpStatus: 403, exitCode: 1},
	ErrorCodeRefusal:              {httpStatus: 422, exitCode: 1},
	ErrorCodeToolsUnsupported:     {httpStatus: 422, exitCode: 1},
	ErrorCodeBudgetExhausted:      {httpStatus: 429, exitCode: 3},
	ErrorCodeBackendMissing:       {httpStatus: 503, exitCode: 1},
	ErrorCodeBackendLoginRequired: {httpStatus: 401, exitCode: 1},
	ErrorCodeServer:               {httpStatus: 500, exitCode: 1, retryable: true},
	ErrorCodeCursorExpired:        {httpStatus: 410, exitCode: 1},
}

// Error is the safe, language-neutral failure entity. Provider response bodies,
// stack traces, and other private diagnostics never belong in these fields.
type Error struct {
	Code                   ErrorCode `json:"code"`
	Message                string    `json:"message"`
	RetryAfterMilliseconds *int64    `json:"retry_after_ms,omitempty"`
	Remedy                 string    `json:"remedy,omitempty"`
}

// Error implements the Go error interface using only the scrubbed display text.
func (e Error) Error() string { return e.Message }

// HTTPStatus returns Kolkrabbi's transport status for this error code. It does
// not expose or preserve an upstream provider's raw status.
func (e Error) HTTPStatus() int { return e.Code.HTTPStatus() }

// ExitCode returns the stable process exit status for this error code.
func (e Error) ExitCode() int { return e.Code.ExitCode() }

// Retryable reports the code's default pre-commit replay policy.
func (e Error) Retryable() bool { return e.Code.Retryable() }

// HTTPStatus returns Kolkrabbi's transport status for the code. An invalid
// programmatic code fails closed as an internal error.
func (c ErrorCode) HTTPStatus() int { return policyForErrorCode(c).httpStatus }

// ExitCode returns the process exit status for the code. An invalid
// programmatic code fails closed as a generic runtime failure.
func (c ErrorCode) ExitCode() int { return policyForErrorCode(c).exitCode }

// Retryable reports whether replay is safe by default before content commits.
// Invalid programmatic codes are never retried automatically.
func (c ErrorCode) Retryable() bool { return policyForErrorCode(c).retryable }

func policyForErrorCode(code ErrorCode) errorPolicy {
	if policy, ok := errorPolicies[code]; ok {
		return policy
	}
	return errorPolicy{httpStatus: 500, exitCode: 1}
}

func validErrorCode(code ErrorCode) bool {
	_, ok := errorPolicies[code]
	return ok
}

func allErrorCodes() []ErrorCode {
	return append([]ErrorCode(nil), errorCodes...)
}

func validateErrorEntity(raw json.RawMessage) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return fmt.Errorf("protocol: error entity: %w", err)
	}
	var entity Error
	if err := json.Unmarshal(raw, &entity); err != nil {
		return fmt.Errorf("protocol: error entity: %w", err)
	}
	if _, present := fields["code"]; !present || !validErrorCode(entity.Code) {
		return fmt.Errorf("protocol: error entity code is missing or not defined")
	}
	if _, present := fields["message"]; !present || entity.Message == "" {
		return fmt.Errorf("protocol: error entity message must be present and non-empty")
	}
	if _, present := fields["retry_after_ms"]; present {
		if entity.RetryAfterMilliseconds == nil || *entity.RetryAfterMilliseconds < 1 {
			return fmt.Errorf("protocol: error entity retry_after_ms must be a positive integer when present")
		}
	}
	if _, present := fields["remedy"]; present && entity.Remedy == "" {
		return fmt.Errorf("protocol: error entity remedy must be non-empty and string-valued when present")
	}
	return nil
}
