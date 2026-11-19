package projects

import "golang.org/x/sys/windows"

// diskFreeBytes reports the bytes available to the current user on the
// volume holding dir.
func diskFreeBytes(dir string) (uint64, error) {
	path, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return 0, err
	}
	var available, total, free uint64
	if err := windows.GetDiskFreeSpaceEx(path, &available, &total, &free); err != nil {
		return 0, err
	}
	return available, nil
}
