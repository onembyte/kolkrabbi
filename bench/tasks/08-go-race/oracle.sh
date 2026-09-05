#!/usr/bin/env bash
set -euo pipefail
cat > counter.go <<'C'
package bench

import "sync"

// Counter counts events from several goroutines.
type Counter struct {
	mu sync.Mutex
	n  int
}

func (c *Counter) Inc() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.n++
}

func (c *Counter) Value() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}
C
