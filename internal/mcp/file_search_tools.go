package mcp

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lsegal/aviary/internal/filesystem"
)

// Search result caps.
const (
	maxGlobResults   = 1000
	maxGrepResults   = 500
	maxGrepFileBytes = 10 * 1024 * 1024
	maxGrepContext   = 20
)

var errSearchLimit = errors.New("search result limit reached")

type fileGlobArgs struct {
	Pattern        string `json:"pattern"`
	Path           string `json:"path,omitempty"`
	IncludeIgnored bool   `json:"include_ignored,omitempty"`
}

type fileGrepArgs struct {
	Pattern         string `json:"pattern"`
	Path            string `json:"path,omitempty"`
	Glob            string `json:"glob,omitempty"`
	Mode            string `json:"mode,omitempty"`
	Context         int    `json:"context,omitempty"`
	CaseInsensitive bool   `json:"case_insensitive,omitempty"`
	IncludeIgnored  bool   `json:"include_ignored,omitempty"`
	Limit           int    `json:"limit,omitempty"`
}

type grepMatch struct {
	File    string   `json:"file"`
	Line    int      `json:"line"`
	Text    string   `json:"text"`
	Before  []string `json:"before,omitempty"`
	After   []string `json:"after,omitempty"`
	Matches int      `json:"-"`
}

type grepCount struct {
	File  string `json:"file"`
	Count int    `json:"count"`
}

func registerFileSearchTools(s *sdkmcp.Server) {
	addTool(s, &sdkmcp.Tool{
		Name:        "file_glob",
		Description: "List files matching a glob pattern under a directory within the current agent's filesystem allowlist. Arguments: pattern (required, e.g. **/*.go; a pattern without a slash matches file names at any depth), path (optional directory, default the working directory), include_ignored (optional; .gitignore is respected by default). Returns paths relative to the directory.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args fileGlobArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		if strings.TrimSpace(args.Pattern) == "" {
			return nil, struct{}{}, fmt.Errorf("pattern is required")
		}
		root, access, err := resolveSearchRoot(ctx, args.Path, "glob")
		if err != nil {
			return nil, struct{}{}, err
		}
		files := []string{}
		truncated := false
		err = filesystem.WalkFiles(root, !args.IncludeIgnored, func(rel string, _ fs.DirEntry) error {
			if !filesystem.MatchGlob(args.Pattern, rel) || !access.Allows(filepath.Join(root, filepath.FromSlash(rel))) {
				return nil
			}
			if len(files) == maxGlobResults {
				truncated = true
				return errSearchLimit
			}
			files = append(files, rel)
			return nil
		})
		if err != nil && !errors.Is(err, errSearchLimit) {
			return nil, struct{}{}, fmt.Errorf("walking %s: %w", root, err)
		}
		return jsonResult(map[string]any{
			"root":      root,
			"files":     files,
			"truncated": truncated,
		})
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "file_grep",
		Description: "Search file contents with a regular expression (Go RE2 syntax) within the current agent's filesystem allowlist. Arguments: pattern (required), path (optional file or directory, default the working directory), glob (optional file filter such as *.go or src/**/*.ts), mode (optional: content (default) returns matching lines, files returns file names only, count returns match counts per file), context (optional lines of context around each match, content mode only), case_insensitive (optional), include_ignored (optional; .gitignore is respected by default), limit (optional result cap, default 500).",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args fileGrepArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		return runFileGrep(ctx, args)
	})
}

// resolveSearchRoot resolves a search directory or file. The root itself only
// has to resolve; each file found is checked against the access rules.
func resolveSearchRoot(ctx context.Context, rawPath, operation string) (string, *agentPathAccess, error) {
	if strings.TrimSpace(rawPath) == "" {
		rawPath = "."
	}
	root, access, err := resolveAgentPath(ctx, rawPath, operation)
	if err != nil {
		return "", nil, err
	}
	if !access.Allows(root) && !access.MayAllowWithin(root) {
		return "", nil, fmt.Errorf("%s path is outside the filesystem allowlist: %s", operation, root)
	}
	if _, err := os.Stat(root); err != nil {
		return "", nil, fmt.Errorf("reading %s path: %w", operation, err)
	}
	return root, access, nil
}

