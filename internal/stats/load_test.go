package stats

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeStats(t *testing.T, lines ...string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, fileName), []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

const goodCall = `{"kind":"call","turn":"t1","model":"vendor/model","prompt_tokens":10,"completion_tokens":2,"cost":0.001}`
const otherCall = `{"kind":"call","turn":"t2","model":"vendor/model","prompt_tokens":20,"completion_tokens":4,"cost":0.002}`

func TestLoadSkipsAMalformedLineAndCountsIt(t *testing.T) {
	dir := writeStats(t, goodCall, `{"kind":"call",BROKEN`, otherCall)

	records, skipped, err := LoadCounted(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("loaded %d records, want both good ones", len(records))
	}
	// Silently dropping data is how a user ends up trusting a number that is
	// missing half its history.
	if skipped != 1 {
		t.Fatalf("skipped = %d, want 1", skipped)
	}
}

func TestLoadSurvivesALineTooLongToScan(t *testing.T) {
	// A partial write or one enormous record used to make bufio return
	// ErrTooLong, which failed the whole load: one bad line cost the user
	// every record they had.
	huge := `{"kind":"call","model":"` + strings.Repeat("x", 6*1024*1024) + `"}`
	dir := writeStats(t, goodCall, huge, otherCall)

	records, skipped, err := LoadCounted(dir)
	if err != nil {
		t.Fatalf("an over-long line failed the whole load: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("loaded %d records, want the readable ones on both sides", len(records))
	}
	if skipped != 1 {
		t.Fatalf("skipped = %d, want the unreadable line counted", skipped)
	}
}

func TestLoadDoesNotCountBlankLinesAsLoss(t *testing.T) {
	dir := writeStats(t, goodCall, "", "   ", otherCall)

	records, skipped, err := LoadCounted(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 || skipped != 0 {
		t.Fatalf("records %d skipped %d, want 2 and 0", len(records), skipped)
	}
}

func TestLoadOnAMissingFileIsNotAnError(t *testing.T) {
	records, skipped, err := LoadCounted(t.TempDir())
	if err != nil || len(records) != 0 || skipped != 0 {
		t.Fatalf("records %d skipped %d err %v", len(records), skipped, err)
	}
}

func TestLoadKeepsItsOriginalSignatureWorking(t *testing.T) {
	dir := writeStats(t, goodCall, `not json at all`, otherCall)
	records, err := Load(dir)
	if err != nil || len(records) != 2 {
		t.Fatalf("records %d err %v", len(records), err)
	}
}
