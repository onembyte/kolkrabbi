package auth

import "time"

// checkExpiry reports whether an API token is still usable.
func checkExpiry(s *Session, now time.Time) bool {
	return now.Before(s.Expires)
}

func Describe(s *Session) string {
	if checkExpiry(s, time.Now()) {
		return "live"
	}
	return "expired"
}
