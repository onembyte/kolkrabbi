package stats

import (
	"testing"
	"time"
)

// The ratings the user already gives with /rate, folded per model so routing
// can use them. Aggregate already joins a turn's rating to its calls; this is
// that join, exposed.
func TestRatingsByModelFoldsWhatWasRated(t *testing.T) {
	dir := t.TempDir()
	for _, r := range []Record{
		{Kind: "call", Time: time.Now(), Session: "s_a", Turn: "t_1", Model: "vendor/good"},
		{Kind: "rating", Time: time.Now(), Session: "s_a", Turn: "t_1", Rating: 5},
		{Kind: "call", Time: time.Now(), Session: "s_a", Turn: "t_2", Model: "vendor/good"},
		{Kind: "rating", Time: time.Now(), Session: "s_a", Turn: "t_2", Rating: 4},
		{Kind: "call", Time: time.Now(), Session: "s_a", Turn: "t_3", Model: "vendor/bad"},
		{Kind: "rating", Time: time.Now(), Session: "s_a", Turn: "t_3", Rating: 1},
	} {
		if err := Append(dir, r); err != nil {
			t.Fatal(err)
		}
	}

	ratings, err := RatingsByModel(dir)
	if err != nil {
		t.Fatalf("RatingsByModel: %v", err)
	}
	good := ratings["vendor/good"]
	if good.Count != 2 || good.Average < 4.4 || good.Average > 4.6 {
		t.Errorf("vendor/good = %+v, want two ratings averaging 4.5", good)
	}
	if bad := ratings["vendor/bad"]; bad.Count != 1 || bad.Average != 1 {
		t.Errorf("vendor/bad = %+v, want one rating of 1", bad)
	}
}

// A call nobody rated is not an opinion and must not become one.
func TestUnratedCallsProduceNoRating(t *testing.T) {
	dir := t.TempDir()
	if err := Append(dir, Record{Kind: "call", Time: time.Now(), Turn: "t_1", Model: "vendor/quiet"}); err != nil {
		t.Fatal(err)
	}
	ratings, err := RatingsByModel(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, present := ratings["vendor/quiet"]; present {
		t.Error("an unrated model appeared in the ratings")
	}
}

func TestNoUsageLogIsNotAnError(t *testing.T) {
	ratings, err := RatingsByModel(t.TempDir())
	if err != nil {
		t.Fatalf("RatingsByModel with no log: %v", err)
	}
	if len(ratings) != 0 {
		t.Errorf("ratings = %v, want empty", ratings)
	}
}
