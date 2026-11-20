//go:build !windows

package projects

import "golang.org/x/sys/unix"

// diskFreeBytes reports the bytes available to unprivileged users on the
// volume holding dir.
func diskFreeBytes(dir string) (uint64, error) {
	var stat unix.Statfs_t
	if err := unix.Statfs(dir, &stat); err != nil {
		return 0, err
	}
	return uint64(stat.Bavail) * uint64(stat.Bsize), nil
}
