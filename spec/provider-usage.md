# Provider usage mapping

`usage.reported` carries one row for one model within one physical provider attempt. The event
envelope supplies `session`, `turn`, and the report timestamp; payloads do not repeat them.

| Protocol field | Producer field | Presence |
|---|---|---|
| `model` | `provider.Usage.Model` | required |
| `provider_name` | `provider.Usage.ProviderName` | required |
| `request_model` | attempt request model | required |
| `response_model` | attempt response model | optional |
| `input_tokens` | `provider.Usage.InputTokens` | optional; omitted is unknown |
| `cache_read_tokens` | `provider.Usage.CachedInputTokens` | optional; omitted is unknown |
| `cache_write_tokens` | `provider.Usage.CacheWriteTokens` | optional; omitted is unknown |
| `output_tokens` | `provider.Usage.OutputTokens` | optional; omitted is unknown |
| `reasoning_tokens` | `provider.Usage.ReasoningTokens` | optional; omitted is unknown |
| `cost_usd` | `provider.Usage.CostUSD` | optional according to `cost_source` |
| `cost_source` | `provider.Usage.CostSource` | required string projection |
| `measurement` | `provider.Usage.Measurement` | required string projection |
| `ttft_ms` | `provider.Usage.TTFT` in milliseconds | optional; omitted is unknown |
| `finish_reason` | attempt normalized finish reason | optional |
| `error_type` | attempt normalized provider error kind | optional |
| `gen_id` | `provider.Usage.GenID` | optional |
| `attempt` | engine physical-attempt ordinal | required, 1-based |
| `role` | engine call role | required |
| `effort` | resolved effort | required |

Optional numeric fields use presence, not a sentinel: omitted means unknown and an explicit zero is
a measured zero. `cost_source: unknown` requires omitted `cost_usd`; `cost_source: free` requires
an explicit zero; all other cost sources require an explicit non-negative value.
