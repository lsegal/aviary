package store

import "io"

// ReadFile reads the whole file at path. Unlike os.ReadFile, its handle never
// blocks WriteJSON from atomically renaming a new file over path: on Windows a
// reader that opens without delete sharing makes that rename fail, so a file
// polled while another goroutine or process saves it would intermittently
// refuse the save.
func ReadFile(path string) ([]byte, error) {
	f, err := openShared(path)
	if err != nil {
		return nil, err
	}
	defer f.Close() //nolint:errcheck
	return io.ReadAll(f)
}
