package bench

// Index maps a key to the lines it appeared on.
type Index struct {
	byKey map[string][]int
}

func (ix *Index) Add(key string, line int) {
	ix.byKey[key] = append(ix.byKey[key], line)
}

func (ix *Index) Lines(key string) []int { return ix.byKey[key] }
