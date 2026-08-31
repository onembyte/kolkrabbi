package protocol

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkUpdatedAcceptsMainAndSubagentSteps(t *testing.T) {
	tests := []WorkUpdatedData{
		{
			ID: "t_01ARYZ6S41TSV4RRFFQ69G5FAW", Role: WorkRoleMain,
			State: WorkStateWorking, Phase: WorkPhasePlanning,
			Step: "decomposing the request", Sequence: 1,
			Model: "gpt-5.6-luna", Effort: "high",
		},
		{
			ID: "k_01ARYZ6S41TSV4RRFFQ69G5FAX", ChildTurn: "t_01ARYZ6S41TSV4RRFFQ69G5FAY",
			Role: WorkRoleSubagent, State: WorkStateWaiting, Phase: WorkPhaseSchedule,
			Step: "waiting for task 1", Sequence: 2, Index: 2, Total: 3,
			Model: "gpt-5.6-luna", Effort: "medium",
		},
	}
	for _, data := range tests {
		raw, err := json.Marshal(data)
		if err != nil {
			t.Fatal(err)
		}
		envelope, err := Decode(workUpdatedFrame(raw))
		if err != nil {
			t.Fatalf("Decode(%+v): %v", data, err)
		}
		if envelope.Type != EventWorkUpdated {
			t.Fatalf("event type = %q, want %q", envelope.Type, EventWorkUpdated)
		}
	}
}

func TestWorkUpdatedRejectsInvalidVocabularyCorrelationAndCoordinates(t *testing.T) {
	valid := map[string]any{
		"id": "k_01ARYZ6S41TSV4RRFFQ69G5FAX", "child_turn": "t_01ARYZ6S41TSV4RRFFQ69G5FAY",
		"role": WorkRoleSubagent, "state": WorkStateWorking, "phase": WorkPhaseProvider,
		"step": "asking the model", "sequence": 3, "index": 1, "total": 2,
	}
	tests := map[string]func(map[string]any){
		"unknown role":       func(data map[string]any) { data["role"] = "manager" },
		"unknown state":      func(data map[string]any) { data["state"] = "almost" },
		"unknown phase":      func(data map[string]any) { data["phase"] = "mystery" },
		"zero sequence":      func(data map[string]any) { data["sequence"] = 0 },
		"empty step":         func(data map[string]any) { data["step"] = "" },
		"task id for main":   func(data map[string]any) { data["role"] = WorkRoleMain },
		"missing child turn": func(data map[string]any) { delete(data, "child_turn") },
		"zero index":         func(data map[string]any) { data["index"] = 0 },
		"index above total":  func(data map[string]any) { data["index"] = 3 },
		"terminal phase": func(data map[string]any) {
			data["state"] = WorkStateDone
		},
		"complete while active": func(data map[string]any) {
			data["phase"] = WorkPhaseComplete
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			data := make(map[string]any, len(valid))
			for key, value := range valid {
				data[key] = value
			}
			mutate(data)
			raw, err := json.Marshal(data)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := Decode(workUpdatedFrame(raw)); err == nil {
				t.Fatalf("Decode accepted invalid payload: %s", raw)
			}
		})
	}
}

func TestWorkUpdatedMainMustNotPretendToBeAChild(t *testing.T) {
	raw := []byte(`{"id":"t_01ARYZ6S41TSV4RRFFQ69G5FAW","child_turn":"t_01ARYZ6S41TSV4RRFFQ69G5FAY","role":"main","state":"working","phase":"planning","step":"planning","sequence":1}`)
	if _, err := Decode(workUpdatedFrame(raw)); err == nil {
		t.Fatal("Decode accepted child-only correlation on main work")
	}
}

func TestWorkUpdatedGoldenDecodes(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "spec", "testdata", "events", "work.updated.json"))
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if envelope.Type != EventWorkUpdated {
		t.Fatalf("golden type = %q, want %q", envelope.Type, EventWorkUpdated)
	}
	var data WorkUpdatedData
	if err := json.Unmarshal(envelope.Data, &data); err != nil {
		t.Fatal(err)
	}
	if data.Role != WorkRoleSubagent || data.Sequence != 4 || !strings.Contains(data.Step, "tests") {
		t.Fatalf("golden data = %+v", data)
	}
}

func workUpdatedFrame(data []byte) []byte {
	return []byte(`{"seq":804,"ts":"2026-08-23T22:01:03Z","session":"s_01ARYZ6S41TSV4RRFFQ69G5FAV","turn":"t_01ARYZ6S41TSV4RRFFQ69G5FAW","type":"work.updated","data":` + string(data) + `}`)
}
