//go:build !linux

package shell

import (
	"fmt"
	"io"
	"runtime"
)

// Off linux the child entry can only refuse: there is no Landlock here, and a
// child that ran the command anyway would be a sandbox that quietly is not one.
func landlockChildMain(_ []string, stderr io.Writer) int {
	fmt.Fprintf(stderr, "kolk: the Landlock child entry exists only on linux; this is %s. The command was not run.\n", runtime.GOOS)
	return 125
}
