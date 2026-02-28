package store

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
)

// AppendJSONL marshals v as a single JSON line and appends it to path.
// The file is created if it does not exist.
func AppendJSONL(path string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshaling: %w", err)
	}
	data = append(data, '\n')

	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("opening %s: %w", path, err)
	}
	defer f.Close()

	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("writing to %s: %w", path, err)
	}
	return nil
}

// ReadJSONL reads all JSON lines from path and returns them as a slice.
func ReadJSONL[T any](path string) ([]T, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}
	defer f.Close()

	var results []T
	scanner := bufio.NewScanner(f)
	// Allow lines up to 1 MiB (for large memory entries).
	scanner.Buffer(make([]byte, 1<<20), 1<<20)

	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var v T
		if err := json.Unmarshal(line, &v); err != nil {
			// Skip malformed lines.
			continue
		}
		results = append(results, v)
	}
	if err := scanner.Err(); err != nil {
		return results, fmt.Errorf("reading %s: %w", path, err)
	}
	return results, nil
}

// RewriteJSONL overwrites path with the given entries (used by compactor).
func RewriteJSONL[T any](path string, entries []T) error {
	// Write to temp file in same directory, then rename for atomicity.
	tmp, err := os.CreateTemp(fmt.Sprintf("%s/..", path), ".tmp-rewrite-*")
	if err != nil {
		// Fallback: write directly if temp dir creation fails.
		return rewriteDirect(path, entries)
	}
	tmpName := tmp.Name()

	w := bufio.NewWriter(tmp)
	enc := json.NewEncoder(w)
	for _, v := range entries {
		if err := enc.Encode(v); err != nil {
			tmp.Close()
			os.Remove(tmpName)
			return fmt.Errorf("encoding entry: %w", err)
		}
	}
	if err := w.Flush(); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("flushing: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("closing temp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("renaming: %w", err)
	}
	return nil
}

func rewriteDirect[T any](path string, entries []T) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("creating %s: %w", path, err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, v := range entries {
		if err := enc.Encode(v); err != nil {
			return fmt.Errorf("encoding: %w", err)
		}
	}
	return nil
}
