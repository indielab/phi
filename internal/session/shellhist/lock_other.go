//go:build !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !windows

package shellhist

import (
	"fmt"
	"os"
)

func lockFile(_ *os.File) error {
	return fmt.Errorf("shell history locking is unsupported on this operating system")
}
func unlockFile(_ *os.File) {}
