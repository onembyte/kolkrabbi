package auth

import "time"

type Session struct {
	Token   string
	Expires time.Time
}

func NewSession(token string, ttl time.Duration) *Session {
	return &Session{Token: token, Expires: time.Now().Add(ttl)}
}
