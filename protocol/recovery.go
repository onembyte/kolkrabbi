package protocol

import (
	"encoding/json"
	"fmt"
)

// RecoveryFailedData reports an exceptional write failure. Durable means the
// compressed recovery point exists and only its compatibility mirror failed.
type RecoveryFailedData struct {
	Code    string `json:"code"`
	Reason  string `json:"reason"`
	Message string `json:"message"`
	Durable bool   `json:"durable"`
}

func validateRecoveryFailed(raw json.RawMessage) error {
	var data struct {
		RecoveryFailedData
		Durable *bool `json:"durable"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		return fmt.Errorf("protocol: recovery.failed data: %w", err)
	}
	if data.Code != "recovery_save_failed" || data.Message == "" || data.Durable == nil {
		return fmt.Errorf("protocol: recovery.failed requires code, message and durable")
	}
	switch data.Reason {
	case "pause", "limit", "error", "resume":
		return nil
	}
	return fmt.Errorf("protocol: recovery.failed reason is not defined")
}
