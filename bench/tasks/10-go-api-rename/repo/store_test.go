package bench

import "testing"

func TestTotal(t *testing.T) {
	var s Store
	s.Add(2)
	s.Add(3)
	if got := s.Total(); got != 5 {
		t.Errorf("Total() = %d, want 5", got)
	}
	if got := Report(&s); got != "total=5" {
		t.Errorf("Report = %q", got)
	}
}
