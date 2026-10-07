//go:build !darwin

package shell

import "context"

// CheckOllamaPlatform has no extra OS-version probe on other systems. The
// installer independently refuses platforms without a supported native bundle.
func CheckOllamaPlatform(ctx context.Context) error { return ctx.Err() }
