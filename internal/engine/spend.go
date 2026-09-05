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
	// Admission state (V34.2f). A call's cost is known only after it, so the
	// scheduler reserves the worst single-call cost seen in this run for every
	// task still in flight; until one call has reported, only one task runs.
	inflight int
	calls    int
	worst    float64
}

func (s *spend) add(usd float64) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.usd += usd
	s.calls++
	if usd > s.worst {
		s.worst = usd
	}
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

// start and finish bracket one task in flight for admission purposes.
func (s *spend) start() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inflight++
}

func (s *spend) finish() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inflight > 0 {
		s.inflight--
	}
}

// wouldCrossInFlight reports that starting one more task now could carry the
// run past its ceiling together with the tasks already running: either nothing
// has reported a cost yet (so nothing is known and one task must calibrate), or
// the total plus the worst known call for each task in flight already reaches
// the ceiling. Unlimited runs never wait. The ceiling can still be exceeded by
// the one call that reports after crossing it -- that is the sequential
// semantic the existing tests pin -- never by a whole wave of them.
func (s *spend) wouldCrossInFlight() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.limit <= 0 || s.inflight == 0 {
		return false
	}
	if s.calls == 0 {
		return true
	}
	return s.usd+float64(s.inflight)*s.worst >= s.limit
}
