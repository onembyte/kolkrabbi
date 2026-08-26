package provider

import "testing"

func TestPlanModelsFilterAndEfforts(t *testing.T) {
	got := PlanModels("gemini")
	if len(got) == 0 {
		t.Fatal("gemini plan models should be present")
	}
	for _, model := range got {
		if model.Provider != "google" {
			t.Errorf("filter returned unrelated provider: %+v", model)
		}
		if len(model.Efforts) == 0 {
			t.Errorf("model has no effort metadata: %+v", model)
		}
	}
}

func TestPlanModelsMetadataIsComplete(t *testing.T) {
	for _, model := range PlanModels("") {
		if model.Provider == "" || model.Plan == "" || model.Connector == "" ||
			model.Model == "" || model.Access == "" || len(model.Efforts) == 0 {
			t.Errorf("incomplete plan model metadata: %+v", model)
		}
	}
}
