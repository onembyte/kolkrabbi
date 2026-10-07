package tools

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/onembyte/kolkrabbi/internal/diff"
)

// ExecutionReport contains facts observed by the executor. Diff is bounded;
// Added and Removed count the complete diff before its display is shortened.
type ExecutionReport struct {
	Path, Diff               string
	Added, Removed           int
	Changed, Created, Failed bool
}

func (o Options) reportChange(path, before, after string, created bool) {
	if o.Report == nil {
		return
	}
	patch := diff.Unified(before, after, 3)
	report := ExecutionReport{Path: path, Changed: true, Created: created}
	for _, line := range strings.Split(patch, "\n") {
		if strings.HasPrefix(line, "+") {
			report.Added++
		} else if strings.HasPrefix(line, "-") {
			report.Removed++
		}
	}
	report.Diff = workDiffPreview(patch, 40)
	o.Report(report)
}

// Unlike a plain preview, the work log numbers lines. Restore the actual
// location before the retained tail when truncation cuts through a hunk.
func workDiffPreview(patch string, limit int) string {
	lines := strings.Split(strings.TrimSuffix(patch, "\n"), "\n")
	if len(lines) <= limit {
		return patch
	}
	head := limit / 2
	tail := len(lines) - (limit - head)
	old, next := 1, 1
	for _, line := range lines[:tail] {
		if strings.HasPrefix(line, "@@ ") {
			fields := strings.Fields(line)
			if len(fields) >= 3 {
				left, _, _ := strings.Cut(strings.TrimPrefix(fields[1], "-"), ",")
				right, _, _ := strings.Cut(strings.TrimPrefix(fields[2], "+"), ",")
				old, _ = strconv.Atoi(left)
				next, _ = strconv.Atoi(right)
			}
		} else if strings.HasPrefix(line, "-") {
			old++
		} else if strings.HasPrefix(line, "+") {
			next++
		} else if strings.HasPrefix(line, " ") {
			old++
			next++
		}
	}
	var out strings.Builder
	out.WriteString(strings.Join(lines[:head], "\n") + "\n")
	fmt.Fprintf(&out, "… %d lines not shown …\n", tail-head)
	if !strings.HasPrefix(lines[tail], "@@ ") {
		oldCount, newCount := 0, 0
		for _, line := range lines[tail:] {
			if strings.HasPrefix(line, "@@ ") {
				break
			}
			if strings.HasPrefix(line, "-") || strings.HasPrefix(line, " ") {
				oldCount++
			}
			if strings.HasPrefix(line, "+") || strings.HasPrefix(line, " ") {
				newCount++
			}
		}
		fmt.Fprintf(&out, "@@ -%d,%d +%d,%d @@\n", old, oldCount, next, newCount)
	}
	out.WriteString(strings.Join(lines[tail:], "\n") + "\n")
	return out.String()
}
