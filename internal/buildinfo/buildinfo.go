// Package buildinfo reports which build of kolk is running.
//
// Release builds stamp the values in with -ldflags -X. Everything else — a
// developer's `go build`, and `go install …@latest`, which is how most people
// will get kolk — stamps nothing, so the values are recovered from the module
// data the toolchain embeds anyway. A build that cannot say what it is makes
// every bug report cost an extra round trip.
package buildinfo

import (
	"runtime"
	"runtime/debug"
	"strings"
)

// Set by -ldflags -X github.com/onembyte/kolkrabbi/internal/buildinfo.version=…
// at release time. Empty in every other build, which is the normal case.
var (
	version string
	commit  string
	date    string
)

// Info is one build's identity.
type Info struct {
	Version string // semver tag, a pseudo-version, or "dev"
	Commit  string // short commit hash, "+dirty" when the tree was modified
	Date    string // RFC3339 build or commit time, "" if unknown
	Go      string // the toolchain that built it
	OS      string
	Arch    string
}

// Get resolves the running build's identity. It never fails: an unknown field
// is empty or "dev", never a lie.
func Get() Info {
	i := Info{
		Version: version,
		Commit:  commit,
		Date:    date,
		Go:      runtime.Version(),
		OS:      runtime.GOOS,
		Arch:    runtime.GOARCH,
	}

	bi, ok := debug.ReadBuildInfo()
	if !ok {
		if i.Version == "" {
			i.Version = "dev"
		}
		return i
	}

	if i.Version == "" {
		// "(devel)" is what the toolchain reports for an untagged build; say
		// "dev", which is what a person would write.
		if v := bi.Main.Version; v != "" && v != "(devel)" {
			i.Version = v
		} else {
			i.Version = "dev"
		}
	}

	var revision, modified string
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			revision = s.Value
		case "vcs.time":
			if i.Date == "" {
				i.Date = s.Value
			}
		case "vcs.modified":
			modified = s.Value
		}
	}
	if i.Commit == "" && revision != "" {
		i.Commit = shortCommit(revision)
	}
	if modified == "true" && i.Commit != "" && !strings.HasSuffix(i.Commit, "+dirty") {
		i.Commit += "+dirty"
	}
	return i
}

func shortCommit(rev string) string {
	if len(rev) > 12 {
		return rev[:12]
	}
	return rev
}

// String is the one line `kolk version` prints.
func (i Info) String() string {
	var b strings.Builder
	b.WriteString("kolk ")
	b.WriteString(i.Version)
	if i.Commit != "" {
		b.WriteString(" (")
		b.WriteString(i.Commit)
		if i.Date != "" {
			b.WriteString(", ")
			b.WriteString(i.Date)
		}
		b.WriteString(")")
	}
	b.WriteString(" ")
	b.WriteString(i.Go)
	b.WriteString(" ")
	b.WriteString(i.OS)
	b.WriteString("/")
	b.WriteString(i.Arch)
	return b.String()
}
