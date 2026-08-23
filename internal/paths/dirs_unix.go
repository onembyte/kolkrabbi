//go:build !windows

package paths

import (
	"path/filepath"
)

// resolve implements the XDG Base Directory layout on every Unix, macOS
// included.
//
// macOS deliberately does NOT use ~/Library/Application Support, against what
// os.UserConfigDir would return. Three reasons, in order of weight: the
// prototype already wrote ~/.config/kolk and moving it would strand people;
// developers symlink dotfiles and Application Support is hostile to both `cd`
// and a dotfiles repo; and it keeps one Unix code path instead of two, which is
// one fewer place for the three directories to disagree.
//
// It takes getenv and home as parameters rather than reading the process
// environment, so the whole table is testable without touching $HOME.
func resolve(getenv func(string) string, home string) Dirs {
	var d Dirs

	d.Config = override(getenv, EnvConfigDir)
	if d.Config == "" {
		d.Config = xdg(getenv, "XDG_CONFIG_HOME", home, ".config")
	}

	d.Data = override(getenv, EnvDataDir)
	if d.Data == "" {
		d.Data = xdg(getenv, "XDG_DATA_HOME", home, ".local", "share")
	}

	d.Cache = override(getenv, EnvCacheDir)
	if d.Cache == "" {
		d.Cache = xdg(getenv, "XDG_CACHE_HOME", home, ".cache")
	}

	return d
}

// xdg returns $VAR/kolk, or home/fallback…/kolk when the variable is unset.
//
// The spec says a relative XDG value must be ignored, not resolved: honouring
// one would put state somewhere that depends on the working directory, and
// nobody who sets XDG_DATA_HOME=data means "wherever I happen to run this".
func xdg(getenv func(string) string, name, home string, fallback ...string) string {
	if v := getenv(name); filepath.IsAbs(v) {
		return filepath.Join(v, app)
	}
	if home == "" {
		return ""
	}
	parts := append([]string{home}, fallback...)
	return filepath.Join(append(parts, app)...)
}
