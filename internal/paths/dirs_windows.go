//go:build windows

package paths

import (
	"path/filepath"
)

// resolve implements the Windows Known Folders layout.
//
// The split that matters: config goes in %AppData%, which ROAMS to a domain
// profile server, and everything in Data goes in %LocalAppData%, which does
// not. Credentials are Data. Putting a key somewhere that silently copies
// itself onto a corporate file server is the kind of mistake that is only
// visible in an audit log, months later.
//
// Windows is advisory until migration step 13; this is a real implementation
// rather than a stub, because the directory layout is a decision and not a
// platform detail, and getting it wrong later means migrating people's data.
func resolve(getenv func(string) string, home string) Dirs {
	var d Dirs

	roaming := getenv("AppData")
	local := getenv("LocalAppData")

	// A profile old or unusual enough to lack these still has a home directory.
	if roaming == "" && home != "" {
		roaming = filepath.Join(home, "AppData", "Roaming")
	}
	if local == "" && home != "" {
		local = filepath.Join(home, "AppData", "Local")
	}
	// If only one exists, prefer keeping state local over losing it entirely.
	if local == "" {
		local = roaming
	}

	d.Config = override(getenv, EnvConfigDir)
	if d.Config == "" && roaming != "" {
		d.Config = filepath.Join(roaming, app)
	}

	d.Data = override(getenv, EnvDataDir)
	if d.Data == "" && local != "" {
		d.Data = filepath.Join(local, app)
	}

	d.Cache = override(getenv, EnvCacheDir)
	if d.Cache == "" && local != "" {
		d.Cache = filepath.Join(local, app, "cache")
	}

	return d
}
