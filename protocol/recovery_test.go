package protocol

import (
	"strings"
	"testing"
)

func TestRecoveryNoticeRejectsMissingOrFalseGuarantees(t *testing.T) {
	for _, payload := range []string{`{}`, `{"code":"wrong","reason":"error","message":"failed","durable":false}`, `{"code":"recovery_save_failed","reason":"unknown","message":"failed","durable":false}`, `{"code":"recovery_save_failed","reason":"error","message":"","durable":false}`, `{"code":"recovery_save_failed","reason":"error","message":"failed"}`} {
		raw := `{"seq":1,"ts":"2026-09-30T00:00:00Z","session":"` + goldenSession + `","turn":"` + goldenTurn + `","type":"recovery.failed","data":` + payload + `}`
		if _, err := Decode([]byte(raw)); err == nil {
			t.Errorf("accepted invalid recovery notice: %s", payload)
		}
	}
	for _, reason := range []string{"pause", "limit", "error", "resume"} {
		raw := `{"seq":1,"ts":"2026-09-30T00:00:00Z","session":"` + goldenSession + `","turn":"` + goldenTurn + `","type":"recovery.failed","data":{"code":"recovery_save_failed","reason":"` + reason + `","message":"failed","durable":false}}`
		env, err := Decode([]byte(raw))
		if err != nil || !strings.Contains(string(env.Data), reason) {
			t.Errorf("valid %s: %v", reason, err)
		}
	}
}
