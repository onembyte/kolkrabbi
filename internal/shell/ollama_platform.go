package shell

import (
	"fmt"
	"strconv"
	"strings"
)

func checkOllamaMacOS(version string) error {
	major, _, _ := strings.Cut(version, ".")
	n, err := strconv.Atoi(major)
	if err != nil || n < 14 {
		return fmt.Errorf("native Localia setup requires macOS 14 or newer (found %q)", version)
	}
	return nil
}
