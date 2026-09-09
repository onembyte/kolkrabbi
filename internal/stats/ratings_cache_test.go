package stats

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// appendAll is the log a cache test starts from.
func appendAll(t *testing.T, dir string, records ...Record) {
	t.Helper()
	for _, r := range records {
		if err := Append(dir, r); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
}

func rated(t *testing.T, dir string, model string) ModelRating {
	t.Helper()
	ratings, err := RatingsByModel(dir)
	if err != nil {
		t.Fatalf("RatingsByModel: %v", err)
	}
	return ratings[model]
}

// The second read of an unchanged log reads no log at all. This is the whole
// point of O5: the fold is on the startup path and the log only grows.
func TestSecondFoldOfAnUnchangedLogIsCached(t *testing.T) {
	dir := t.TempDir()
	appendAll(t, dir,
		Record{Kind: "call", Turn: "t_1", Model: "vendor/a"},
		Record{Kind: "rating", Turn: "t_1", Rating: 4},
	)

	first, fold, err := RatingsByModelFold(dir)
	if err != nil {
		t.Fatal(err)
	}
	if fold != FoldRebuilt {
		t.Fatalf("first fold = %v, want a rebuild", fold)
	}
	second, fold, err := RatingsByModelFold(dir)
	if err != nil {
		t.Fatal(err)
	}
	if fold != FoldCached {
		t.Fatalf("second fold = %v, want the cache", fold)
	}
	if first["vendor/a"] != second["vendor/a"] {
		t.Fatalf("cached %+v, folded %+v", second["vendor/a"], first["vendor/a"])
	}
	if _, err := os.Stat(filepath.Join(dir, ratingsCacheFile)); err != nil {
		t.Fatalf("ratings.json was not written: %v", err)
	}
}

// Calls appended for turns nobody rated cost a scan of the new bytes and
// change nothing — which is the case a running session produces all day.
func TestAppendedCallsExtendTheFoldWithoutRebuilding(t *testing.T) {
	dir := t.TempDir()
	appendAll(t, dir,
		Record{Kind: "call", Turn: "t_1", Model: "vendor/a"},
		Record{Kind: "rating", Turn: "t_1", Rating: 4},
	)
	if _, _, err := RatingsByModelFold(dir); err != nil {
		t.Fatal(err)
	}

	appendAll(t, dir,
		Record{Kind: "call", Turn: "t_2", Model: "vendor/a"},
		Record{Kind: "call", Turn: "t_2", Model: "vendor/b"},
	)
	ratings, fold, err := RatingsByModelFold(dir)
	if err != nil {
		t.Fatal(err)
	}
	if fold != FoldAppended {
		t.Fatalf("fold = %v, want the appended bytes only", fold)
	}
	if got := ratings["vendor/a"]; got.Count != 1 || got.Average != 4 {
		t.Fatalf("vendor/a = %+v, want the one rating it had", got)
	}
	if _, present := ratings["vendor/b"]; present {
		t.Fatal("an unrated model appeared in the ratings")
	}
	// And the next start reads nothing: the cache moved with the log.
	if _, fold, _ := RatingsByModelFold(dir); fold != FoldCached {
		t.Fatalf("fold after extending = %v, want the cache", fold)
	}
}

// A rating appended by another kolk is seen. The cache file it wrote is not
// the authority; the log is.
func TestARatingAppendedElsewhereRebuildsTheFold(t *testing.T) {
	dir := t.TempDir()
	appendAll(t, dir,
		Record{Kind: "call", Turn: "t_1", Model: "vendor/a"},
		Record{Kind: "rating", Turn: "t_1", Rating: 5},
	)
	if _, _, err := RatingsByModelFold(dir); err != nil {
		t.Fatal(err)
	}

	// Another process: a second turn, rated 1. Append removes the cache, so
	// put it back first to prove the log alone would have been enough.
	appendAll(t, dir, Record{Kind: "call", Turn: "t_2", Model: "vendor/a"})
	before, _, err := RatingsByModelFold(dir)
	if err != nil {
		t.Fatal(err)
	}
	if before["vendor/a"].Average != 5 {
		t.Fatalf("vendor/a = %+v before the second rating", before["vendor/a"])
	}
	writeLine(t, dir, Record{Kind: "rating", Turn: "t_2", Rating: 1})

	ratings, fold, err := RatingsByModelFold(dir)
	if err != nil {
		t.Fatal(err)
	}
	if fold != FoldRebuilt {
		t.Fatalf("fold = %v, want a rebuild", fold)
	}
	if got := ratings["vendor/a"]; got.Count != 2 || got.Average != 3 {
		t.Fatalf("vendor/a = %+v, want two ratings averaging 3", got)
	}
}

// The case a forward fold cannot see on its own: a call appended for a turn
// that was already rated. The rating counts once per call of its turn, so the
// average moves without a single new rating line.
func TestACallForAnAlreadyRatedTurnRebuildsTheFold(t *testing.T) {
	dir := t.TempDir()
	appendAll(t, dir,
		Record{Kind: "call", Turn: "t_1", Model: "vendor/a"},
		Record{Kind: "rating", Turn: "t_1", Rating: 5},
		Record{Kind: "call", Turn: "t_2", Model: "vendor/b"},
		Record{Kind: "rating", Turn: "t_2", Rating: 1},
	)
	if _, _, err := RatingsByModelFold(dir); err != nil {
		t.Fatal(err)
	}

	// A second call of the already-rated turn t_2, on vendor/a: the fold now
	// owes vendor/a a 1 as well as its 5.
	writeLine(t, dir, Record{Kind: "call", Turn: "t_2", Model: "vendor/a"})
	ratings, fold, err := RatingsByModelFold(dir)
	if err != nil {
		t.Fatal(err)
	}
	if fold != FoldRebuilt {
		t.Fatalf("fold = %v, want a rebuild", fold)
	}
	if got := ratings["vendor/a"]; got.Count != 2 || got.Average != 3 {
		t.Fatalf("vendor/a = %+v, want the rated turn's call counted", got)
	}
	if got := rated(t, dir, "vendor/a"); got.Average != 3 {
		t.Fatalf("RatingsByModel = %+v, want what the fold said", got)
	}
}

// A hand-edited log is not a grown log. Size and modification time are both
// checked, so an edit that keeps the length is still seen.
func TestAnEditedLogIsFoldedAgain(t *testing.T) {
	dir := t.TempDir()
	appendAll(t, dir,
		Record{Kind: "call", Turn: "t_1", Model: "vendor/a"},
		Record{Kind: "rating", Turn: "t_1", Rating: 5},
	)
	if _, _, err := RatingsByModelFold(dir); err != nil {
		t.Fatal(err)
	}

	body, err := os.ReadFile(path(dir))
	if err != nil {
		t.Fatal(err)
	}
	edited := []byte(string(body[:len(body)-len("5}\n")]) + "1}\n")
	if len(edited) != len(body) {
		t.Fatalf("the edit changed the length (%d → %d); this test is about the one that does not", len(body), len(edited))
	}
	if err := os.WriteFile(path(dir), edited, 0o600); err != nil {
		t.Fatal(err)
	}
	touch(t, path(dir), time.Now().Add(time.Second))

	ratings, fold, err := RatingsByModelFold(dir)
	if err != nil {
		t.Fatal(err)
	}
	if fold != FoldRebuilt {
		t.Fatalf("fold = %v, want a rebuild after an edit", fold)
	}
	if got := ratings["vendor/a"]; got.Average != 1 {
		t.Fatalf("vendor/a = %+v, want the edited rating", got)
	}
}

// A truncated log — a rotation, or a file replaced by a shorter one — is never
// extended from an offset that is no longer inside it.
func TestATruncatedLogIsFoldedAgain(t *testing.T) {
	dir := t.TempDir()
	appendAll(t, dir,
		Record{Kind: "call", Turn: "t_1", Model: "vendor/a"},
		Record{Kind: "rating", Turn: "t_1", Rating: 5},
	)
	if _, _, err := RatingsByModelFold(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path(dir), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	ratings, fold, err := RatingsByModelFold(dir)
	if err != nil {
		t.Fatal(err)
	}
	if fold != FoldRebuilt || len(ratings) != 0 {
		t.Fatalf("fold = %v, ratings = %v, want a rebuild of an empty log", fold, ratings)
	}
}

// Recording a rating drops the cache. The checks above would catch it anyway;
// this is the one that keeps `/rate` from ever depending on them.
func TestRecordingARatingDropsTheCache(t *testing.T) {
	dir := t.TempDir()
	appendAll(t, dir, Record{Kind: "call", Turn: "t_1", Model: "vendor/a"})
	if _, _, err := RatingsByModelFold(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ratingsCacheFile)); err != nil {
		t.Fatalf("no cache to drop: %v", err)
	}
	if err := NewStore(dir).RecordRating("s_a", "t_1", 2); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ratingsCacheFile)); !os.IsNotExist(err) {
		t.Fatalf("cache after /rate: %v, want it gone", err)
	}
	if got := rated(t, dir, "vendor/a"); got.Count != 1 || got.Average != 2 {
		t.Fatalf("vendor/a = %+v, want the rating just given", got)
	}
}

