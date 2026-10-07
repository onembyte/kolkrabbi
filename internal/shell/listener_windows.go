//go:build windows

package shell

import (
	"context"
	"fmt"
)

func ProcessOwnsLoopbackListener(context.Context, int, string) (bool, error) {
	return false, fmt.Errorf("managed Localia stop cannot verify the listener on Windows")
}
