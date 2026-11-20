//go:build windows

package store

import (
	"errors"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

// openShared opens path for reading with delete sharing, which os.Open leaves
// out, so replaceFile can still rename a new file over path while it is open.
func openShared(path string) (*os.File, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	handle, err := windows.CreateFile(name, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	return os.NewFile(uintptr(handle), path), nil
}

// fileRenameInfo is FILE_RENAME_INFO in its FileRenameInfoEx form, where the
// first field holds flags rather than a ReplaceIfExists boolean.
type fileRenameInfo struct {
	Flags          uint32
	RootDirectory  windows.Handle
	FileNameLength uint32
	FileName       [1]uint16
}

// replaceFile renames oldpath over newpath with POSIX semantics, which
// os.Rename's MoveFileEx lacks: MoveFileEx refuses to replace a file any
// reader has open, even one opened with delete sharing. Volumes or Windows
// versions without POSIX renames fall back to os.Rename.
func replaceFile(oldpath, newpath string) error {
	if err := posixRename(oldpath, newpath); err != nil {
		if errors.Is(err, windows.ERROR_INVALID_PARAMETER) || errors.Is(err, windows.ERROR_NOT_SUPPORTED) || errors.Is(err, windows.ERROR_INVALID_FUNCTION) {
			return os.Rename(oldpath, newpath)
		}
		return &os.LinkError{Op: "rename", Old: oldpath, New: newpath, Err: err}
	}
	return nil
}

func posixRename(oldpath, newpath string) error {
	target, err := filepath.Abs(newpath)
	if err != nil {
		return err
	}
	targetName, err := windows.UTF16FromString(`\\?\` + target)
	if err != nil {
		return err
	}
	sourceName, err := windows.UTF16PtrFromString(oldpath)
	if err != nil {
		return err
	}
	source, err := windows.CreateFile(sourceName, windows.DELETE|windows.SYNCHRONIZE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(source) //nolint:errcheck

	nameBytes := (len(targetName) - 1) * 2
	size := int(unsafe.Offsetof(fileRenameInfo{}.FileName)) + nameBytes + 2
	buffer := make([]byte, max(size, int(unsafe.Sizeof(fileRenameInfo{}))))
	info := (*fileRenameInfo)(unsafe.Pointer(&buffer[0]))
	info.Flags = windows.FILE_RENAME_REPLACE_IF_EXISTS | windows.FILE_RENAME_POSIX_SEMANTICS
	info.FileNameLength = uint32(nameBytes)
	copy(unsafe.Slice(&info.FileName[0], len(targetName)), targetName)
	return windows.SetFileInformationByHandle(source, windows.FileRenameInfoEx, &buffer[0], uint32(len(buffer)))
}
