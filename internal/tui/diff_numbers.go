package tui

import (
	"fmt"
	"strconv"
	"strings"
)

type diffLineNumbers struct {
	old, next int
	located   bool
}

func (n *diffLineNumbers) prefix(row string, width int) string {
	if strings.HasPrefix(row, "… ") {
		n.located = false
	}
	if strings.HasPrefix(row, "@@ ") {
		n.located = false
		fields := strings.Fields(row)
		if len(fields) >= 3 && strings.HasPrefix(fields[1], "-") && strings.HasPrefix(fields[2], "+") {
			old, _, _ := strings.Cut(fields[1][1:], ",")
			next, _, _ := strings.Cut(fields[2][1:], ",")
			var oldErr, nextErr error
			n.old, oldErr = strconv.Atoi(old)
			n.next, nextErr = strconv.Atoi(next)
			n.located = oldErr == nil && nextErr == nil && n.old >= 0 && n.next >= 0
		}
		return diffPrefix(row)
	}
	if !n.located || len(row) == 0 {
		return diffPrefix(row)
	}
	var number int
	switch row[0] {
	case '+':
		number = n.next
		n.next++
	case '-':
		number = n.old
		n.old++
	case ' ':
		number = n.next
		n.old++
		n.next++
	default:
		return diffPrefix(row)
	}
	if width < 20 {
		return diffPrefix(row)
	}
	return fmt.Sprintf("%4d %c %s", number, row[0], row[1:])
}
