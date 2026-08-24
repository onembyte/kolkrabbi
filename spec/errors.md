# Kolkrabbi error mapping

This is the single public mapping from a stable error code to Kolkrabbi's HTTP response status,
shell exit code, and default retryability. The HTTP column describes Kolkrabbi's own transport
response, not a status copied from an upstream provider. Retryable means that replaying the same
operation is safe by default before content is committed; the runtime may still stop after content,
cancellation, an exhausted attempt budget, or a delay outside policy.

The wire entity contains only `code`, a safe non-empty `message`, optional positive
`retry_after_ms`, and an optional non-empty `remedy`. It does not repeat the three derived policy
columns and never carries raw provider bodies or stack traces.

| Code | HTTP | Exit | Retryable | Meaning |
|---|---:|---:|:---:|---|
| `unknown` | 500 | 1 | true | Unclassified failure. |
| `invalid_argument` | 400 | 2 | false | The Kolkrabbi command or request was invalid. |
| `credential_required` | 401 | 2 | false | Setup must add a credential before work can begin. |
| `cancelled` | 499 | 130 | false | The caller cancelled the operation; 499 is the explicit client-closed convention. |
| `stalled` | 504 | 1 | true | Kolkrabbi's provider idle watchdog fired. |
| `authentication_failed` | 401 | 1 | false | A supplied provider credential was rejected. |
| `permission_denied` | 403 | 1 | false | A provider or policy denied the requested operation. |
| `credits_exhausted` | 402 | 1 | false | Provider credits are insufficient. |
| `rate_limited` | 429 | 1 | true | A temporary request or endpoint limit was reached. |
| `quota_exhausted` | 429 | 1 | false | An account-wide quota cannot be fixed by immediate replay or peer rotation. |
| `provider_overloaded` | 503 | 1 | true | The selected provider is temporarily overloaded. |
| `provider_unavailable` | 502 | 1 | true | The selected upstream provider is unavailable. |
| `timeout` | 504 | 1 | true | The provider operation timed out. |
| `transport` | 502 | 1 | true | The provider connection failed. |
| `context_overflow` | 413 | 1 | false | The request context does not fit. |
| `output_limit` | 422 | 1 | false | The requested output limit prevented completion. |
| `truncated` | 502 | 1 | true | The provider stream ended without a valid terminal result. |
| `model_not_found` | 404 | 1 | false | The requested model does not exist. |
| `no_endpoints` | 503 | 1 | false | No endpoint can serve the requested model and constraints. |
| `invalid_request` | 500 | 1 | false | Kolkrabbi generated an invalid upstream request; this is not blamed on the client. |
| `moderation` | 403 | 1 | false | A content policy blocked the operation. |
| `refusal` | 422 | 1 | false | The model refused the operation. |
| `tools_unsupported` | 422 | 1 | false | The selected endpoint cannot execute the required tools. |
| `budget_exhausted` | 429 | 3 | false | A configured Kolkrabbi run budget or step limit was reached. |
| `backend_missing` | 503 | 1 | false | A configured local subscription backend is not installed. |
| `backend_login_required` | 401 | 1 | false | A local subscription backend requires sign-in. |
| `server_error` | 500 | 1 | true | Kolkrabbi or its upstream provider failed unexpectedly. |
| `cursor_expired` | 410 | 1 | false | The requested event cursor is older than the retained replay window. |
