package engine

import (
	"bufio"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type ChapterStatus string

const (
	StatusPending   ChapterStatus = "pending"
	StatusPlanning  ChapterStatus = "planning"
	StatusExecuting ChapterStatus = "executing"
	StatusVerifying ChapterStatus = "verifying"
	StatusDone      ChapterStatus = "completed"
	StatusFailed    ChapterStatus = "failed"
	StatusBlocked   ChapterStatus = "blocked"
	StatusAborted   ChapterStatus = "aborted"
)

// ValidateTransition enforces valid chapter lifecycle progression per
// docs/plan/10-saga-loop.md §1.
func ValidateTransition(from, to ChapterStatus) error {
	switch from {
	case StatusPending:
		if to == StatusPlanning || to == StatusAborted {
			return nil
		}
	case StatusPlanning:
		if to == StatusExecuting || to == StatusBlocked || to == StatusAborted {
			return nil
		}
	case StatusExecuting:
		if to == StatusVerifying || to == StatusFailed || to == StatusAborted {
			return nil
		}
	case StatusVerifying:
		if to == StatusDone || to == StatusFailed || to == StatusAborted {
			return nil
		}
	case StatusFailed:
		if to == StatusPlanning || to == StatusAborted || to == StatusBlocked {
			return nil
		}
	case StatusDone, StatusAborted:
		return fmt.Errorf("cannot transition terminal chapter status %q to %q", from, to)
	}
	return fmt.Errorf("illegal chapter status transition from %q to %q", from, to)
}

type Chapter struct {
	Number       int           `json:"number"`
	Title        string        `json:"title"`
	Status       ChapterStatus `json:"status"`
	Changes      []string      `json:"changes,omitempty"`
	Verification string        `json:"verification,omitempty"`
	Commit       string        `json:"commit,omitempty"`
	CostUSD      float64       `json:"cost_usd,omitempty"`
	DurationSec  int           `json:"duration_sec,omitempty"`
}

type AcceptanceCriterion struct {
	Description string `json:"description"`
	Done        bool   `json:"done"`
}

type SagaState struct {
	Goal           string                `json:"goal"`
	Started        time.Time             `json:"started"`
	Status         string                `json:"status"`
	ActiveChapter  int                   `json:"active_chapter"`
	MaxChapters    int                   `json:"max_chapters"`
	CumulativeCost float64               `json:"cumulative_cost"`
	CostLimit      float64               `json:"cost_limit"`
	Strikes        int                   `json:"strikes,omitempty"`
	MaxStrikes     int                   `json:"max_strikes,omitempty"`
	Criteria       []AcceptanceCriterion `json:"criteria"`
	Chapters       []Chapter             `json:"chapters"`
	OpenRisks      []string              `json:"open_risks,omitempty"`
}

const DefaultMaxStrikes = 3

// RecordGateFailure accounts for one consecutive failed chapter gate and
// blocks the saga when the bounded doom-loop threshold is reached.
func RecordGateFailure(s *SagaState) error {
	if s == nil {
		return fmt.Errorf("saga: state is required")
	}
	max := s.MaxStrikes
	if max <= 0 {
		max = DefaultMaxStrikes
		s.MaxStrikes = max
	}
	s.Strikes++
	if s.Strikes >= max {
		s.Status = "blocked"
	}
	return nil
}

// RecordChapterSuccess clears consecutive failures after a verified chapter.
func RecordChapterSuccess(s *SagaState) error {
	if s == nil {
		return fmt.Errorf("saga: state is required")
	}
	s.Strikes = 0
	return nil
}

const timeFormat = "2006-01-02 15:04:05"

func FormatSagaMarkdown(s *SagaState) string {
	var b strings.Builder

	fmt.Fprintf(&b, "# SAGA: %s\n\n", s.Goal)
	fmt.Fprintf(&b, "- **Goal**: %s\n", s.Goal)
	if !s.Started.IsZero() {
		fmt.Fprintf(&b, "- **Started**: %s\n", s.Started.Format(timeFormat))
	}
	status := s.Status
	if status == "" {
		status = "in-progress"
	}
	maxChap := s.MaxChapters
	if maxChap == 0 {
		maxChap = 15
	}
	fmt.Fprintf(&b, "- **Status**: %s (Chapter %d / %d)\n", status, s.ActiveChapter, maxChap)

	costLimit := s.CostLimit
	if costLimit == 0 {
		costLimit = 5.00
	}
	fmt.Fprintf(&b, "- **Cumulative Cost**: $%.2f / $%.2f limit\n\n", s.CumulativeCost, costLimit)
	if s.Strikes > 0 || s.MaxStrikes > 0 {
		maxStrikes := s.MaxStrikes
		if maxStrikes <= 0 {
			maxStrikes = DefaultMaxStrikes
		}
		fmt.Fprintf(&b, "- **Strikes**: %d / %d\n\n", s.Strikes, maxStrikes)
	}

	if len(s.Criteria) > 0 {
		b.WriteString("## Acceptance Criteria\n")
		for _, c := range s.Criteria {
			mark := " "
			if c.Done {
				mark = "x"
			}
			fmt.Fprintf(&b, "- [%s] %s\n", mark, c.Description)
		}
		b.WriteString("\n")
	}

	if len(s.Chapters) > 0 {
		b.WriteString("## Chapter Log\n\n")
		for _, ch := range s.Chapters {
			fmt.Fprintf(&b, "### Chapter %d: %s\n", ch.Number, ch.Title)
			fmt.Fprintf(&b, "- **Status**: %s\n", ch.Status)
			if len(ch.Changes) > 0 {
				fmt.Fprintf(&b, "- **Changes**: %s\n", strings.Join(ch.Changes, ", "))
			}
			if ch.Verification != "" {
				fmt.Fprintf(&b, "- **Verification**: %s\n", ch.Verification)
			}
			if ch.Commit != "" {
				fmt.Fprintf(&b, "- **Commit**: `%s`\n", ch.Commit)
			}
			if ch.CostUSD > 0 || ch.DurationSec > 0 {
				fmt.Fprintf(&b, "- **Cost**: $%.2f · %ds\n", ch.CostUSD, ch.DurationSec)
			}
			b.WriteString("\n")
		}
	}

	if len(s.OpenRisks) > 0 {
		b.WriteString("## Open Risks & Notes\n")
		for _, r := range s.OpenRisks {
			fmt.Fprintf(&b, "- %s\n", r)
		}
		b.WriteString("\n")
	}

	return b.String()
}

func ParseSagaMarkdown(data string) (*SagaState, error) {
	s := &SagaState{
		MaxChapters: 15,
		CostLimit:   5.00,
	}

	scanner := bufio.NewScanner(strings.NewReader(data))
	var currentSection string
	var currentChapter *Chapter

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "# SAGA:") {
			s.Goal = strings.TrimSpace(strings.TrimPrefix(line, "# SAGA:"))
			continue
		}

		if strings.HasPrefix(line, "## ") {
			if currentChapter != nil {
				s.Chapters = append(s.Chapters, *currentChapter)
				currentChapter = nil
			}
			currentSection = strings.TrimPrefix(line, "## ")
			continue
		}

		if strings.HasPrefix(line, "### Chapter ") {
			if currentChapter != nil {
				s.Chapters = append(s.Chapters, *currentChapter)
			}
			chHeader := strings.TrimPrefix(line, "### Chapter ")
			parts := strings.SplitN(chHeader, ":", 2)
			num, _ := strconv.Atoi(strings.TrimSpace(parts[0]))
			title := ""
			if len(parts) > 1 {
				title = strings.TrimSpace(parts[1])
			}
			currentChapter = &Chapter{
				Number: num,
				Title:  title,
			}
			continue
		}

		switch currentSection {
		case "":
			if strings.HasPrefix(line, "- **Goal**:") {
				s.Goal = strings.TrimSpace(strings.TrimPrefix(line, "- **Goal**:"))
			} else if strings.HasPrefix(line, "- **Started**:") {
				val := strings.TrimSpace(strings.TrimPrefix(line, "- **Started**:"))
				if t, err := time.Parse(timeFormat, val); err == nil {
					s.Started = t
				}
			} else if strings.HasPrefix(line, "- **Status**:") {
				val := strings.TrimSpace(strings.TrimPrefix(line, "- **Status**:"))
				if idx := strings.Index(val, "("); idx != -1 {
					s.Status = strings.TrimSpace(val[:idx])
					inner := val[idx+1:]
					if cidx := strings.Index(inner, "Chapter "); cidx != -1 {
						_, _ = fmt.Sscanf(inner[cidx:], "Chapter %d / %d", &s.ActiveChapter, &s.MaxChapters)
					}
				} else {
					s.Status = val
				}
			} else if strings.HasPrefix(line, "- **Cumulative Cost**:") {
				val := strings.TrimSpace(strings.TrimPrefix(line, "- **Cumulative Cost**:"))
				_, _ = fmt.Sscanf(val, "$%f / $%f", &s.CumulativeCost, &s.CostLimit)
			} else if strings.HasPrefix(line, "- **Strikes**:") {
				val := strings.TrimSpace(strings.TrimPrefix(line, "- **Strikes**:"))
				_, _ = fmt.Sscanf(val, "%d / %d", &s.Strikes, &s.MaxStrikes)
			}
		case "Acceptance Criteria":
			if strings.HasPrefix(line, "- [") && len(line) > 5 {
				done := line[3] == 'x' || line[3] == 'X'
				desc := strings.TrimSpace(line[5:])
				s.Criteria = append(s.Criteria, AcceptanceCriterion{
					Description: desc,
					Done:        done,
				})
			}
		case "Chapter Log":
			if currentChapter != nil {
				if strings.HasPrefix(line, "- **Status**:") {
					currentChapter.Status = ChapterStatus(strings.TrimSpace(strings.TrimPrefix(line, "- **Status**:")))
				} else if strings.HasPrefix(line, "- **Changes**:") {
					changesStr := strings.TrimSpace(strings.TrimPrefix(line, "- **Changes**:"))
					for _, c := range strings.Split(changesStr, ",") {
						if tr := strings.TrimSpace(c); tr != "" {
							currentChapter.Changes = append(currentChapter.Changes, tr)
						}
					}
				} else if strings.HasPrefix(line, "- **Verification**:") {
					currentChapter.Verification = strings.TrimSpace(strings.TrimPrefix(line, "- **Verification**:"))
				} else if strings.HasPrefix(line, "- **Commit**:") {
					val := strings.TrimSpace(strings.TrimPrefix(line, "- **Commit**:"))
					currentChapter.Commit = strings.Trim(val, "`")
				} else if strings.HasPrefix(line, "- **Cost**:") {
					val := strings.TrimSpace(strings.TrimPrefix(line, "- **Cost**:"))
					_, _ = fmt.Sscanf(val, "$%f · %ds", &currentChapter.CostUSD, &currentChapter.DurationSec)
				}
			}
		case "Open Risks & Notes":
			if strings.HasPrefix(line, "- ") {
				s.OpenRisks = append(s.OpenRisks, strings.TrimSpace(strings.TrimPrefix(line, "- ")))
			}
		}
	}

	if currentChapter != nil {
		s.Chapters = append(s.Chapters, *currentChapter)
	}

	return s, scanner.Err()
}
