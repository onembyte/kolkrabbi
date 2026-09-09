package stats

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/onembyte/kolkrabbi/internal/atomicfile"
)

// ModelRating is how a model has been rated on this machine.
type ModelRating struct {
	Average float64 `json:"average"`
	Count   int     `json:"count"`
}

// RatingsByModel folds this machine's own 1–5 ratings per model.
//
// It reuses Aggregate rather than re-deriving the join: a rating is recorded
// against a turn, and every call of that turn shares it, which is a rule that
// should exist in one place. Aggregate already computes exactly that, so this
// is a projection of what `kolk stats` shows rather than a second opinion about
// the same log.
//
// A model nobody rated is absent rather than zero. "Never rated" and "rated
// badly" are different facts, and only one of them should move a ranking.
//
// The fold is cached (OPTIMIZATION_PLAN.md O5). It is on the startup path and
// stats.jsonl grows by a line per model call forever, so re-reading all of it
// to answer a question whose answer only changes when somebody types `/rate`
// was the whole cost. See ratings.json below for what makes reusing it safe.
func RatingsByModel(dir string) (map[string]ModelRating, error) {
	ratings, _, err := RatingsByModelFold(dir)
	return ratings, err
}

// RatingsFold says how an answer was reached, so `kolk doctor` can report a
// cache that had to be rebuilt rather than leave it invisible.
type RatingsFold int

const (
	// FoldCached: ratings.json still matched the log; nothing was read.
	FoldCached RatingsFold = iota
	// FoldAppended: only the bytes appended since the cache were read.
	FoldAppended
	// FoldRebuilt: the whole log was folded again.
	FoldRebuilt
)

// String names the fold for a person.
func (f RatingsFold) String() string {
	switch f {
	case FoldCached:
		return "cached"
	case FoldAppended:
		return "extended"
	default:
		return "rebuilt"
	}
}

// RatingsByModelFold is RatingsByModel with how it got there.
func RatingsByModelFold(dir string) (map[string]ModelRating, RatingsFold, error) {
	info, err := os.Stat(path(dir))
	if os.IsNotExist(err) {
		return map[string]ModelRating{}, FoldCached, nil
	}
	if err != nil {
		return nil, FoldRebuilt, err
	}

	cache, ok := loadRatingsCache(dir)
	switch {
	case !ok:
	case cache.matches(info):
		// The log has not moved since the fold: nothing to read at all.
		return cache.ratings(), FoldCached, nil
	case cache.canExtend(info):
		if consumed, extended := cache.extend(path(dir)); extended {
			cache.Offset += consumed
			cache.Size, cache.ModTime = info.Size(), info.ModTime()
			cache.save(dir)
			return cache.ratings(), FoldAppended, nil
		}
	}

	records, err := Load(dir)
	if err != nil {
		return nil, FoldRebuilt, err
	}
	rebuilt := ratingsCache{
		Version: ratingsCacheVersion,
		// The size is the one measured before the read, never after: a log
		// that grew while it was being folded must leave those bytes to the
		// next fold rather than have them counted as already seen.
		Offset:  info.Size(),
		Size:    info.Size(),
		ModTime: info.ModTime(),
		Models:  map[string]ModelRating{},
		Turns:   encodeRatedTurns(records),
	}
	for _, row := range Aggregate(records) {
		if row.Ratings == 0 {
			continue
		}
		rebuilt.Models[row.Model] = ModelRating{Average: row.AvgRating, Count: row.Ratings}
	}
	rebuilt.save(dir)
	return rebuilt.ratings(), FoldRebuilt, nil
}

// ratings.json — the fold of stats.jsonl as of a known point in the file.
//
// Reusing it is safe because of what a rating is: a line joined to a turn, and
// through the turn to every model call of that turn. So an appended *call*
// changes this fold only when its turn has already been rated, and an appended
// *rating* changes it always. Both are exactly what a forward scan of the new
// bytes can see, and either one sends the fold back to the whole log — which
// is why the numbers here are the averages themselves and never a running sum
// that could drift.
//
// It is a cache: deleting it costs one full fold and nothing else.
const ratingsCacheFile = "ratings.json"

const ratingsCacheVersion = 1

type ratingsCache struct {
	Version int `json:"version"`
	// Offset is how many bytes of stats.jsonl this fold consumed, counting
	// only whole lines: a record still being appended is left for next time.
	Offset int64 `json:"offset"`
	// Size and ModTime are what the log looked like when the fold was taken.
	// Both are checked, because an append moves the size and an edit that
	// keeps the size moves the modification time.
	Size    int64                  `json:"size"`
	ModTime time.Time              `json:"mod_time"`
	Models  map[string]ModelRating `json:"models"`
	// Turns is every turn a rating was recorded against. A call appended for
	// one of them would change the fold, and that is the one thing a forward
	// scan needs the previous fold to recognise.
	//
	// Left encoded until something asks: it is the only part of this file that
	// grows with the log, and the read that matters — an unchanged log, on
	// every start — never looks at it. Decoding it into strings anyway cost
	// 2.4 ms of the 2.9 ms a warm 20k-record fold took, measured.
	Turns json.RawMessage `json:"rated_turns,omitempty"`
}

