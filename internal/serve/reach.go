package serve

import (
	"fmt"
	"net"
	"strings"
)

// Interface is one network interface as Describe needs to see it.
//
// A plain struct rather than net.Interface so the description can be tested
// against a machine that does not exist. Reachability is exactly the kind of
// thing that is wrong only on somebody else's laptop.
type Interface struct {
	Name  string
	Addrs []string
}

// ReachKind is the shape of what can reach a bound server.
type ReachKind int

const (
	// ReachLoopback is reachable only from this machine.
	ReachLoopback ReachKind = iota
	// ReachTailscale has an address that works from anywhere on the tailnet.
	ReachTailscale
	// ReachNetwork is reachable by anything on the local network.
	ReachNetwork
)

// Reachability is how to reach a running server, and who else can.
type Reachability struct {
	Kind ReachKind
	URLs []string
	Note string
	// Tunnel is the SSH command that reaches a loopback bind from elsewhere.
	// Documented rather than implemented: it is one line and it needs nothing
	// from Kolkrabbi.
	Tunnel string
}

// tailscaleCGNAT is the range Tailscale assigns from. Recognising the range as
// well as the interface name matters because the interface is not always called
// tailscale0 — utun on macOS, ts0 on some setups.
var tailscaleCGNAT = &net.IPNet{
	IP:   net.IPv4(100, 64, 0, 0),
	Mask: net.CIDRMask(10, 32),
}

// Describe works out how a server bound to addr can be reached.
//
// The common failure here is not insecurity but confusion: someone binds
// 0.0.0.0, sets a token, and still cannot work out which URL to open on their
// phone. Every branch below exists to answer that, and to say plainly who else
// can reach the port.
func Describe(addr string, interfaces []Interface) Reachability {
	host, port := splitBound(addr)

	if isLoopback(addr) {
		return Reachability{
			Kind:   ReachLoopback,
			URLs:   []string{urlFor(host, port)},
			Note:   "Only this machine can reach it.",
			Tunnel: fmt.Sprintf("ssh -L %s:127.0.0.1:%s <host>", port, port),
		}
	}

	// A specific non-wildcard address answers on that address and nowhere
	// else. Listing the others would send someone to a port that will not
	// answer.
	if !isWildcard(host) {
		return Reachability{
			Kind: ReachNetwork,
			URLs: []string{urlFor(host, port)},
			Note: "Anything that can route to this address can reach it. Keep the token secret.",
		}
	}

	tailnet, local := sortAddresses(interfaces)
	urls := make([]string, 0, len(tailnet)+len(local))
	for _, ip := range append(tailnet, local...) {
		urls = append(urls, urlFor(ip, port))
	}

	switch {
	case len(tailnet) > 0:
		return Reachability{
			Kind: ReachTailscale,
			URLs: urls,
			Note: "The first address works from anywhere on your tailnet. The rest are reachable by anything on those networks.",
		}
	case len(urls) > 0:
		return Reachability{
			Kind: ReachNetwork,
			URLs: urls,
			Note: "Bound to every interface: anything on these networks can reach it. Keep the token secret.",
		}
	default:
		return Reachability{
			Kind: ReachNetwork,
			Note: "Bound to every interface, but no usable address was found on this machine.",
		}
	}
}

// sortAddresses splits usable addresses into tailnet and everything else.
func sortAddresses(interfaces []Interface) (tailnet, local []string) {
	for _, iface := range interfaces {
		named := strings.HasPrefix(strings.ToLower(iface.Name), "tailscale") ||
			strings.EqualFold(iface.Name, "ts0")
		for _, addr := range iface.Addrs {
			ip := net.ParseIP(strings.TrimSpace(strings.Split(addr, "/")[0]))
			if ip == nil || !usable(ip) {
				continue
			}
			if named || tailscaleCGNAT.Contains(ip.To4()) {
				tailnet = append(tailnet, ip.String())
				continue
			}
			local = append(local, ip.String())
		}
	}
	return tailnet, local
}

// usable rejects the addresses nobody types into a phone.
func usable(ip net.IP) bool {
	return !ip.IsLoopback() &&
		!ip.IsLinkLocalUnicast() &&
		!ip.IsLinkLocalMulticast() &&
		!ip.IsUnspecified()
}

func isWildcard(host string) bool {
	if host == "" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsUnspecified()
}

// splitBound pulls the host and port out of a bind address, tolerating the
// spellings people actually write.
func splitBound(addr string) (host, port string) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return strings.Trim(addr, "[]"), ""
	}
	return strings.Trim(host, "[]"), port
}

// urlFor builds a URL, bracketing an IPv6 host.
func urlFor(host, port string) string {
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if port == "" {
		return "http://" + host
	}
	return "http://" + host + ":" + port
}

// LocalInterfaces reads this machine's interfaces for Describe.
//
// Separated from Describe so the decision-making is testable and only this
// thin wrapper touches the machine.
func LocalInterfaces() []Interface {
	found, err := net.Interfaces()
	if err != nil {
		return nil
	}
	out := make([]Interface, 0, len(found))
	for _, iface := range found {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		entry := Interface{Name: iface.Name}
		for _, addr := range addrs {
			entry.Addrs = append(entry.Addrs, addr.String())
		}
		out = append(out, entry)
	}
	return out
}