// A cache from another version, or a corrupt one, is not trusted and not
// fatal: it is simply not a cache.
func TestAnUnusableCacheIsIgnored(t *testing.T) {
	dir := t.TempDir()
	appendAll(t, dir,
		Record{Kind: "call", Turn: "t_1", Model: "vendor/a"},
		Record{Kind: "rating", Turn: "t_1", Rating: 4},
	)
	for _, body := range []string{"{", `{"version":999,"offset":10,"models":{}}`, ""} {
		if err := os.WriteFile(filepath.Join(dir, ratingsCacheFile), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		ratings, fold, err := RatingsByModelFold(dir)
		if err != nil {
			t.Fatalf("cache %q: %v", body, err)
		}
		if fold != FoldRebuilt {
			t.Fatalf("cache %q folded %v, want a rebuild", body, fold)
		}
		if got := ratings["vendor/a"]; got.Count != 1 || got.Average != 4 {
			t.Fatalf("cache %q gave %+v", body, got)
		}
	}
}

// The cache never disagrees with the whole log. One log, folded record by
// record with a cache present, must equal the same log folded once from cold.
func TestTheCachedFoldEqualsTheColdFold(t *testing.T) {
	warm, cold := t.TempDir(), t.TempDir()
	models := []string{"vendor/a", "vendor/b", "vendor/c"}
	var log []Record
	for turn := 0; turn < 40; turn++ {
		for call := 0; call < 3; call++ {
			log = append(log, Record{Kind: "call", Turn: "t_" + string(rune('a'+turn%26)) + string(rune('0'+turn/26)),
				Model: models[(turn+call)%len(models)]})
		}
		if turn%3 == 0 {
			log = append(log, Record{Kind: "rating", Turn: "t_" + string(rune('a'+turn%26)) + string(rune('0'+turn/26)), Rating: turn%5 + 1})
		}
	}
	for _, r := range log {
		appendAll(t, warm, r)
		// Fold after every record, so every path above is exercised in order.
		if _, _, err := RatingsByModelFold(warm); err != nil {
			t.Fatal(err)
		}
	}
	appendAll(t, cold, log...)

	warmed, err := RatingsByModel(warm)
	if err != nil {
		t.Fatal(err)
	}
	wanted, err := RatingsByModel(cold)
	if err != nil {
		t.Fatal(err)
	}
	if len(warmed) != len(wanted) {
		t.Fatalf("warm fold has %d models, cold %d", len(warmed), len(wanted))
	}
	for model, want := range wanted {
		if warmed[model] != want {
			t.Errorf("%s: warm %+v, cold %+v", model, warmed[model], want)
		}
	}
}

// writeLine appends a record the way another process would: straight to the
// file, without going through Append and its cache invalidation.
func writeLine(t *testing.T, dir string, r Record) {
	t.Helper()
	if r.Time.IsZero() {
		r.Time = time.Now()
	}
	f, err := os.OpenFile(path(dir), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	line, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	touch(t, path(dir), time.Now().Add(time.Second))
}

// touch moves a file's modification time forward. A test writes a log in
// milliseconds; a filesystem with one-second timestamps would otherwise report
// the same modification time for two different logs and hide a stale read.
func touch(t *testing.T, path string, when time.Time) {
	t.Helper()
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatal(err)
	}
}