func ratingsCachePath(dir string) string { return filepath.Join(dir, ratingsCacheFile) }

// loadRatingsCache reads the cache. Anything unreadable, of another version,
// or self-inconsistent is simply not a cache: the caller folds the log.
func loadRatingsCache(dir string) (ratingsCache, bool) {
	data, err := os.ReadFile(ratingsCachePath(dir))
	if err != nil {
		return ratingsCache{}, false
	}
	var cache ratingsCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return ratingsCache{}, false
	}
	if cache.Version != ratingsCacheVersion || cache.Offset < 0 || cache.Models == nil {
		return ratingsCache{}, false
	}
	return cache, true
}

// save writes the cache beside the log. Best effort on purpose: a data
// directory that cannot be written still answers the question, one fold at a
// time, and a session must never fail over a cache.
func (c ratingsCache) save(dir string) {
	c.Version = ratingsCacheVersion
	data, err := json.Marshal(c)
	if err != nil {
		return
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	_ = atomicfile.Write(ratingsCachePath(dir), append(data, '\n'), 0o600)
}

// matches reports that the log is byte-for-byte where the fold left it.
func (c ratingsCache) matches(info os.FileInfo) bool {
	return c.Offset == info.Size() && c.Size == info.Size() && c.ModTime.Equal(info.ModTime())
}

// canExtend reports that the log has only grown since the fold. A log that
// shrank, or that changed without growing, was edited or replaced, and the
// only honest answer to that is to read it again.
func (c ratingsCache) canExtend(info os.FileInfo) bool {
	return c.Offset > 0 && info.Size() > c.Offset && !info.ModTime().Before(c.ModTime)
}

// ratings is the fold as callers see it, copied so a caller cannot edit the
// cache's own map.
func (c ratingsCache) ratings() map[string]ModelRating {
	out := make(map[string]ModelRating, len(c.Models))
	for model, rating := range c.Models {
		out[model] = rating
	}
	return out
}

// extend scans the bytes appended since the fold, reporting how many whole
// lines' bytes it consumed and whether the fold still stands.
//
// It stands unless the new bytes hold a rating, or a call of a turn already
// rated — the two records that would change an average. Everything else is a
// call for an unrated turn, which contributes nothing to this fold at all.
func (c ratingsCache) extend(logPath string) (int64, bool) {
	f, err := os.Open(logPath)
	if err != nil {
		return 0, false
	}
	defer func() { _ = f.Close() }() // read path: nothing to lose on close
	if _, err := f.Seek(c.Offset, io.SeekStart); err != nil {
		return 0, false
	}
	rated := c.ratedTurns()

	var consumed int64
	reader := bufio.NewReaderSize(f, 64*1024)
	for {
		line, raw, err := readStatsLine(reader)
		// Only a line that ended is counted. A record half-written by another
		// kolk is left where it is, so the next fold reads it whole.
		if err == nil || errors.Is(err, errLineTooLong) {
			consumed += int64(raw)
		}
		if len(bytes.TrimSpace(line)) > 0 {
			var r Record
			if json.Unmarshal(line, &r) == nil {
				switch {
				case r.Kind == "rating":
					return 0, false
				case r.Kind == "call" && rated[r.Turn]:
					return 0, false
				}
			}
		}
		switch {
		case err == nil:
		case errors.Is(err, io.EOF):
			return consumed, true
		case errors.Is(err, errLineTooLong):
		default:
			// A read that failed part way says nothing about the rest of the
			// file; the whole log is the only sound answer.
			return 0, false
		}
	}
}

// ratedTurns decodes the rated turns this fold was taken with. A list that
// cannot be decoded is an empty one, which costs a full re-fold rather than a
// wrong answer: extend refuses every call it cannot rule out.
func (c ratingsCache) ratedTurns() map[string]bool {
	var turns []string
	if len(c.Turns) > 0 {
		if err := json.Unmarshal(c.Turns, &turns); err != nil {
			return nil
		}
	}
	rated := make(map[string]bool, len(turns))
	for _, turn := range turns {
		rated[turn] = true
	}
	return rated
}

// encodeRatedTurns is every turn a usable rating was recorded against,
// deduplicated and ordered so the cache file is stable across folds of the
// same log.
func encodeRatedTurns(records []Record) json.RawMessage {
	seen := map[string]bool{}
	for _, r := range records {
		if r.Kind == "rating" && r.Turn != "" && r.Rating >= 1 && r.Rating <= 5 {
			seen[r.Turn] = true
		}
	}
	turns := make([]string, 0, len(seen))
	for turn := range seen {
		turns = append(turns, turn)
	}
	sort.Strings(turns)
	encoded, err := json.Marshal(turns)
	if err != nil {
		return nil
	}
	return encoded
}

// invalidateRatingsCache drops the fold. Called from Append when a rating is
// recorded: the size and modification-time checks would catch it anyway, but a
// cache that is known to be wrong is deleted rather than left to be detected.
func invalidateRatingsCache(dir string) {
	_ = os.Remove(ratingsCachePath(dir))
}