func runFileGrep(ctx context.Context, args fileGrepArgs) (*sdkmcp.CallToolResult, struct{}, error) {
	if args.Pattern == "" {
		return nil, struct{}{}, fmt.Errorf("pattern is required")
	}
	expr := args.Pattern
	if args.CaseInsensitive {
		expr = "(?i)" + expr
	}
	re, err := regexp.Compile(expr)
	if err != nil {
		return nil, struct{}{}, fmt.Errorf("invalid pattern: %w", err)
	}
	mode := strings.ToLower(strings.TrimSpace(args.Mode))
	switch mode {
	case "":
		mode = "content"
	case "content", "files", "count":
	default:
		return nil, struct{}{}, fmt.Errorf("unsupported mode %q (use content, files, or count)", args.Mode)
	}
	contextLines := min(max(args.Context, 0), maxGrepContext)
	limit := args.Limit
	if limit <= 0 || limit > maxGrepResults {
		limit = maxGrepResults
	}
	root, access, err := resolveSearchRoot(ctx, args.Path, "grep")
	if err != nil {
		return nil, struct{}{}, err
	}

	matches := []grepMatch{}
	files := []string{}
	counts := []grepCount{}
	results := 0
	truncated := false
	searchFile := func(path, rel string) error {
		if !access.Allows(path) {
			return nil
		}
		fileMatches, err := grepFile(path, rel, re, contextLines)
		if err != nil || len(fileMatches) == 0 {
			return nil //nolint:nilerr // unreadable or binary files are skipped
		}
		if mode == "content" {
			for _, m := range fileMatches {
				if results == limit {
					truncated = true
					return errSearchLimit
				}
				matches = append(matches, m)
				results++
			}
			return nil
		}
		if results == limit {
			truncated = true
			return errSearchLimit
		}
		results++
		if mode == "files" {
			files = append(files, rel)
			return nil
		}
		total := 0
		for _, m := range fileMatches {
			total += m.Matches
		}
		counts = append(counts, grepCount{File: rel, Count: total})
		return nil
	}

	info, err := os.Stat(root)
	if err != nil {
		return nil, struct{}{}, err
	}
	if info.IsDir() {
		err = filesystem.WalkFiles(root, !args.IncludeIgnored, func(rel string, _ fs.DirEntry) error {
			if args.Glob != "" && !filesystem.MatchGlob(args.Glob, rel) {
				return nil
			}
			return searchFile(filepath.Join(root, filepath.FromSlash(rel)), rel)
		})
	} else {
		err = searchFile(root, filepath.Base(root))
	}
	if err != nil && !errors.Is(err, errSearchLimit) {
		return nil, struct{}{}, fmt.Errorf("searching %s: %w", root, err)
	}

	result := map[string]any{"root": root, "mode": mode, "truncated": truncated}
	switch mode {
	case "files":
		result["files"] = files
	case "count":
		result["counts"] = counts
	default:
		result["matches"] = matches
	}
	return jsonResult(result)
}

// grepFile returns the matching lines of a text file. Binary and oversized
// files yield no matches.
func grepFile(path, rel string, re *regexp.Regexp, contextLines int) ([]grepMatch, error) {
	info, err := os.Stat(path)
	if err != nil || info.Size() > maxGrepFileBytes {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
		return nil, nil
	}
	var lines []string
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), maxGrepFileBytes)
	for scanner.Scan() {
		lines = append(lines, strings.TrimRight(scanner.Text(), "\r"))
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	var out []grepMatch
	for i, line := range lines {
		found := re.FindAllStringIndex(line, -1)
		if len(found) == 0 {
			continue
		}
		m := grepMatch{File: rel, Line: i + 1, Text: line, Matches: len(found)}
		if contextLines > 0 {
			m.Before = append([]string{}, lines[max(0, i-contextLines):i]...)
			m.After = append([]string{}, lines[i+1:min(len(lines), i+1+contextLines)]...)
		}
		out = append(out, m)
	}
	return out, nil
}
