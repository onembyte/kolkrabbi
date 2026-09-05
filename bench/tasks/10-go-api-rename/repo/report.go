package bench

import "fmt"

func Report(s *Store) string {
	return fmt.Sprintf("total=%d", s.Sum())
}
