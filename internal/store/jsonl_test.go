package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// jsonlItem is the type used for JSONL round-trip tests.
type jsonlItem struct {
	ID    string `json:"id"`
	Score int    `json:"score"`
}

// TestAppendJSONL covers normal appending, file creation, open-error, and
// marshal-error paths.
func TestAppendJSONL(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	t.Run("creates_and_appends", func(t *testing.T) {
		p := filepath.Join(tmp, "append.jsonl")

		// Append first entry — file does not yet exist.
		if err := AppendJSONL(p, jsonlItem{ID: "a", Score: 1}); err != nil {
			t.Fatalf("AppendJSONL() first: %v", err)
		}
		// Append second entry.
		if err := AppendJSONL(p, jsonlItem{ID: "b", Score: 2}); err != nil {
			t.Fatalf("AppendJSONL() second: %v", err)
		}

		entries, err := ReadJSONL[jsonlItem](p)
		if err != nil {
			t.Fatalf("ReadJSONL(): %v", err)
		}
		if len(entries) != 2 {
			t.Errorf("expected 2 entries, got %d", len(entries))
		}
	})

	t.Run("open_error", func(t *testing.T) {
		// Parent directory does not exist; AppendJSONL should auto-create it.
		p := filepath.Join(tmp, "nodir", "append.jsonl")
		err := AppendJSONL(p, jsonlItem{ID: "x"})
		if err != nil {
			t.Fatalf("unexpected error when parent dir missing: %v", err)
		}
		items, err := ReadJSONL[jsonlItem](p)
		if err != nil {
			t.Fatalf("reading back appended file: %v", err)
		}
		if len(items) != 1 || items[0].ID != "x" {
			t.Errorf("expected [{x 0}], got %v", items)
		}
	})

	t.Run("marshal_error", func(t *testing.T) {
		// A channel cannot be marshaled.
		p := filepath.Join(tmp, "marshal.jsonl")
		err := AppendJSONL(p, make(chan int))
		if err == nil {
			t.Fatal("expected marshal error, got nil")
		}
		if !strings.Contains(err.Error(), "marshaling") {
			t.Errorf("expected 'marshaling' in error, got: %v", err)
		}
	})
}

// TestReadJSONL covers missing file, empty lines, malformed lines, and normal
// reading.
func TestReadJSONL(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	t.Run("file_not_exist", func(t *testing.T) {
		results, err := ReadJSONL[jsonlItem](filepath.Join(tmp, "nope.jsonl"))
		if err != nil {
			t.Errorf("expected nil error for missing file, got: %v", err)
		}
		if results != nil {
			t.Errorf("expected nil results for missing file, got: %v", results)
		}
	})

	t.Run("open_error_not_notexist", func(t *testing.T) {
		// Open a path of the form <file>/<name> where <file> is a regular
		// file, not a directory. This produces an open error that is NOT
		// os.IsNotExist, so ReadJSONL should propagate it.
		blocker := filepath.Join(tmp, "blocker")
		if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := ReadJSONL[jsonlItem](filepath.Join(blocker, "sub.jsonl"))
		if err == nil {
			// Some platforms classify this case as not-exist and ReadJSONL
			// intentionally returns nil,nil for not-exist paths.
			t.Skip("platform returns nil error for file/subpath open; skipping branch assertion")
		}
		if !strings.Contains(err.Error(), "opening") {
			t.Errorf("expected 'opening' in error, got: %v", err)
		}
	})

	t.Run("valid_lines", func(t *testing.T) {
		p := filepath.Join(tmp, "valid.jsonl")
		content := `{"id":"x","score":10}` + "\n" + `{"id":"y","score":20}` + "\n"
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		results, err := ReadJSONL[jsonlItem](p)
		if err != nil {
			t.Fatalf("ReadJSONL(): %v", err)
		}
		if len(results) != 2 {
			t.Errorf("expected 2 results, got %d", len(results))
		}
	})

	t.Run("empty_and_bad_lines", func(t *testing.T) {
		p := filepath.Join(tmp, "mixed.jsonl")
		// An empty line and a malformed line should both be skipped.
		content := `{"id":"ok","score":5}` + "\n" +
			"\n" +
			`not valid json` + "\n"
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		results, err := ReadJSONL[jsonlItem](p)
		if err != nil {
			t.Fatalf("ReadJSONL() unexpected error: %v", err)
		}
		if len(results) != 1 {
			t.Errorf("expected 1 result (skipping bad lines), got %d", len(results))
		}
	})

	t.Run("empty_file", func(t *testing.T) {
		p := filepath.Join(tmp, "empty.jsonl")
		if err := os.WriteFile(p, []byte{}, 0o600); err != nil {
			t.Fatal(err)
		}
		results, err := ReadJSONL[jsonlItem](p)
		if err != nil {
			t.Fatalf("ReadJSONL() on empty file: %v", err)
		}
		if len(results) != 0 {
			t.Errorf("expected 0 results for empty file, got %d", len(results))
		}
	})

	t.Run("scanner_error_line_too_long", func(t *testing.T) {
		// Write a line exceeding the 16 MiB scanner buffer to trigger
		// scanner.Err() returning bufio.ErrTooLong.
		p := filepath.Join(tmp, "toolong.jsonl")
		// Build a JSON string value larger than 16 MiB.
		// The JSON line must be a single line (no embedded newlines).
		huge := make([]byte, (16<<20)+1)
		for i := range huge {
			huge[i] = 'a'
		}
		// Wrap it as a JSON string in an object so the line is valid-ish JSON
		// but way over the scanner buffer limit.
		line := []byte(`{"id":"` + string(huge) + `","score":0}` + "\n")
		if err := os.WriteFile(p, line, 0o600); err != nil {
			t.Fatal(err)
		}
		results, err := ReadJSONL[jsonlItem](p)
		if err == nil {
			t.Fatal("expected scanner error for huge line, got nil")
		}
		if !strings.Contains(err.Error(), "reading") {
			t.Errorf("expected 'reading' in error, got: %v", err)
		}
		// results may be nil or empty — either is acceptable.
		_ = results
	})
}

