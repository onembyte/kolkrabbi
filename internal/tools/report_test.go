package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkReportKeepsCountsTailAndItsActualLocation(t *testing.T) {
	var before, after strings.Builder
	for i := 1; i <= 60; i++ {
		fmt.Fprintf(&before, "old line %d\n", i)
		fmt.Fprintf(&after, "new line %d\n", i)
	}
	root := t.TempDir()
	path := filepath.Join(root, "large.txt")
	if err := os.WriteFile(path, []byte(before.String()), 0600); err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal(map[string]string{"path": path, "content": after.String()})
	var report ExecutionReport
	if _, err := Execute(context.Background(), "write_file", string(args), Options{Root: root, Report: func(r ExecutionReport) { report = r }}); err != nil {
		t.Fatal(err)
	}
	if report.Added != 60 || report.Removed != 60 || !strings.Contains(report.Diff, "@@ -61,0 +41,20 @@\n+new line 41\n") || !strings.HasSuffix(report.Diff, "+new line 60\n") {
		t.Fatalf("incorrect long-edit report: %+v", report)
	}
}
