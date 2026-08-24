// Package arch encodes kolkrabbi's structural rules as data. arch_test.go
// enforces them against the real tree on every CI run.
//
// There is no per-line escape hatch, deliberately: a violation is fixed by
// editing the import, or by editing this file — which is a reviewed data
// change. A comment that suppresses a rule is something that gets typed at
// 1 a.m.; a data file is a decision someone has to agree with.
//
// The one concession is knownViolations, a ratchet for debt the migration has
// not paid off yet. It can only shrink: the test fails both when a package
// violates a rule without being listed AND when a listed violation has been
// fixed but not removed, so the list cannot rot into a permanent exemption.
package arch

// Layer is a rung of the dependency ladder. It is not a strict ladder — L5
// adapters import L4 upward, because that is the only way to implement the
// engine's port interfaces — so each layer names what it may import rather
// than relying on an ordering.
type Layer int

const (
	L0Platform Layer = iota // the only layer that knows what an OS is
	L1Contract              // protocol/ — public, stdlib only, never internal
	L2Hinge                 // the event bus
	L3Domain                // providers, tools, permissions
	L4Engine                // the turn loop and what orchestrates it
	L5Adapter               // implementations of the engine's ports
	L6Surface               // CLI, TUI, daemon, dashboard — the wiring lives here
	LTestKit                // fakes and fixtures; imported only by tests
)

var layerNames = map[Layer]string{
	L0Platform: "L0 platform",
	L1Contract: "L1 contract",
	L2Hinge:    "L2 hinge",
	L3Domain:   "L3 domain",
	L4Engine:   "L4 engine",
	L5Adapter:  "L5 adapter",
	L6Surface:  "L6 surface",
	LTestKit:   "test kit",
}

func (l Layer) String() string { return layerNames[l] }

// mayImport is docs/plan/02-architecture.md §5, verbatim. A package may always
// import its own layer; these are the layers it may reach beyond that.
//
// Two entries carry the rules that do the real work:
//   - L1Contract imports nothing. The protocol is one language's view of
//     spec/, and it stays cheap to version by depending on nothing.
//   - L4Engine cannot reach L5Adapter. The engine declares ports; surfaces
//     inject the implementations. This is what lets the daemon, the desktop
//     shell and a test each supply their own without the engine knowing.
var mayImport = map[Layer][]Layer{
	L0Platform: {},
	L1Contract: {},
	L2Hinge:    {L0Platform, L1Contract},
	L3Domain:   {L0Platform, L1Contract, L2Hinge},
	L4Engine:   {L0Platform, L1Contract, L2Hinge, L3Domain},
	L5Adapter:  {L0Platform, L1Contract, L3Domain, L4Engine},
	L6Surface:  {L0Platform, L1Contract, L2Hinge, L3Domain, L4Engine, L5Adapter},
	LTestKit:   {L0Platform, L1Contract, L2Hinge, L3Domain, L4Engine, L5Adapter, L6Surface},
}

// packageLayer maps every package in the module to its layer. Paths are
// relative to the module root.
//
// A package that is not listed here fails the test. That is the point: the
// table cannot silently fall behind the tree, so adding a package forces the
// question "which layer is this?" to be answered on the way in rather than
// discovered later.
var packageLayer = map[string]Layer{
	// L1 — the language-neutral wire contract's public Go binding.
	"protocol": L1Contract,

	// L0 — platform. Everything that knows what an OS is lives here and
	// nowhere else. Most of these arrive at migration step 5.
	"internal/buildinfo":  L0Platform,
	"internal/paths":      L0Platform,
	"internal/shell":      L0Platform,
	"internal/atomicfile": L0Platform,
	"internal/keystore":   L0Platform,
	"internal/lock":       L0Platform,
	"internal/redact":     L0Platform,
	"internal/secret":     L0Platform,
	"internal/selfupdate": L0Platform,
	"internal/term":       L0Platform,
	"internal/xid":        L0Platform,

	// L2 — the event journal and fan-out hinge.
	"internal/bus": L2Hinge,

	// L3 — domain.
	"internal/provider": L3Domain,
	"internal/tools":    L3Domain,

	// L4 — engine. orchestrator is still inside engine/ until step 9.
	"internal/engine": L4Engine,

	// L5 — adapters: they implement the engine's ports.
	"internal/config":     L5Adapter,
	"internal/session":    L5Adapter,
	"internal/checkpoint": L5Adapter,
	"internal/stats":      L5Adapter,

	// L6 — surfaces. The only layer allowed to wire concrete types together.
	"internal/cli": L6Surface,
	"internal/tui": L6Surface,

	// Test kit — fakes, fixtures and the rules themselves.
	"internal/enginetest": LTestKit,
	"internal/arch":       LTestKit,
}

