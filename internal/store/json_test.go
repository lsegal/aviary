package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
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
		err := os.WriteFile(p, []byte(`{"name":"foo","value":42}`), 0o600)
		assert.NoError(t, err)

		got, err := ReadJSON[testItem](p)
		assert.NoError(t, err)
		assert.Equal(t, "foo", got.Name)
		assert.Equal(t, 42, got.Value)

	})

	t.Run("file_not_found", func(t *testing.T) {
		_, err := ReadJSON[testItem](filepath.Join(tmp, "nonexistent.json"))
		assert.Error(t, err)
		assert.True(t, strings.Contains(err.Error(), "reading"))

	})

	t.Run("bad_json", func(t *testing.T) {
		p := filepath.Join(tmp, "bad.json")
		err := os.WriteFile(p, []byte(`not valid json`), 0o600)
		assert.NoError(t, err)

		_, err = ReadJSON[testItem](p)
		assert.Error(t, err)
		assert.True(t, strings.Contains(err.Error(), "parsing"))

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
		err := WriteJSON(p, item)
		assert.NoError(t, err)

		got, err := ReadJSON[testItem](p)
		assert.NoError(t, err)
		assert.Equal(t, item.Name, got.Name)
		assert.Equal(t, item.Value, got.Value)

	})

	t.Run("dir_not_exist", func(t *testing.T) {
		// Parent directory does not exist; WriteJSON should auto-create it.
		p := filepath.Join(tmp, "nosuchdir", "out.json")
		err := WriteJSON(p, testItem{Name: "x"})
		assert.NoError(t, err)

		got, err := ReadJSON[testItem](p)
		assert.NoError(t, err)
		assert.Equal(t, "x", got.Name)

	})

	t.Run("unmarshalable", func(t *testing.T) {
		// A channel cannot be marshaled to JSON.
		p := filepath.Join(tmp, "bad.json")
		err := WriteJSON(p, make(chan int))
		assert.Error(t, err)
		assert.True(t, strings.Contains(err.Error(), "marshaling"))

	})
}

// TestDeleteJSON covers delete of existing file, delete of non-existent file,
// and delete when the path is invalid (permission-style error).
func TestDeleteJSON(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	t.Run("existing_file", func(t *testing.T) {
		p := filepath.Join(tmp, "del.json")
		err := os.WriteFile(p, []byte(`{}`), 0o600)
		assert.NoError(t, err)

		err = DeleteJSON(p)
		assert.NoError(t, err)

		_, err = os.Stat(p)
		assert.True(t, os.IsNotExist(err))

	})

	t.Run("not_exist", func(t *testing.T) {
		// Should NOT return an error when the file is already missing.
		p := filepath.Join(tmp, "gone.json")
		err := DeleteJSON(p)
		assert.NoError(t, err)

	})

	t.Run("error_other", func(t *testing.T) {
		// Attempting to remove a directory as if it were a file returns an
		// error that is not os.IsNotExist, so DeleteJSON should propagate it.
		dir := filepath.Join(tmp, "notempty")
		err := os.MkdirAll(filepath.Join(dir, "child"), 0o700)
		assert.NoError(t, err)

		// On most OSes, removing a non-empty directory path with os.Remove
		// fails with a non-not-exist error.
		err = DeleteJSON(dir)
		if err == nil {
			// Some platforms (e.g., Windows) may succeed or behave differently;
			// skip rather than fail.
			t.Skip("os.Remove on directory did not error on this platform")
		}
		assert.True(t, strings.Contains(err.Error(), "deleting"))

	})
}

// TestListJSON covers non-existent dir, matching files, non-matching files,
// directories inside the target dir, and corrupted JSON files.
func TestListJSON(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	t.Run("dir_not_exist", func(t *testing.T) {
		results, err := ListJSON[testItem](filepath.Join(tmp, "nope"), ".json")
		assert.NoError(t, err)
		assert.Nil(t, results)

	})

	t.Run("dir_is_a_file", func(t *testing.T) {
		// Pass a regular file as the directory argument. On most Unix systems,
		// os.ReadDir will return an error that is NOT os.IsNotExist, so
		// ListJSON should propagate it. On Windows, ReadDir on a file path
		// returns an IsNotExist error, which makes this branch unreachable;
		// skip in that case.
		p := filepath.Join(tmp, "notadir.json")
		err := os.WriteFile(p, []byte(`{}`), 0o600)
		assert.NoError(t, err)

		_, err = ListJSON[testItem](p, ".json")
		if err == nil {
			// Windows returns nil,nil for this case (IsNotExist path).
			t.Skip("platform returns nil error for ReadDir on file; skipping branch coverage")
		}
		assert.True(t, strings.Contains(err.Error(), "reading directory"))

	})

	t.Run("normal", func(t *testing.T) {
		dir := filepath.Join(tmp, "listdir")
		err := os.MkdirAll(dir, 0o700)
		assert.NoError(t, err)

		items := []testItem{
			{Name: "alpha", Value: 1},
			{Name: "beta", Value: 2},
		}
		for i, item := range items {
			p := filepath.Join(dir, filepath.Base(item.Name)+".json")
			_ = i
			data := []byte(`{"name":"` + item.Name + `","value":` + itoa(item.Value) + `}`)
			err := os.WriteFile(p, data, 0o600)
			assert.NoError(t, err)

		}

		// Non-matching file (wrong suffix) — should be ignored.
		err = os.WriteFile(filepath.Join(dir, "ignore.txt"), []byte(`{"name":"skip"}`), 0o600)
		assert.NoError(t, err)

		// Corrupted JSON file — should be skipped.
		err = os.WriteFile(filepath.Join(dir, "corrupt.json"), []byte(`not json`), 0o600)
		assert.NoError(t, err)

		// Sub-directory — should be skipped.
		err = os.MkdirAll(filepath.Join(dir, "subdir.json"), 0o700)
		assert.NoError(t, err)

		results, err := ListJSON[testItem](dir, ".json")
		assert.NoError(t, err)
		assert.Equal(t, 2, len(results))

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
	err := os.MkdirAll(dir, 0o700)
	assert.NoError(t, err)

	// Write two items.
	items := []testItem{
		{Name: "job1", Value: 100},
		{Name: "job2", Value: 200},
	}
	for _, item := range items {
		p := filepath.Join(dir, item.Name+".json")
		err := WriteJSON(p, item)
		assert.NoError(t, err)

	}

	// List should return both.
	list, err := ListJSON[testItem](dir, ".json")
	assert.NoError(t, err)
	assert.Equal(t, 2, len(list))

	// Read one directly.
	p := filepath.Join(dir, "job1.json")
	got, err := ReadJSON[testItem](p)
	assert.NoError(t, err)
	assert.Equal(t, "job1", got.Name)
	assert.Equal(t, 100, got.Value)

	// Delete one.
	err = DeleteJSON(p)
	assert.NoError(t, err)

	// Now list should return only one.
	list2, err := ListJSON[testItem](dir, ".json")
	assert.NoError(t, err)
	assert.Equal(t, 1, len(list2))

}
