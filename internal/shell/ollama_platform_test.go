package shell

import "testing"

func TestOllamaMacOSMinimum(t *testing.T) {
	for _, version := range []string{"10.15.7", "13.7", "", "invalid"} {
		if err := checkOllamaMacOS(version); err == nil {
			t.Errorf("accepted %q", version)
		}
	}
	for _, version := range []string{"14.0", "15.6.1", "26.0"} {
		if err := checkOllamaMacOS(version); err != nil {
			t.Errorf("refused %q: %v", version, err)
		}
	}
}
