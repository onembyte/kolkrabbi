package bench

type Store struct {
	items []int
}

func (s *Store) Add(n int) { s.items = append(s.items, n) }

// Sum returns the total of every item.
func (s *Store) Sum() int {
	t := 0
	for _, n := range s.items {
		t += n
	}
	return t
}
