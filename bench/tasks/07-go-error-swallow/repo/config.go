package bench

import (
	"errors"
	"strconv"
)

var ErrEmpty = errors.New("empty value")

// ParsePort parses a port number. An empty string is an error, and so is
// anything strconv cannot read.
func ParsePort(s string) (int, error) {
	if s == "" {
		return 0, ErrEmpty
	}
	n, _ := strconv.Atoi(s)
	return n, nil
}