// TestRewriteJSONL covers the normal (temp-file) path, which uses CreateTemp
// in the sibling directory.
func TestRewriteJSONL(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	t.Run("normal", func(t *testing.T) {
		// Seed a file with some content.
		p := filepath.Join(tmp, "rewrite.jsonl")
		seed := []jsonlItem{{ID: "old", Score: 0}}
		if err := RewriteJSONL(p, seed); err != nil {
			t.Fatalf("RewriteJSONL() seed: %v", err)
		}

		// Now rewrite with new entries.
		entries := []jsonlItem{
			{ID: "a", Score: 10},
			{ID: "b", Score: 20},
		}
		if err := RewriteJSONL(p, entries); err != nil {
			t.Fatalf("RewriteJSONL() rewrite: %v", err)
		}

		results, err := ReadJSONL[jsonlItem](p)
		if err != nil {
			t.Fatalf("ReadJSONL() after RewriteJSONL: %v", err)
		}
		if len(results) != 2 {
			t.Errorf("expected 2 entries after rewrite, got %d", len(results))
		}
	})

	t.Run("empty_entries", func(t *testing.T) {
		p := filepath.Join(tmp, "rewrite_empty.jsonl")
		if err := RewriteJSONL(p, []jsonlItem{}); err != nil {
			t.Fatalf("RewriteJSONL() with empty entries: %v", err)
		}
		results, err := ReadJSONL[jsonlItem](p)
		if err != nil {
			t.Fatalf("ReadJSONL() after empty rewrite: %v", err)
		}
		if len(results) != 0 {
			t.Errorf("expected 0 entries, got %d", len(results))
		}
	})

	t.Run("fallback_rewrite_direct", func(t *testing.T) {
		// When RewriteJSONL is given a path whose parent directory does not
		// exist, os.CreateTemp will fail, which triggers rewriteDirect.
		// rewriteDirect will also fail if the parent doesn't exist, so we
		// create the parent first but then pass a path that tricks CreateTemp
		// into looking at a non-existent grandparent.
		//
		// Actually the simplest approach is to call rewriteDirect directly since
		// it is unexported but visible within the same package.
		p := filepath.Join(tmp, "direct.jsonl")
		entries := []jsonlItem{{ID: "d1", Score: 5}}
		if err := rewriteDirect(p, entries); err != nil {
			t.Fatalf("rewriteDirect() unexpected error: %v", err)
		}
		results, err := ReadJSONL[jsonlItem](p)
		if err != nil {
			t.Fatalf("ReadJSONL() after rewriteDirect: %v", err)
		}
		if len(results) != 1 || results[0].ID != "d1" {
			t.Errorf("rewriteDirect round-trip: got %+v", results)
		}
	})
}

