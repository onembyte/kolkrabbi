package provider

import (
	"sort"
	"strings"
)

// ModelIsFree returns true if the model is explicitly zero-cost on OpenRouter.
func ModelIsFree(model ModelInfo) bool {
	return model.ID == "openrouter/auto" || strings.HasSuffix(model.ID, ":free")
}

// CodingSuitability scores how oriented a model is toward software engineering.
func CodingSuitability(model ModelInfo) int {
	identity := strings.ToLower(model.ID + " " + model.Name)
	description := strings.ToLower(model.Description)
	score := 0
	for _, signal := range []struct {
		text   string
		weight int
	}{
		{"software engineering", 8},
		{"programming", 7},
		{"coding", 7},
		{"coder", 7},
		{"code", 5},
		{"terminal", 4},
		{"swe-bench", 4},
		{"agentic", 2},
	} {
		if strings.Contains(identity, signal.text) {
			score += signal.weight * 2
		}
		if strings.Contains(description, signal.text) {
			score += signal.weight
		}
	}
	return score
}

type rankedFreeCandidate struct {
	model       ModelInfo
	codingScore int
}

// RankFreeModels filters and ranks available free models by coding suitability
// and context length.
func RankFreeModels(models []ModelInfo) []string {
	var freeCandidates []rankedFreeCandidate
	for _, m := range models {
		if !ModelIsFree(m) {
			continue
		}
		freeCandidates = append(freeCandidates, rankedFreeCandidate{
			model:       m,
			codingScore: CodingSuitability(m),
		})
	}

	sort.Slice(freeCandidates, func(i, j int) bool {
		a, b := freeCandidates[i], freeCandidates[j]
		if a.codingScore != b.codingScore {
			return a.codingScore > b.codingScore
		}
		if a.model.ContextLength != b.model.ContextLength {
			return a.model.ContextLength > b.model.ContextLength
		}
		return a.model.ID < b.model.ID
	})

	result := make([]string, len(freeCandidates))
	for i, c := range freeCandidates {
		result[i] = c.model.ID
	}
	return result
}
