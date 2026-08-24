package protocol

import (
	"encoding/json"
	"fmt"
)

// UsageCostSource identifies how CostUSD was obtained. Unknown and Free are
// distinct so an absent price can never be charted as zero spend.
type UsageCostSource string

const (
	UsageCostUnknown        UsageCostSource = "unknown"
	UsageCostReported       UsageCostSource = "reported"
	UsageCostHeader         UsageCostSource = "header"
	UsageCostFollowup       UsageCostSource = "followup"
	UsageCostPriceTable     UsageCostSource = "price_table"
	UsageCostVendorEstimate UsageCostSource = "vendor_estimate"
	UsageCostFree           UsageCostSource = "free"
)

func validUsageCostSource(source UsageCostSource) bool {
	switch source {
	case UsageCostUnknown, UsageCostReported, UsageCostHeader, UsageCostFollowup,
		UsageCostPriceTable, UsageCostVendorEstimate, UsageCostFree:
		return true
	default:
		return false
	}
}

// UsageMeasurement is the comparability class of one accounting row.
type UsageMeasurement string

const (
	UsageMeasurementUnknown   UsageMeasurement = "unknown"
	UsageMeasurementMetered   UsageMeasurement = "metered"
	UsageMeasurementEstimated UsageMeasurement = "estimated"
	UsageMeasurementLocal     UsageMeasurement = "local"
)

func validUsageMeasurement(measurement UsageMeasurement) bool {
	switch measurement {
	case UsageMeasurementUnknown, UsageMeasurementMetered,
		UsageMeasurementEstimated, UsageMeasurementLocal:
		return true
	default:
		return false
	}
}

