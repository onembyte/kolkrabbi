package shell

import (
	"fmt"
	"strings"
)

// SandboxDiagnostic is the one bounded line appended to a sandboxed command's
// failing result when the output carries the kernel's refusal phrase. It says
// what is confined and names the switch. It never claims to know the cause: the
// phrase is a strong hint, not proof, and a line that overstates is worse than
// none. Anything else -- success, an ordinary non-zero exit, kolk's own
// refusal, a timeout -- gets nothing.
func SandboxDiagnostic(p Sandbox, res Result) string {
	if res.OK() || res.ExitCode == -1 || res.TimedOut {
		return ""
	}
	if !strings.Contains(res.Output, "Operation not permitted") && !strings.Contains(res.Output, "Permission denied") {
		return ""
	}
	network := "allowed"
	if p.Network == NetworkDeny {
		network = "denied"
	}
	return fmt.Sprintf("[sandbox: writes are confined to %s and %s; network %s. If this command legitimately needs more: /sandbox off]\n",
		p.Root, p.Temp, network)
}

// SandboxReport is what /doctor prints: machine facts about what would enforce
// a policy here, so a pasted report explains why /sandbox on did or did not take.
type SandboxReport struct {
	Mechanism           string // "seatbelt", "landlock v4"; empty when Err is set
	Err                 error  // why nothing can enforce a sandbox here
	NetworkDenyEnforced bool   // whether network = deny could be kept on this machine
	NetworkDenyReason   string // why not, when it could not
}

// Report probes the machine. It enforces nothing.
func Report() SandboxReport {
	r := SandboxReport{}
	r.Mechanism, r.Err = mechanism()
	if r.Err != nil {
		r.NetworkDenyReason = r.Err.Error()
		return r
	}
	r.NetworkDenyEnforced, r.NetworkDenyReason = networkDenySupported()
	return r
}