// TestRewriteDirect_Error verifies rewriteDirect returns an error when the
// target file cannot be created.
func TestRewriteDirect_Error(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	// Path whose parent does not exist.
	p := filepath.Join(tmp, "nosuchdir", "data.jsonl")
	err := rewriteDirect(p, []jsonlItem{{ID: "x"}})
	if err == nil {
		t.Fatal("expected error from rewriteDirect when parent missing, got nil")
	}
	if !strings.Contains(err.Error(), "creating") {
		t.Errorf("expected 'creating' in error, got: %v", err)
	}
}

// TestRewriteDirect_EncodeError verifies rewriteDirect returns an error when
// encoding an entry fails. Since rewriteDirect is generic, we instantiate it
// with chan int, which cannot be JSON-encoded.
func TestRewriteDirect_EncodeError(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	p := filepath.Join(tmp, "encode_err.jsonl")
	err := rewriteDirect(p, []chan int{make(chan int)})
	if err == nil {
		t.Fatal("expected encode error from rewriteDirect, got nil")
	}
	if !strings.Contains(err.Error(), "encoding") {
		t.Errorf("expected 'encoding' in error, got: %v", err)
	}
}

// TestRewriteJSONL_EncodeError verifies the encode-error branch inside the
// temp-file code path of RewriteJSONL.
func TestRewriteJSONL_EncodeError(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	// Place the file in a directory that exists so CreateTemp succeeds, then
	// pass an entry that cannot be encoded to reach the error branch.
	p := filepath.Join(tmp, "encode_err2.jsonl")
	err := RewriteJSONL(p, []chan int{make(chan int)})
	if err == nil {
		t.Fatal("expected encode error from RewriteJSONL, got nil")
	}
	if !strings.Contains(err.Error(), "encoding entry") {
		t.Errorf("expected 'encoding entry' in error, got: %v", err)
	}
}

// TestRewriteJSONL_DeepPath verifies that RewriteJSONL auto-creates deeply
// nested parent directories when they do not exist.
func TestRewriteJSONL_DeepPath(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	// Use a path with multiple missing parents; RewriteJSONL should create them.
	p := filepath.Join(tmp, "noexist", "sub", "file.jsonl")

	err := RewriteJSONL(p, []jsonlItem{{ID: "fb", Score: 1}})
	if err != nil {
		t.Fatalf("unexpected error from RewriteJSONL with deep path: %v", err)
	}
	items, err := ReadJSONL[jsonlItem](p)
	if err != nil {
		t.Fatalf("reading back rewritten file: %v", err)
	}
	if len(items) != 1 || items[0].ID != "fb" {
		t.Errorf("expected [{fb 1}], got %v", items)
	}
}

// TestIntegration_JSONLRoundTrip exercises AppendJSONL + ReadJSONL +
// RewriteJSONL together.
func TestIntegration_JSONLRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	p := filepath.Join(tmp, "session.jsonl")

	// Append several entries.
	for i := 0; i < 5; i++ {
		if err := AppendJSONL(p, jsonlItem{ID: "e", Score: i}); err != nil {
			t.Fatalf("AppendJSONL(%d): %v", i, err)
		}
	}

	all, err := ReadJSONL[jsonlItem](p)
	if err != nil {
		t.Fatalf("ReadJSONL(): %v", err)
	}
	if len(all) != 5 {
		t.Errorf("expected 5 entries, got %d", len(all))
	}

	// Compact: keep only those with Score >= 2.
	var keep []jsonlItem
	for _, e := range all {
		if e.Score >= 2 {
			keep = append(keep, e)
		}
	}

	if err := RewriteJSONL(p, keep); err != nil {
		t.Fatalf("RewriteJSONL(): %v", err)
	}

	final, err := ReadJSONL[jsonlItem](p)
	if err != nil {
		t.Fatalf("ReadJSONL() after rewrite: %v", err)
	}
	if len(final) != 3 {
		t.Errorf("expected 3 entries after compaction, got %d", len(final))
	}
}
