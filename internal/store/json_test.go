package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testItem is a simple struct used as the type parameter in generic functions.
type testItem struct {
	Name  string `json:"name"`
	Value int    `json:"value"`
}

// TestReadJSON covers success, file-not-found, and bad-JSON paths.
func TestReadJSON(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	t.Run("success", func(t *testing.T) {
		p := filepath.Join(tmp, "item.json")
		if err := os.WriteFile(p, []byte(`{"name":"foo","value":42}`), 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := ReadJSON[testItem](p)
		if err != nil {
			t.Fatalf("ReadJSON() error: %v", err)
		}
		if got.Name != "foo" || got.Value != 42 {
			t.Errorf("ReadJSON() = %+v; want {Name:foo Value:42}", got)
		}
	})

	t.Run("file_not_found", func(t *testing.T) {
		_, err := ReadJSON[testItem](filepath.Join(tmp, "nonexistent.json"))
		if err == nil {
			t.Fatal("expected error for missing file, got nil")
		}
		if !strings.Contains(err.Error(), "reading") {
			t.Errorf("expected 'reading' in error, got: %v", err)
		}
	})

	t.Run("bad_json", func(t *testing.T) {
		p := filepath.Join(tmp, "bad.json")
		if err := os.WriteFile(p, []byte(`not valid json`), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := ReadJSON[testItem](p)
		if err == nil {
			t.Fatal("expected error for bad JSON, got nil")
		}
		if !strings.Contains(err.Error(), "parsing") {
			t.Errorf("expected 'parsing' in error, got: %v", err)
		}
	})
}

// TestWriteJSON covers success and the case where the destination directory
// does not exist (temp file creation fails).
func TestWriteJSON(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	t.Run("success", func(t *testing.T) {
		p := filepath.Join(tmp, "out.json")
		item := testItem{Name: "bar", Value: 99}
		if err := WriteJSON(p, item); err != nil {
			t.Fatalf("WriteJSON() error: %v", err)
		}
		got, err := ReadJSON[testItem](p)
		if err != nil {
			t.Fatalf("ReadJSON() after WriteJSON error: %v", err)
		}
		if got.Name != item.Name || got.Value != item.Value {
			t.Errorf("round-trip mismatch: got %+v, want %+v", got, item)
		}
	})

	t.Run("dir_not_exist", func(t *testing.T) {
		// Provide a path whose parent directory does not exist → CreateTemp fails.
		p := filepath.Join(tmp, "nosuchdir", "out.json")
		err := WriteJSON(p, testItem{Name: "x"})
		if err == nil {
			t.Fatal("expected error when parent dir missing, got nil")
		}
		if !strings.Contains(err.Error(), "creating temp file") {
			t.Errorf("expected 'creating temp file' in error, got: %v", err)
		}
	})

	t.Run("unmarshalable", func(t *testing.T) {
		// A channel cannot be marshaled to JSON.
		p := filepath.Join(tmp, "bad.json")
		err := WriteJSON(p, make(chan int))
		if err == nil {
			t.Fatal("expected marshal error, got nil")
		}
		if !strings.Contains(err.Error(), "marshaling") {
			t.Errorf("expected 'marshaling' in error, got: %v", err)
		}
	})
}

// TestDeleteJSON covers delete of existing file, delete of non-existent file,
// and delete when the path is invalid (permission-style error).
func TestDeleteJSON(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	t.Run("existing_file", func(t *testing.T) {
		p := filepath.Join(tmp, "del.json")
		if err := os.WriteFile(p, []byte(`{}`), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := DeleteJSON(p); err != nil {
			t.Fatalf("DeleteJSON() unexpected error: %v", err)
		}
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("file should not exist after DeleteJSON")
		}
	})

	t.Run("not_exist", func(t *testing.T) {
		// Should NOT return an error when the file is already missing.
		p := filepath.Join(tmp, "gone.json")
		if err := DeleteJSON(p); err != nil {
			t.Errorf("DeleteJSON() on missing file returned error: %v", err)
		}
	})

	t.Run("error_other", func(t *testing.T) {
		// Attempting to remove a directory as if it were a file returns an
		// error that is not os.IsNotExist, so DeleteJSON should propagate it.
		dir := filepath.Join(tmp, "notempty")
		if err := os.MkdirAll(filepath.Join(dir, "child"), 0o700); err != nil {
			t.Fatal(err)
		}
		// On most OSes, removing a non-empty directory path with os.Remove
		// fails with a non-not-exist error.
		err := DeleteJSON(dir)
		if err == nil {
			// Some platforms (e.g., Windows) may succeed or behave differently;
			// skip rather than fail.
			t.Skip("os.Remove on directory did not error on this platform")
		}
		if !strings.Contains(err.Error(), "deleting") {
			t.Errorf("expected 'deleting' in error, got: %v", err)
		}
	})
}

// TestListJSON covers non-existent dir, matching files, non-matching files,
// directories inside the target dir, and corrupted JSON files.
func TestListJSON(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	t.Run("dir_not_exist", func(t *testing.T) {
		results, err := ListJSON[testItem](filepath.Join(tmp, "nope"), ".json")
		if err != nil {
			t.Errorf("expected nil error for missing dir, got: %v", err)
		}
		if results != nil {
			t.Errorf("expected nil results for missing dir, got: %v", results)
		}
	})

	t.Run("dir_is_a_file", func(t *testing.T) {
		// Pass a regular file as the directory argument. On most Unix systems,
		// os.ReadDir will return an error that is NOT os.IsNotExist, so
		// ListJSON should propagate it. On Windows, ReadDir on a file path
		// returns an IsNotExist error, which makes this branch unreachable;
		// skip in that case.
		p := filepath.Join(tmp, "notadir.json")
		if err := os.WriteFile(p, []byte(`{}`), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := ListJSON[testItem](p, ".json")
		if err == nil {
			// Windows returns nil,nil for this case (IsNotExist path).
			t.Skip("platform returns nil error for ReadDir on file; skipping branch coverage")
		}
		if !strings.Contains(err.Error(), "reading directory") {
			t.Errorf("expected 'reading directory' in error, got: %v", err)
		}
	})

	t.Run("normal", func(t *testing.T) {
		dir := filepath.Join(tmp, "listdir")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}

		items := []testItem{
			{Name: "alpha", Value: 1},
			{Name: "beta", Value: 2},
		}
		for i, item := range items {
			p := filepath.Join(dir, filepath.Base(item.Name)+".json")
			_ = i
			data := []byte(`{"name":"` + item.Name + `","value":` + itoa(item.Value) + `}`)
			if err := os.WriteFile(p, data, 0o600); err != nil {
				t.Fatal(err)
			}
		}

		// Non-matching file (wrong suffix) — should be ignored.
		if err := os.WriteFile(filepath.Join(dir, "ignore.txt"), []byte(`{"name":"skip"}`), 0o600); err != nil {
			t.Fatal(err)
		}

		// Corrupted JSON file — should be skipped.
		if err := os.WriteFile(filepath.Join(dir, "corrupt.json"), []byte(`not json`), 0o600); err != nil {
			t.Fatal(err)
		}

		// Sub-directory — should be skipped.
		if err := os.MkdirAll(filepath.Join(dir, "subdir.json"), 0o700); err != nil {
			t.Fatal(err)
		}

		results, err := ListJSON[testItem](dir, ".json")
		if err != nil {
			t.Fatalf("ListJSON() error: %v", err)
		}
		if len(results) != 2 {
			t.Errorf("ListJSON() returned %d items; want 2", len(results))
		}
	})
}

// itoa is a tiny helper used in test data construction.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	buf := make([]byte, 0, 10)
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	if neg {
		buf = append([]byte{'-'}, buf...)
	}
	return string(buf)
}

// TestIntegration_JSONRoundTrip exercises WriteJSON + ReadJSON + ListJSON +
// DeleteJSON together, simulating a typical usage pattern.
func TestIntegration_JSONRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	dir := filepath.Join(tmp, "jobs")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}

	// Write two items.
	items := []testItem{
		{Name: "job1", Value: 100},
		{Name: "job2", Value: 200},
	}
	for _, item := range items {
		p := filepath.Join(dir, item.Name+".json")
		if err := WriteJSON(p, item); err != nil {
			t.Fatalf("WriteJSON(%q): %v", p, err)
		}
	}

	// List should return both.
	list, err := ListJSON[testItem](dir, ".json")
	if err != nil {
		t.Fatalf("ListJSON(): %v", err)
	}
	if len(list) != 2 {
		t.Errorf("ListJSON() count = %d; want 2", len(list))
	}

	// Read one directly.
	p := filepath.Join(dir, "job1.json")
	got, err := ReadJSON[testItem](p)
	if err != nil {
		t.Fatalf("ReadJSON(): %v", err)
	}
	if got.Name != "job1" || got.Value != 100 {
		t.Errorf("ReadJSON() = %+v; want {job1 100}", got)
	}

	// Delete one.
	if err := DeleteJSON(p); err != nil {
		t.Fatalf("DeleteJSON(): %v", err)
	}

	// Now list should return only one.
	list2, err := ListJSON[testItem](dir, ".json")
	if err != nil {
		t.Fatalf("ListJSON() after delete: %v", err)
	}
	if len(list2) != 1 {
		t.Errorf("ListJSON() after delete count = %d; want 1", len(list2))
	}
}
