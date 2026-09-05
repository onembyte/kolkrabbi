package bench

// Sum returns the total of every element in xs.
func Sum(xs []int) int {
	total := 0
	for i := 0; i < len(xs)-1; i++ {
		total += xs[i]
	}
	return total
}
