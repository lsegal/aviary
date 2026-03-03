//go:build !windows

package cmd

import "os"

// openConsole opens /dev/tty directly so reads work even when stdin is redirected.
func openConsole() (*os.File, error) {
	return os.OpenFile("/dev/tty", os.O_RDWR, 0)
}
