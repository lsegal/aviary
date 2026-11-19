package mcp

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/hexops/gotextdiff"
	"github.com/hexops/gotextdiff/myers"
	"github.com/hexops/gotextdiff/span"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lsegal/aviary/internal/agent"
)

// maxDiffBytes caps the unified diff returned by editing tools.
const maxDiffBytes = 64 * 1024

type fileEditOp struct {
	OldString  string `json:"old_string"`
	NewString  string `json:"new_string"`
	ReplaceAll bool   `json:"replace_all,omitempty"`
}

type fileEditArgs struct {
	Path       string `json:"path"`
	OldString  string `json:"old_string"`
	NewString  string `json:"new_string"`
	ReplaceAll bool   `json:"replace_all,omitempty"`
}

type fileMultiEditArgs struct {
	Path  string       `json:"path"`
	Edits []fileEditOp `json:"edits"`
}

func registerFileEditTools(s *sdkmcp.Server) {
	addTool(s, &sdkmcp.Tool{
		Name:        "file_edit",
		Description: "Replace an exact string in a file within the current agent's filesystem allowlist, without reading or rewriting the whole file. Arguments: path, old_string, new_string, replace_all(optional). Fails when old_string is missing, or appears more than once without replace_all; add surrounding lines to make it unique. Keeps the file's line endings. Returns a unified diff.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args fileEditArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		return applyFileEdits(ctx, args.Path, []fileEditOp{{OldString: args.OldString, NewString: args.NewString, ReplaceAll: args.ReplaceAll}})
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "file_multi_edit",
		Description: "Apply several exact-string replacements to one file in order, within the current agent's filesystem allowlist. Arguments: path, edits (array of {old_string, new_string, replace_all(optional)}). Each edit sees the result of the previous one. Either every edit applies or the file is left unchanged. Returns a unified diff.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args fileMultiEditArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		if len(args.Edits) == 0 {
			return nil, struct{}{}, fmt.Errorf("edits is required")
		}
		return applyFileEdits(ctx, args.Path, args.Edits)
	})
}

func applyFileEdits(ctx context.Context, rawPath string, edits []fileEditOp) (*sdkmcp.CallToolResult, struct{}, error) {
	path, _, err := resolveAllowedAgentPath(ctx, rawPath, "edit")
	if err != nil {
		return nil, struct{}{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, struct{}{}, fmt.Errorf("reading file metadata: %w", err)
	}
	if info.IsDir() {
		return nil, struct{}{}, fmt.Errorf("path is a directory: %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, struct{}{}, fmt.Errorf("reading file: %w", err)
	}
	if !utf8.Valid(data) {
		return nil, struct{}{}, fmt.Errorf("cannot edit %s: file is not utf-8 text", path)
	}
	before := string(data)
	crlf := usesCRLF(before)
	after := before
	replacements := 0
	for i, edit := range edits {
		next, n, err := replaceExact(after, edit, crlf)
		if err != nil {
			if len(edits) > 1 {
				return nil, struct{}{}, fmt.Errorf("edit %d of %d: %w; no edits were applied to %s", i+1, len(edits), err, path)
			}
			return nil, struct{}{}, fmt.Errorf("%w in %s", err, path)
		}
		after = next
		replacements += n
	}
	if err := writeFileAtomic(path, []byte(after), info.Mode().Perm()); err != nil {
		return nil, struct{}{}, err
	}
	fileReads.record(ctx, path)
	return jsonResult(map[string]any{
		"path":         path,
		"replacements": replacements,
		"diff":         unifiedDiff(path, before, after),
	})
}

func replaceExact(content string, edit fileEditOp, crlf bool) (string, int, error) {
	if edit.OldString == "" {
		return "", 0, errors.New("old_string must not be empty; use file_write to create a file")
	}
	oldString, newString := edit.OldString, edit.NewString
	if crlf {
		oldString = toCRLF(oldString)
		newString = toCRLF(newString)
	}
	if oldString == newString {
		return "", 0, errors.New("old_string and new_string are identical")
	}
	count := strings.Count(content, oldString)
	switch {
	case count == 0:
		return "", 0, errors.New("old_string not found")
	case count > 1 && !edit.ReplaceAll:
		return "", 0, fmt.Errorf("old_string appears %d times; include more surrounding context to make it unique, or set replace_all", count)
	}
	return strings.ReplaceAll(content, oldString, newString), count, nil
}

// usesCRLF reports whether every line break in content is CRLF.
func usesCRLF(content string) bool {
	crlf := strings.Count(content, "\r\n")
	return crlf > 0 && crlf == strings.Count(content, "\n")
}

func toCRLF(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\n", "\r\n")
}

// writeFileAtomic replaces path with data through a temporary file in the same
// directory, so a failed write never leaves a partially written file.
func writeFileAtomic(path string, data []byte, perm fs.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating parent directories: %w", err)
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("creating temporary file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) //nolint:errcheck
	if _, err := tmp.Write(data); err != nil {
		tmp.Close() //nolint:errcheck,gosec
		return fmt.Errorf("writing file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("writing file: %w", err)
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return fmt.Errorf("setting file mode: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replacing file: %w", err)
	}
	return nil
}

// unifiedDiff renders a unified diff of a change to path, ignoring CR so CRLF
// files diff by line.
func unifiedDiff(path, before, after string) string {
	before = strings.ReplaceAll(before, "\r\n", "\n")
	after = strings.ReplaceAll(after, "\r\n", "\n")
	edits := myers.ComputeEdits(span.URIFromPath(path), before, after)
	diff := fmt.Sprint(gotextdiff.ToUnified("a/"+filepath.ToSlash(path), "b/"+filepath.ToSlash(path), before, edits))
	if len(diff) > maxDiffBytes {
		cut := strings.LastIndexByte(diff[:maxDiffBytes], '\n') + 1
		diff = diff[:cut] + fmt.Sprintf("... diff truncated (%d bytes total)\n", len(diff))
	}
	return diff
}

// fileReadTracker remembers which files each session has read, and their
// modification time then, so file_write can refuse to clobber a file the
// model has not seen.
type fileReadTracker struct {
	mu    sync.Mutex
	reads map[string]map[string]time.Time
}

var fileReads = &fileReadTracker{reads: map[string]map[string]time.Time{}}

func fileReadSessionKey(ctx context.Context) string {
	if sessionID, ok := agent.SessionIDFromContext(ctx); ok {
		return "session:" + sessionID
	}
	agentID, _ := agent.SessionAgentIDFromContext(ctx)
	return "agent:" + agentID
}

func (t *fileReadTracker) record(ctx context.Context, path string) {
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	key := fileReadSessionKey(ctx)
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.reads[key] == nil {
		t.reads[key] = map[string]time.Time{}
	}
	t.reads[key][path] = info.ModTime()
}

// checkOverwrite returns an error when the write guard applies to ctx and
// path is an existing file the session has not read since it last changed.
func (t *fileReadTracker) checkOverwrite(ctx context.Context, path string) error {
	if !agent.WriteGuardFromContext(ctx) {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil //nolint:nilerr // a missing file is a new file
	}
	t.mu.Lock()
	readAt, ok := t.reads[fileReadSessionKey(ctx)][path]
	t.mu.Unlock()
	if !ok {
		return fmt.Errorf("refusing to overwrite %s: read it with file_read first, or change it with file_edit", path)
	}
	if !info.ModTime().Equal(readAt) {
		return fmt.Errorf("refusing to overwrite %s: it changed since it was last read; read it again first", path)
	}
	return nil
}
