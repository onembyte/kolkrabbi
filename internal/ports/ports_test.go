package ports

import (
	"strings"
	"testing"
)

// /proc/net/tcp is a table the kernel already keeps: hex address, hex port, and
// a state column where 0A means LISTEN. Nothing is probed or requested.
const procSample = `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 0100007F:1451 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 12345 1 0000 100 0 0 10 0
   1: 00000000:14E9 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 12346 1 0000 100 0 0 10 0
   2: 0100007F:C350 0100007F:1451 01 00000000:00000000 00:00000000 00000000  1000        0 12347 1 0000 100 0 0 10 0
`

func TestParseProcFindsOnlyListeners(t *testing.T) {
	found := parseProcNetTCP(procSample)
	if len(found) != 2 {
		t.Fatalf("found %d listeners, want 2 (the third row is an established connection): %v", len(found), found)
	}
	byPort := map[int]Listener{}
	for _, l := range found {
		byPort[l.Port] = l
	}
	loopback, ok := byPort[5201] // 0x1451
	if !ok {
		t.Fatalf("did not find the loopback listener: %v", found)
	}
	if !loopback.Loopback {
		t.Errorf("127.0.0.1 was not recognised as loopback: %#v", loopback)
	}
	wildcard, ok := byPort[5353] // 0x14E9
	if !ok {
		t.Fatalf("did not find the wildcard listener: %v", found)
	}
	if wildcard.Loopback {
		t.Errorf("0.0.0.0 was treated as loopback: %#v", wildcard)
	}
}

// Only loopback ports get a URL. Printing http://192.168.1.5:5173 invites a
// click that publishes what the user may not have meant to publish.
func TestOnlyLoopbackListenersGetALink(t *testing.T) {
	if line := Describe(Listener{Address: "127.0.0.1", Port: 5173, Loopback: true}); !strings.Contains(line, "http://127.0.0.1:5173") {
		t.Errorf("a loopback listener has no link: %q", line)
	}
	line := Describe(Listener{Address: "0.0.0.0", Port: 5173})
	if strings.Contains(line, "http://") {
		t.Errorf("a wildcard listener was given a clickable link: %q", line)
	}
	if !strings.Contains(line, "5173") {
		t.Errorf("a wildcard listener does not even state its port: %q", line)
	}
}

// The whole feature is the difference between two snapshots: what this command
// started, not what was already running.
func TestOnlyNewListenersAreReported(t *testing.T) {
	before := []Listener{{Address: "127.0.0.1", Port: 5432, Loopback: true}}
	after := []Listener{
		{Address: "127.0.0.1", Port: 5432, Loopback: true},
		{Address: "127.0.0.1", Port: 5173, Loopback: true},
	}
	opened := Opened(before, after)
	if len(opened) != 1 || opened[0].Port != 5173 {
		t.Fatalf("opened = %v, want only the new port", opened)
	}
}

func TestNothingNewIsReportedWhenNothingStarted(t *testing.T) {
	same := []Listener{{Address: "127.0.0.1", Port: 5432, Loopback: true}}
	if opened := Opened(same, same); len(opened) != 0 {
		t.Errorf("opened = %v, want nothing", opened)
	}
}

// A machine whose table cannot be read still runs the agent; it just does not
// get the line.
func TestUnreadableTablesAreSilent(t *testing.T) {
	if found := parseProcNetTCP("this is not a proc table"); len(found) != 0 {
		t.Errorf("garbage parsed into %v", found)
	}
	if found := parseProcNetTCP(""); len(found) != 0 {
		t.Errorf("an empty table parsed into %v", found)
	}
}

// lsof is the fallback where /proc is not a thing.
func TestParseLsofFindsListeners(t *testing.T) {
	const sample = `COMMAND   PID USER   FD  TYPE DEVICE SIZE/OFF NODE NAME
node    12345 you   23u  IPv4 123456      0t0  TCP 127.0.0.1:5173 (LISTEN)
node    12345 you   24u  IPv6 123457      0t0  TCP *:8080 (LISTEN)
`
	found := parseLsof(sample)
	if len(found) != 2 {
		t.Fatalf("found %d listeners, want 2: %v", len(found), found)
	}
	byPort := map[int]Listener{}
	for _, l := range found {
		byPort[l.Port] = l
	}
	if !byPort[5173].Loopback {
		t.Errorf("127.0.0.1 was not recognised as loopback: %#v", byPort[5173])
	}
	if byPort[8080].Loopback {
		t.Errorf("* was treated as loopback: %#v", byPort[8080])
	}
}
