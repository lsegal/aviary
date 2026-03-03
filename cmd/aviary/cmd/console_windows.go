package cmd

import "os"

// openConsole opens the Windows console input device (CONIN$) directly,
// which works even when os.Stdin is redirected or piped.
func openConsole() (*os.File, error) {
	return os.OpenFile("CONIN$", os.O_RDWR, 0)
}
