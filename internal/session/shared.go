package session

import "sort"

// SharedCheckout is one directory that more than one live session is working
// in.
type SharedCheckout struct {
	Dir      string
	Sessions []string
}

// SharedCheckouts reports the directories where live sessions overlap.
//
// Two sessions in one checkout will edit each other's files, and each one's
// `/undo` restores over the other's work — the shadow store snapshots a whole
// tree, so a rewind in one session takes back what the other did in the same
// tree. Item 27 does not refuse the overlap, because two terminals in one
// repository is a thing people do on purpose. What it refuses is **silence**
// about it: this should be something a person is told once, not something
// discovered when an undo restores someone else's work.
//
// Only live sessions count. An idle one holds no lock and runs no turns, so it
// is not competing for anything, and a session with no recorded directory
// cannot be said to share one — guessing would produce a warning about nothing,
// which is how warnings come to be ignored.
func SharedCheckouts(cards []Card) []SharedCheckout {
	byDir := map[string][]string{}
	for _, card := range cards {
		if card.State != StateLive || card.CWD == "" {
			continue
		}
		byDir[card.CWD] = append(byDir[card.CWD], card.ID)
	}

	shared := make([]SharedCheckout, 0, len(byDir))
	for dir, ids := range byDir {
		if len(ids) < 2 {
			continue
		}
		sort.Strings(ids)
		shared = append(shared, SharedCheckout{Dir: dir, Sessions: ids})
	}
	// One warning per directory, ordered, so the same situation reads the same
	// way twice.
	sort.Slice(shared, func(i, j int) bool { return shared[i].Dir < shared[j].Dir })
	return shared
}
