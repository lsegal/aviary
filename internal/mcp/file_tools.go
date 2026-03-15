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
	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "file_read",
		Description: "Read a file within the current agent's filesystem allowlist. Arguments: path (required). Returns utf-8 text when possible, otherwise base64.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args filePathArgs) (*sdkmcp.CallToolResult, struct{}, error) {
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
		content := string(data)
		encoding := "utf-8"
		if !utf8.Valid(data) {
			content = base64.StdEncoding.EncodeToString(data)
			encoding = "base64"
		}
		return jsonResult(map[string]any{
			"path":     path,
			"content":  content,
			"encoding": encoding,
		})
	})

	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "file_write",
		Description: "Create or replace a file within the current agent's filesystem allowlist. Arguments: path, content, encoding(optional utf-8|base64).",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args fileWriteArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		path, _, err := resolveAllowedAgentPath(ctx, args.Path, "write")
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
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return nil, struct{}{}, fmt.Errorf("writing file: %w", err)
		}
		return text(fmt.Sprintf("file written: %s", path))
	})

	sdkmcp.AddTool(s, &sdkmcp.Tool{
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

	sdkmcp.AddTool(s, &sdkmcp.Tool{
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

	sdkmcp.AddTool(s, &sdkmcp.Tool{
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

	sdkmcp.AddTool(s, &sdkmcp.Tool{
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

	sdkmcp.AddTool(s, &sdkmcp.Tool{
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

func resolveAllowedAgentPath(ctx context.Context, rawPath, operation string) (string, *filesystem.Policy, error) {
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
	workspaceDir := store.WorkspaceDir()
	policy, err := filesystem.PolicyFromAgent(runner.Config(), workspaceDir)
	if err != nil {
		return "", nil, err
	}
	resolved, err := filesystem.ResolvePath(rawPath, workspaceDir)
	if err != nil {
		return "", nil, err
	}
	cfg := runner.Config()
	if cfg == nil || cfg.Permissions == nil || cfg.Permissions.Filesystem == nil || len(cfg.Permissions.Filesystem.AllowedPaths) == 0 {
		return "", nil, fmt.Errorf("agent %q has no filesystem allowedPaths configured", runner.Agent().Name)
	}
	if !policy.Allows(resolved) {
		return "", nil, fmt.Errorf("%s path is outside the filesystem allowlist: %s", operation, resolved)
	}
	return resolved, policy, nil
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
