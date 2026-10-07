//go:build windows

package shell

import (
	"context"
	"fmt"
)

func SignalManagedProcessGroup(context.Context, int) error {
	return fmt.Errorf("managed Localia stop is unavailable on Windows")
}
