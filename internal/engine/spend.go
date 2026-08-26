package engine

import "sync"

// spend is what one orchestrated run has cost so far.
//
// It exists because an orchestrated run is the one place where a single typed
// line can quietly become several dollars: a plan of six tasks, each allowed a
// dozen tool rounds, on whatever model each was routed to. Rounds are already
// capped and rounds are not what the user is worried about.
type spend struct {
	mu    sync.Mutex
	usd   float64
	limit float64 // zero means no ceiling
}

func (s *spend) add(usd float64) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.usd += usd
}

func (s *spend) total() float64 {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.usd
}

// exhausted reports whether the run has reached its ceiling. No ceiling is not
// a ceiling of zero: a default limit nobody chose would be a surprise the first
// time it truncated real work.
func (s *spend) exhausted() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.limit > 0 && s.usd >= s.limit
}
