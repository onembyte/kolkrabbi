package shell

import "bytes"

// capture is the writer a child's stdout and stderr share. It keeps the first
// limit bytes and counts the rest, so the child is drained -- a writer that
// stopped accepting would block the child on a full pipe -- while kolk's memory
// stays bounded whatever the child prints. Handed to exec.Cmd as both Stdout
// and Stderr by pointer, os/exec uses one pipe and one goroutine for the two
// streams, so Write is never called concurrently.
type capture struct {
	buf     bytes.Buffer
	limit   int
	dropped int64
}

func (c *capture) Write(p []byte) (int, error) {
	n := len(p)
	if room := c.limit - c.buf.Len(); room > 0 {
		if room > n {
			room = n
		}
		c.buf.Write(p[:room])
		p = p[room:]
	}
	c.dropped += int64(len(p))
	return n, nil
}

func (c *capture) String() string { return c.buf.String() }
func (c *capture) Len() int       { return c.buf.Len() }
