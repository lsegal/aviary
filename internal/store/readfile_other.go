//go:build !windows

package store

import "os"

// openShared opens path for reading. Outside Windows an open file never
// blocks a rename over it.
func openShared(path string) (*os.File, error) {
	return os.Open(path)
}

// replaceFile renames oldpath over newpath.
func replaceFile(oldpath, newpath string) error {
	return os.Rename(oldpath, newpath)
}