// Usage is one accounting row for one model within one physical attempt. Nil
// numeric pointers mean unknown; pointers to zero mean measured zero. Field
// order is wire-significant for the conformance fixture.
type Usage struct {
	Model            string           `json:"model"`
	ProviderName     string           `json:"provider_name"`
	RequestModel     string           `json:"request_model"`
	ResponseModel    string           `json:"response_model,omitempty"`
	InputTokens      *int64           `json:"input_tokens,omitempty"`
	CacheReadTokens  *int64           `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens *int64           `json:"cache_write_tokens,omitempty"`
	OutputTokens     *int64           `json:"output_tokens,omitempty"`
	ReasoningTokens  *int64           `json:"reasoning_tokens,omitempty"`
	CostUSD          *float64         `json:"cost_usd,omitempty"`
	CostSource       UsageCostSource  `json:"cost_source"`
	Measurement      UsageMeasurement `json:"measurement"`
	TTFTMilliseconds *int64           `json:"ttft_ms,omitempty"`
	FinishReason     string           `json:"finish_reason,omitempty"`
	ErrorType        string           `json:"error_type,omitempty"`
	GenID            string           `json:"gen_id,omitempty"`
	Attempt          int              `json:"attempt"`
	Role             string           `json:"role"`
	Effort           string           `json:"effort"`
}

// UsageReportedData is the payload of EventUsageReported. It aliases the
// shared Usage entity so event and non-event transports cannot diverge.
type UsageReportedData = Usage

func validateUsageEntity(raw json.RawMessage) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return fmt.Errorf("protocol: usage entity: %w", err)
	}
	var data Usage
	if err := json.Unmarshal(raw, &data); err != nil {
		return fmt.Errorf("protocol: usage entity: %w", err)
	}
	for _, field := range []struct {
		name  string
		value string
	}{
		{name: "model", value: data.Model},
		{name: "provider_name", value: data.ProviderName},
		{name: "request_model", value: data.RequestModel},
		{name: "role", value: data.Role},
		{name: "effort", value: data.Effort},
	} {
		if field.value == "" {
			return fmt.Errorf("protocol: usage entity %s must be non-empty", field.name)
		}
	}
	for _, field := range []struct {
		name  string
		value string
	}{
		{name: "response_model", value: data.ResponseModel},
		{name: "finish_reason", value: data.FinishReason},
		{name: "error_type", value: data.ErrorType},
		{name: "gen_id", value: data.GenID},
	} {
		if _, present := fields[field.name]; present && field.value == "" {
			return fmt.Errorf("protocol: usage entity %s must be non-empty and string-valued when present", field.name)
		}
	}
	if !validUsageCostSource(data.CostSource) {
		return fmt.Errorf("protocol: usage entity cost_source is not defined")
	}
	if !validUsageMeasurement(data.Measurement) {
		return fmt.Errorf("protocol: usage entity measurement is not defined")
	}
	if data.Attempt < 1 {
		return fmt.Errorf("protocol: usage entity attempt must be at least 1")
	}
	for _, field := range []struct {
		name  string
		value *int64
	}{
		{name: "input_tokens", value: data.InputTokens},
		{name: "cache_read_tokens", value: data.CacheReadTokens},
		{name: "cache_write_tokens", value: data.CacheWriteTokens},
		{name: "output_tokens", value: data.OutputTokens},
		{name: "reasoning_tokens", value: data.ReasoningTokens},
		{name: "ttft_ms", value: data.TTFTMilliseconds},
	} {
		if err := validateOptionalUsageInteger(fields, field.name, field.value); err != nil {
			return err
		}
	}
	_, costPresent := fields["cost_usd"]
	if costPresent && data.CostUSD == nil {
		return fmt.Errorf("protocol: usage entity cost_usd must be numeric when present")
	}
	if data.CostUSD != nil && *data.CostUSD < 0 {
		return fmt.Errorf("protocol: usage entity cost_usd must be non-negative")
	}
	switch data.CostSource {
	case UsageCostUnknown:
		if costPresent {
			return fmt.Errorf("protocol: unknown usage cost must omit cost_usd")
		}
	case UsageCostFree:
		if !costPresent || data.CostUSD == nil || *data.CostUSD != 0 {
			return fmt.Errorf("protocol: free usage cost must report cost_usd as zero")
		}
	default:
		if !costPresent || data.CostUSD == nil {
			return fmt.Errorf("protocol: measured usage cost source must report cost_usd")
		}
	}
	return nil
}

func validateOptionalUsageInteger(
	fields map[string]json.RawMessage,
	name string,
	value *int64,
) error {
	if _, present := fields[name]; !present {
		return nil
	}
	if value == nil {
		return fmt.Errorf("protocol: usage entity %s must be integer-valued when present", name)
	}
	if *value < 0 {
		return fmt.Errorf("protocol: usage entity %s must be non-negative", name)
	}
	return nil
}

// ScoreTargetKind identifies the entity evaluated by a score.
type ScoreTargetKind string

const (
	ScoreTargetSession ScoreTargetKind = "session"
	ScoreTargetTurn    ScoreTargetKind = "turn"
	ScoreTargetSpan    ScoreTargetKind = "span"
)

func validScoreTargetKind(kind ScoreTargetKind) bool {
	return kind == ScoreTargetSession || kind == ScoreTargetTurn || kind == ScoreTargetSpan
}

// ScoreDataType declares the native JSON primitive carried in Value.
type ScoreDataType string

const (
	ScoreDataNumeric     ScoreDataType = "numeric"
	ScoreDataCategorical ScoreDataType = "categorical"
	ScoreDataBoolean     ScoreDataType = "boolean"
	ScoreDataText        ScoreDataType = "text"
)

func validScoreDataType(dataType ScoreDataType) bool {
	switch dataType {
	case ScoreDataNumeric, ScoreDataCategorical, ScoreDataBoolean, ScoreDataText:
		return true
	default:
		return false
	}
}

// ScoreSource identifies who or what produced a score.
type ScoreSource string

const (
	ScoreSourceHuman    ScoreSource = "human"
	ScoreSourceJudge    ScoreSource = "judge"
	ScoreSourceImplicit ScoreSource = "implicit"
)

func validScoreSource(source ScoreSource) bool {
	return source == ScoreSourceHuman || source == ScoreSourceJudge || source == ScoreSourceImplicit
}

// Score is one typed evaluation. Value retains its JSON bytes; DataType
// determines whether it must decode as a number, string, or boolean.
type Score struct {
	ID          string          `json:"id"`
	TargetKind  ScoreTargetKind `json:"target_kind"`
	TargetID    string          `json:"target_id"`
	Name        string          `json:"name"`
	DataType    ScoreDataType   `json:"data_type"`
	Value       json.RawMessage `json:"value"`
	Source      ScoreSource     `json:"source"`
	JudgeModel  string          `json:"judge_model,omitempty"`
	Explanation string          `json:"explanation,omitempty"`
}

// ScoreRecordedData is the payload of EventScoreRecorded. It aliases the
// shared Score entity so event and non-event transports cannot diverge.
type ScoreRecordedData = Score

func validateScoreEntity(raw json.RawMessage) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return fmt.Errorf("protocol: score entity: %w", err)
	}
	var data Score
	if err := json.Unmarshal(raw, &data); err != nil {
		return fmt.Errorf("protocol: score entity: %w", err)
	}
	for _, field := range []struct {
		name  string
		value string
	}{
		{name: "id", value: data.ID},
		{name: "target_id", value: data.TargetID},
		{name: "name", value: data.Name},
	} {
		if field.value == "" {
			return fmt.Errorf("protocol: score entity %s must be non-empty", field.name)
		}
	}
	if !validScoreTargetKind(data.TargetKind) {
		return fmt.Errorf("protocol: score entity target_kind is not defined")
	}
	switch data.TargetKind {
	case ScoreTargetSession:
		if !validID(data.TargetID, 's') {
			return fmt.Errorf("protocol: score entity session target_id must be a canonical s_ ULID")
		}
	case ScoreTargetTurn:
		if !validID(data.TargetID, 't') {
			return fmt.Errorf("protocol: score entity turn target_id must be a canonical t_ ULID")
		}
	case ScoreTargetSpan:
		// Span identity remains opaque until A6.3b6 freezes the shared entity.
	}
	if !validScoreDataType(data.DataType) {
		return fmt.Errorf("protocol: score entity data_type is not defined")
	}
	if err := validateScoreValue(data.DataType, data.Value); err != nil {
		return err
	}
	if !validScoreSource(data.Source) {
		return fmt.Errorf("protocol: score entity source is not defined")
	}
	_, judgeModelPresent := fields["judge_model"]
	if data.Source == ScoreSourceJudge {
		if !judgeModelPresent || data.JudgeModel == "" {
			return fmt.Errorf("protocol: judge score requires non-empty string judge_model")
		}
	} else if judgeModelPresent {
		return fmt.Errorf("protocol: score entity judge_model is only valid for judge scores")
	}
	if _, present := fields["explanation"]; present && data.Explanation == "" {
		return fmt.Errorf("protocol: score entity explanation must be non-empty and string-valued when present")
	}
	return nil
}

func validateScoreValue(dataType ScoreDataType, raw json.RawMessage) error {
	if len(raw) == 0 {
		return fmt.Errorf("protocol: score entity value is required")
	}
	switch dataType {
	case ScoreDataNumeric:
		var value *float64
		if err := json.Unmarshal(raw, &value); err != nil || value == nil {
			return fmt.Errorf("protocol: numeric score value must be a JSON number")
		}
	case ScoreDataCategorical, ScoreDataText:
		var value *string
		if err := json.Unmarshal(raw, &value); err != nil || value == nil || *value == "" {
			return fmt.Errorf("protocol: %s score value must be a non-empty JSON string", dataType)
		}
	case ScoreDataBoolean:
		var value *bool
		if err := json.Unmarshal(raw, &value); err != nil || value == nil {
			return fmt.Errorf("protocol: boolean score value must be a JSON boolean")
		}
	}
	return nil
}
