package mcp

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/filesystem"
	"github.com/lsegal/aviary/internal/store"
)

type filePathArgs struct {
	Path string `json:"path"`
}

type fileReadArgs struct {
	Path   string `json:"path"`
	Offset int    `json:"offset,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

// File read caps: a file up to maxWholeReadBytes is returned whole by default;
// larger files and ranged reads return at most maxReadLines numbered lines,
// maxReadBytes in total, with each line cut at maxReadLineChars.
const (
	maxWholeReadBytes = 256 * 1024
	maxReadBytes      = 256 * 1024
	maxReadLines      = 2000
	maxReadLineChars  = 2000
)

type fileWriteArgs struct {
	Path     string `json:"path"`
	Content  string `json:"content"`
	Encoding string `json:"encoding,omitempty"`
}

type fileCopyArgs struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
}

type fileTruncateArgs struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

func registerFileTools(s *sdkmcp.Server) {
	addTool(s, &sdkmcp.Tool{
		Name:        "file_read",
		Description: "Read a file within the current agent's filesystem allowlist. Arguments: path (required), offset (optional 1-based first line), limit (optional line count). A small file is returned whole as utf-8 text, or base64 when it is not text. With offset or limit, or when the file is too large to return whole, content holds just those lines, each prefixed with its line number and a tab, plus a note saying how to read the rest.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args fileReadArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		if args.Offset < 0 || args.Limit < 0 {
			return nil, struct{}{}, fmt.Errorf("offset and limit must be >= 0")
		}
		path, _, err := resolveAllowedAgentPath(ctx, args.Path, "read")
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
		fileReads.record(ctx, path)
		ranged := args.Offset > 0 || args.Limit > 0
		if !utf8.Valid(data) {
			if ranged {
				return nil, struct{}{}, fmt.Errorf("offset and limit need a utf-8 text file; %s is binary", path)
			}
			return jsonResult(map[string]any{
				"path":     path,
				"content":  base64.StdEncoding.EncodeToString(data),
				"encoding": "base64",
			})
		}
		if !ranged && len(data) <= maxWholeReadBytes {
			return jsonResult(map[string]any{
				"path":     path,
				"content":  string(data),
				"encoding": "utf-8",
			})
		}
		return jsonResult(readLineRange(path, string(data), args.Offset, args.Limit))
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "file_write",
		Description: "Create a file, or deliberately rewrite one whole, within the current agent's filesystem allowlist. Arguments: path, content, encoding(optional utf-8|base64). An agent must file_read an existing file before overwriting it; prefer file_edit for changes. Returns a unified diff.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args fileWriteArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		path, _, err := resolveAllowedAgentPath(ctx, args.Path, "write")
		if err != nil {
			return nil, struct{}{}, err
		}
		data, err := decodeFileContent(args.Content, args.Encoding)
		if err != nil {
			return nil, struct{}{}, err
		}
		if err := fileReads.checkOverwrite(ctx, path); err != nil {
			return nil, struct{}{}, err
		}
		perm := os.FileMode(0o644)
		var before []byte
		created := true
		if info, statErr := os.Stat(path); statErr == nil {
			if info.IsDir() {
				return nil, struct{}{}, fmt.Errorf("path is a directory: %s", path)
			}
			perm = info.Mode().Perm()
			created = false
			before, _ = os.ReadFile(path)
		}
		if err := writeFileAtomic(path, data, perm); err != nil {
			return nil, struct{}{}, err
		}
		fileReads.record(ctx, path)
		result := map[string]any{
			"path":    path,
			"created": created,
			"bytes":   len(data),
		}
		if utf8.Valid(before) && utf8.Valid(data) {
			result["diff"] = unifiedDiff(path, string(before), string(data))
		}
		return jsonResult(result)
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "file_append",
		Description: "Append data to a file within the current agent's filesystem allowlist. Arguments: path, content, encoding(optional utf-8|base64).",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args fileWriteArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		path, _, err := resolveAllowedAgentPath(ctx, args.Path, "append")
		if err != nil {
			return nil, struct{}{}, err
		}
		data, err := decodeFileContent(args.Content, args.Encoding)
		if err != nil {
			return nil, struct{}{}, err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, struct{}{}, fmt.Errorf("creating parent directories: %w", err)
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return nil, struct{}{}, fmt.Errorf("opening file for append: %w", err)
		}
		defer f.Close() //nolint:errcheck
		if _, err := f.Write(data); err != nil {
			return nil, struct{}{}, fmt.Errorf("appending file: %w", err)
		}
		return text(fmt.Sprintf("file appended: %s", path))
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "file_truncate",
		Description: "Truncate or extend a file within the current agent's filesystem allowlist to a size in bytes. Arguments: path, size.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args fileTruncateArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		if args.Size < 0 {
			return nil, struct{}{}, fmt.Errorf("size must be >= 0")
		}
		path, _, err := resolveAllowedAgentPath(ctx, args.Path, "truncate")
		if err != nil {
			return nil, struct{}{}, err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, struct{}{}, fmt.Errorf("creating parent directories: %w", err)
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return nil, struct{}{}, fmt.Errorf("opening file for truncate: %w", err)
		}
		defer f.Close() //nolint:errcheck
		if err := f.Truncate(args.Size); err != nil {
			return nil, struct{}{}, fmt.Errorf("truncating file: %w", err)
		}
		return text(fmt.Sprintf("file truncated: %s (%d bytes)", path, args.Size))
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "file_delete",
		Description: "Delete a file within the current agent's filesystem allowlist. Arguments: path.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args filePathArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		path, _, err := resolveAllowedAgentPath(ctx, args.Path, "delete")
		if err != nil {
			return nil, struct{}{}, err
		}
		if err := os.Remove(path); err != nil {
			return nil, struct{}{}, fmt.Errorf("deleting file: %w", err)
		}
		return text(fmt.Sprintf("file deleted: %s", path))
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "file_copy",
		Description: "Copy a file within the current agent's filesystem allowlist. Arguments: source, destination.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args fileCopyArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		source, _, err := resolveAllowedAgentPath(ctx, args.Source, "copy source")
		if err != nil {
			return nil, struct{}{}, err
		}
		destination, _, err := resolveAllowedAgentPath(ctx, args.Destination, "copy destination")
		if err != nil {
			return nil, struct{}{}, err
		}
		info, err := os.Stat(source)
		if err != nil {
			return nil, struct{}{}, fmt.Errorf("reading source metadata: %w", err)
		}
		if info.IsDir() {
			return nil, struct{}{}, fmt.Errorf("source is a directory: %s", source)
		}
		data, err := os.ReadFile(source)
		if err != nil {
			return nil, struct{}{}, fmt.Errorf("reading source file: %w", err)
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return nil, struct{}{}, fmt.Errorf("creating destination directories: %w", err)
		}
		if err := os.WriteFile(destination, data, info.Mode().Perm()); err != nil {
			return nil, struct{}{}, fmt.Errorf("writing destination file: %w", err)
		}
		return text(fmt.Sprintf("file copied: %s -> %s", source, destination))
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "file_move",
		Description: "Move or rename a file within the current agent's filesystem allowlist. Arguments: source, destination.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args fileCopyArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		source, _, err := resolveAllowedAgentPath(ctx, args.Source, "move source")
		if err != nil {
			return nil, struct{}{}, err
		}
		destination, _, err := resolveAllowedAgentPath(ctx, args.Destination, "move destination")
		if err != nil {
			return nil, struct{}{}, err
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return nil, struct{}{}, fmt.Errorf("creating destination directories: %w", err)
		}
		if err := os.Rename(source, destination); err != nil {
			return nil, struct{}{}, fmt.Errorf("moving file: %w", err)
		}
		return text(fmt.Sprintf("file moved: %s -> %s", source, destination))
	})
}

// agentPathAccess is the set of paths the current agent's file tools may
// touch: its static filesystem allowlist plus any per-job granted directory.
type agentPathAccess struct {
	static *filesystem.Policy
	grant  *filesystem.Policy
}

// Allows reports whether a resolved path is accessible.
func (a *agentPathAccess) Allows(resolved string) bool {
	return (a.grant != nil && a.grant.Allows(resolved)) || (a.static != nil && a.static.Allows(resolved))
}

// MayAllowWithin reports whether dir or anything beneath it is accessible.
func (a *agentPathAccess) MayAllowWithin(resolvedDir string) bool {
	return a.grant.MayAllowWithin(resolvedDir) || a.static.MayAllowWithin(resolvedDir)
}

func resolveAllowedAgentPath(ctx context.Context, rawPath, operation string) (string, *agentPathAccess, error) {
	resolved, access, err := resolveAgentPath(ctx, rawPath, operation)
	if err != nil {
		return "", nil, err
	}
	if !access.Allows(resolved) {
		if access.static == nil {
			return "", nil, fmt.Errorf("%s path is outside the job's granted directory: %s", operation, resolved)
		}
		_, rule := access.static.Decide(resolved)
		return "", nil, fmt.Errorf("%s refused by Aviary: the agent's Filesystem Allowed Paths %s: %s", operation, refusingRule(rule), resolved)
	}
	return resolved, access, nil
}

// resolveAgentPath resolves rawPath for the current agent and returns the
// access rules to check it against, without checking it.
func resolveAgentPath(ctx context.Context, rawPath, operation string) (string, *agentPathAccess, error) {
	if strings.TrimSpace(rawPath) == "" {
		return "", nil, fmt.Errorf("path is required")
	}
	deps := GetDeps()
	if deps.Agents == nil {
		return "", nil, fmt.Errorf("agent manager not initialized; is the server running?")
	}
	agentID, ok := agent.SessionAgentIDFromContext(ctx)
	if !ok {
		return "", nil, fmt.Errorf("file tools require an agent session context")
	}
	runner, ok := deps.Agents.GetByID(agentID)
	if !ok || runner == nil {
		return "", nil, fmt.Errorf("agent %q not found", agentID)
	}
	if strings.HasPrefix(strings.TrimSpace(rawPath), "@") {
		return "", nil, fmt.Errorf("%s path is not allowed: agents cannot access the Aviary config directory", operation)
	}
	cfg := runner.Config()
	workspaceDir := store.AgentDir(agentID)
	if cfg != nil && cfg.WorkingDir != "" {
		workspaceDir = cfg.WorkingDir
	}
	access := &agentPathAccess{}
	baseDir := workspaceDir
	if grantRoot, ok := agent.FilesystemGrantFromContext(ctx); ok {
		grant, err := filesystem.NewPolicy([]string{grantRoot, filepath.Join(grantRoot, "**")}, grantRoot)
		if err != nil {
			return "", nil, err
		}
		access.grant = grant
		baseDir = grantRoot
	}
	if cfg != nil && cfg.Permissions != nil && cfg.Permissions.Filesystem != nil && len(cfg.Permissions.Filesystem.AllowedPaths) > 0 {
		static, err := filesystem.PolicyFromAgent(cfg, workspaceDir)
		if err != nil {
			return "", nil, err
		}
		access.static = static
	} else if access.grant == nil {
		return "", nil, fmt.Errorf("agent %q has no filesystem allowedPaths configured", runner.Agent().Name)
	}
	resolved, err := filesystem.ResolvePath(rawPath, baseDir)
	if err != nil {
		return "", nil, err
	}
	return resolved, access, nil
}

// readLineRange returns lines offset..offset+limit-1 (1-based) of content,
// numbered, within the read caps.
func readLineRange(path, content string, offset, limit int) map[string]any {
	lines := strings.SplitAfter(content, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	total := len(lines)
	if offset < 1 {
		offset = 1
	}
	if limit <= 0 || limit > maxReadLines {
		limit = maxReadLines
	}
	result := map[string]any{
		"path":        path,
		"encoding":    "utf-8",
		"total_lines": total,
	}
	if offset > total {
		result["content"] = ""
		result["note"] = fmt.Sprintf("offset %d is past the end of the file (%d lines)", offset, total)
		return result
	}
	var b strings.Builder
	end := offset - 1
	for end < total && end < offset-1+limit {
		line := strings.TrimRight(lines[end], "\r\n")
		if utf8.RuneCountInString(line) > maxReadLineChars {
			line = string([]rune(line)[:maxReadLineChars]) + " ... [line truncated]"
		}
		entry := fmt.Sprintf("%6d\t%s\n", end+1, line)
		if b.Len()+len(entry) > maxReadBytes && end > offset-1 {
			break
		}
		b.WriteString(entry)
		end++
	}
	result["content"] = b.String()
	result["start_line"] = offset
	result["end_line"] = end
	if end < total {
		result["truncated"] = true
		result["note"] = fmt.Sprintf("showing lines %d-%d of %d; call file_read with offset %d to read more", offset, end, total, end+1)
	}
	return result
}

func decodeFileContent(content, encoding string) ([]byte, error) {
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "", "utf-8", "utf8", "text":
		return []byte(content), nil
	case "base64":
		data, err := base64.StdEncoding.DecodeString(content)
		if err != nil {
			return nil, fmt.Errorf("decoding base64 content: %w", err)
		}
		return data, nil
	default:
		return nil, fmt.Errorf("unsupported encoding %q", encoding)
	}
}
