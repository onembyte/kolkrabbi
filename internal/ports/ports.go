// Package ports reports which TCP ports a command started listening on.
//
// It reads a table the kernel already keeps. Nothing is opened, probed or
// requested: an HTTP request to find out what a port is would be the agent
// making a network call nobody asked for, which is a different and worse thing
// than answering "your dev server is on 5173".
package ports

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/onembyte/kolkrabbi/internal/netaddr"
)

// Listener is one listening socket, and nothing about the process behind it.
//
// The process is deliberately absent. What a person needs after a command
// starts a server is the address they can open; the pid is something they can
// ask the shell for, and reporting it would be the first step toward the
// supervision item 29 refused.
type Listener struct {
	Address  string
	Port     int
	Loopback bool
}

// Describe renders one listener for a person.
//
// **Only loopback listeners get a URL.** A service on 0.0.0.0 gets its port
// stated and no link, because printing `http://192.168.1.5:5173` invites a
// click that publishes what the user may not have meant to publish — I26.5's
// reasoning about kolk's own address, applied to somebody else's server.
func Describe(l Listener) string {
	if l.Loopback {
		return fmt.Sprintf("listening on http://%s:%d", l.Address, l.Port)
	}
	return fmt.Sprintf("listening on %s:%d (not loopback, so no link)", l.Address, l.Port)
}

// Opened is what appeared between two snapshots.
//
// The difference is the whole feature: a listener that was already there is not
// news, and a command that started nothing should say nothing.
func Opened(before, after []Listener) []Listener {
	was := make(map[string]bool, len(before))
	for _, l := range before {
		was[key(l)] = true
	}
	var opened []Listener
	for _, l := range after {
		if !was[key(l)] {
			opened = append(opened, l)
		}
	}
	sort.Slice(opened, func(i, j int) bool { return opened[i].Port < opened[j].Port })
	return opened
}

func key(l Listener) string { return l.Address + ":" + strconv.Itoa(l.Port) }

// Snapshot lists what is listening now, or nothing at all when this machine
// will not say.
//
// Failure is silent by design: a machine where the table cannot be read still
// runs the agent, it just does not get the line. A courtesy that breaks a turn
// is a defect.
func Snapshot() []Listener {
	var found []Listener
	for _, path := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		body, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		found = append(found, parseProcNetTCP(string(body))...)
	}
	return found
}

// listenState is the value /proc/net/tcp writes for a socket in LISTEN.
const listenState = "0A"

// parseProcNetTCP reads the kernel's table.
//
// Both address and port are hex, and the address is little-endian per four
// bytes — which is why 127.0.0.1 appears as 0100007F. Rather than reassemble
// the dotted quad by hand, the well-known loopback encodings are matched
// directly: a wrong guess here would print a link to somebody else's network.
func parseProcNetTCP(body string) []Listener {
	var found []Listener
	for _, line := range strings.Split(body, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 || fields[3] != listenState {
			continue
		}
		address, portHex, ok := strings.Cut(fields[1], ":")
		if !ok {
			continue
		}
		port, err := strconv.ParseUint(portHex, 16, 32)
		if err != nil || port == 0 {
			continue
		}
		found = append(found, Listener{
			Address:  dottedFor(address),
			Port:     int(port),
			Loopback: isLoopbackHex(address),
		})
	}
	return found
}

// loopbackHex are the little-endian encodings of 127.0.0.1 and ::1, the two
// addresses that earn a clickable link.
var loopbackHex = map[string]bool{
	"0100007F":                         true, // 127.0.0.1
	"00000000000000000000000001000000": true, // ::1
}

func isLoopbackHex(address string) bool { return loopbackHex[strings.ToUpper(address)] }

func dottedFor(address string) string {
	if isLoopbackHex(address) {
		if len(address) == 8 {
			return "127.0.0.1"
		}
		return "[::1]"
	}
	if strings.Trim(address, "0") == "" {
		return "0.0.0.0"
	}
	return "0.0.0.0"
}

// parseLsof reads `lsof -iTCP -sTCP:LISTEN -P -n`, the fallback where /proc is
// not a thing.
func parseLsof(body string) []Listener {
	var found []Listener
	for _, line := range strings.Split(body, "\n") {
		if !strings.Contains(line, "(LISTEN)") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		// The address is the field before "(LISTEN)".
		var target string
		for i, field := range fields {
			if field == "(LISTEN)" && i > 0 {
				target = fields[i-1]
			}
		}
		host, portText, ok := lastCut(target, ":")
		if !ok {
			continue
		}
		port, err := strconv.Atoi(portText)
		if err != nil || port == 0 {
			continue
		}
		if host == "*" || host == "" {
			host = "0.0.0.0"
		}
		found = append(found, Listener{
			Address:  host,
			Port:     port,
			Loopback: netaddr.IsLoopback(host + ":" + portText),
		})
	}
	return found
}

func lastCut(text, sep string) (before, after string, found bool) {
	i := strings.LastIndex(text, sep)
	if i < 0 {
		return text, "", false
	}
	return text[:i], text[i+len(sep):], true
}
