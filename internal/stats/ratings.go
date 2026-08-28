package stats

// ModelRating is how a model has been rated on this machine.
type ModelRating struct {
	Average float64
	Count   int
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
func RatingsByModel(dir string) (map[string]ModelRating, error) {
	records, err := Load(dir)
	if err != nil {
		return nil, err
	}
	ratings := map[string]ModelRating{}
	for _, row := range Aggregate(records) {
		if row.Ratings == 0 {
			continue
		}
		ratings[row.Model] = ModelRating{Average: row.AvgRating, Count: row.Ratings}
	}
	return ratings, nil
}
