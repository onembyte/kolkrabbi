package bench

// Counter counts events from several goroutines.
type Counter struct {
	n int
}

func (c *Counter) Inc() { c.n++ }

func (c *Counter) Value() int { return c.n }