// commandPackages are the main packages. They are exempt from the layer rules
// because wiring everything together is precisely their job, but they are held
// to the third-party allow-list like everything else.
var commandPackages = map[string]bool{
	"cmd/kolk":      true,
	"cmd/kolk-mock": true,
}

// thirdParty is the per-package allow-list of non-stdlib imports. A package not
// listed here may import nothing outside the standard library and this module.
//
// The honest claim this buys is not "zero dependencies" but "zero dependencies
// below the surface layer, mechanically verified" — which is a claim that
// survives the product growing.
var thirdParty = map[string][]string{
	// Arrive with their packages at later steps; listed now so the budget is
	// a decision on the record rather than a surprise in a diff.
	"internal/term": {"golang.org/x/sys", "golang.org/x/term"},
	"internal/dash": {"modernc.org/sqlite", "modernc.org/libc"},
}

// osOwner names the single package allowed to touch each piece of the OS.
// Everywhere else, these are a build failure rather than a code-review
// convention — which is what makes "the engine touches no OS" a property
// instead of an intention.
var osOwner = map[string]string{
	"os/exec":          "internal/shell",
	"os.UserHomeDir":   "internal/paths",
	"os.UserConfigDir": "internal/paths",
	"os.UserCacheDir":  "internal/paths",
}

// forbiddenImports records negative package capabilities that are more exact
// than layer membership. secret values may be used by domain code, so secret
// itself must remain unable to read the environment or filesystem.
var forbiddenImports = map[string][]string{
	"internal/secret": {"io/fs", "os", "os/exec", "path/filepath", "syscall"},
	"internal/config": {
		"github.com/onembyte/kolkrabbi/internal/keystore",
		"github.com/onembyte/kolkrabbi/internal/secret",
	},
}

// authHeaders are request headers that carry a credential. Building one is
// internal/secret's job and nobody else's: an http.Header is a plain map of
// strings, so once a key is in one, no amount of care elsewhere can redact it
// out of a %+v on the request.
var authHeaders = []string{"Authorization", "X-Api-Key", "Api-Key", "Proxy-Authorization"}

// authHeaderOwner is the one package permitted to set them.
const authHeaderOwner = "internal/secret"

// bannedFilenames are names that describe nothing. A file is named for the one
// concept it holds; if no such name exists, the file is holding two.
var bannedFilenames = []string{
	"util.go", "utils.go", "helpers.go", "helper.go",
	"common.go", "misc.go", "types.go", "models.go", "base.go",
}

// bannedPackageNames are the same rule one level up.
var bannedPackageNames = []string{
	"util", "utils", "common", "helpers", "base", "types", "models", "misc", "lib", "pkg",
}

// tagRequiredSuffixes are filename suffixes the go command does NOT interpret.
// A file called shell_unix.go with no //go:build line compiles on Windows too —
// verified on go1.26.4 — and produces a silently wrong build rather than an
// error. That is why this is its own named rule.
//
// _windows.go and _darwin.go DO get an implicit constraint, but the rule is
// "every OS-divergent file states its constraint", with no exceptions, because
// an implicit rule you have to remember is one you will eventually get wrong.
var tagRequiredSuffixes = []string{
	"_unix.go", "_other.go", "_posix.go", "_stub.go", "_generic.go",
	"_windows.go", "_darwin.go", "_linux.go",
}

// violation is one entry in the ratchet: a rule the tree breaks today, which a
// named migration step will fix.
type violation struct {
	Pkg   string // package path, relative to the module root
	Rule  string // the rule id, as reported by arch_test.go
	Until string // the migration step that removes this entry
}

// knownViolations is debt with a due date. Every entry must still be a real
// violation — a fixed-but-still-listed entry fails the test — so this list can
// only shrink, and the day it empties the ratchet closes for good.
var knownViolations = []violation{
	// The engine still constructs its adapters instead of declaring ports.
	// This is the single largest structural debt in the tree: it is what
	// stands between here and a daemon, a desktop shell, or a deterministic
	// clock in a test.
	{Pkg: "internal/engine", Rule: "internal/session", Until: "step 9 — engine.Port interfaces"},
	{Pkg: "internal/engine", Rule: "internal/checkpoint", Until: "step 9 — engine.Port interfaces"},
	{Pkg: "internal/engine", Rule: "internal/stats", Until: "step 9 — engine.Port interfaces"},
}

func isKnown(pkg, rule string) bool {
	for _, v := range knownViolations {
		if v.Pkg == pkg && v.Rule == rule {
			return true
		}
	}
	return false
}
