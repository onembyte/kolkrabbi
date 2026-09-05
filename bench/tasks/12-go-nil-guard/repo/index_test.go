package bench

import "testing"

func TestIndexAdd(t *testing.T) {
	var ix Index
	ix.Add("alpha", 1)
	ix.Add("alpha", 4)
	got := ix.Lines("alpha")
	if len(got) != 2 || got[0] != 1 || got[1] != 4 {
		t.Errorf("Lines = %v, want [1 4]", got)
	}
	if len(ix.Lines("missing")) != 0 {
		t.Errorf("missing key should have no lines")
	}
}
