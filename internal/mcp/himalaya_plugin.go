package mcp

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

var (
	himalayaLookPath = exec.LookPath
	himalayaCommand  = exec.CommandContext
)

const himalayaToolName = "skill_himalaya"

type himalayaRunArgs struct {
	Command []string `json:"command"`
}

var himalayaAllowedCommands = map[string]struct{}{
	"account":    {},
	"folder":     {},
	"envelope":   {},
	"flag":       {},
	"message":    {},
	"attachment": {},
	"template":   {},
}

func registerHimalayaTool(s *sdkmcp.Server) {
	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name: himalayaToolName,
		Description: "Run a Himalaya CLI command for email workflows. " +
			"Arguments: command (array of strings, required). " +
			"Allowed top-level commands are account, folder, envelope, flag, message, attachment, and template. " +
			"JSON output is forced automatically.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args himalayaRunArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		out, err := runHimalayaCLI(ctx, args)
		if err != nil {
			return nil, struct{}{}, err
		}
		return text(out)
	})
}

func runHimalayaCLI(ctx context.Context, args himalayaRunArgs) (string, error) {
	command := normalizeHimalayaCommand(args.Command)
	if len(command) == 0 {
		return "", fmt.Errorf("command is required")
	}

	topLevel := firstHimalayaCommand(command)
	if topLevel == "" {
		return "", fmt.Errorf("a himalaya command is required")
	}
	if _, ok := himalayaAllowedCommands[topLevel]; !ok {
		return "", fmt.Errorf("himalaya command %q is not allowed", topLevel)
	}

	bin, err := resolveHimalayaBinary()
	if err != nil {
		return "", err
	}

	fullArgs := make([]string, 0, len(command)+2)
	fullArgs = append(fullArgs, "--output", "json")
	fullArgs = append(fullArgs, command...)

	cmd := himalayaCommand(ctx, bin, fullArgs...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		errText := strings.TrimSpace(stderr.String())
		if errText == "" {
			errText = err.Error()
		}
		return "", fmt.Errorf("himalaya failed: %s", errText)
	}

	out := strings.TrimSpace(stdout.String())
	if out == "" {
		return "", fmt.Errorf("himalaya returned no output")
	}
	return out, nil
}

func resolveHimalayaBinary() (string, error) {
	if override := strings.TrimSpace(os.Getenv("AVIARY_HIMALAYA_BIN")); override != "" {
		return override, nil
	}
	bin, err := himalayaLookPath("himalaya")
	if err != nil {
		return "", fmt.Errorf("himalaya binary not found in PATH; install himalaya or set AVIARY_HIMALAYA_BIN")
	}
	return bin, nil
}

func normalizeHimalayaCommand(command []string) []string {
	out := make([]string, 0, len(command))
	skipNext := false
	for _, part := range command {
		part = strings.TrimSpace(part)
		if skipNext {
			skipNext = false
			continue
		}
		if part == "" {
			continue
		}
		switch {
		case part == "-o", part == "--output":
			skipNext = true
			continue
		case strings.HasPrefix(part, "--output="), strings.HasPrefix(part, "-o="):
			continue
		default:
			out = append(out, part)
		}
	}
	return out
}

func firstHimalayaCommand(args []string) string {
	globalValueFlags := map[string]struct{}{
		"-c":       {},
		"--config": {},
		"-o":       {},
		"--output": {},
	}

	skipNext := false
	for _, arg := range args {
		if skipNext {
			skipNext = false
			continue
		}
		if arg == "" {
			continue
		}
		if _, ok := globalValueFlags[arg]; ok {
			skipNext = true
			continue
		}
		if strings.HasPrefix(arg, "--config=") || strings.HasPrefix(arg, "-c=") ||
			strings.HasPrefix(arg, "--output=") || strings.HasPrefix(arg, "-o=") {
			continue
		}
		if strings.HasPrefix(arg, "-") {
			continue
		}
		return arg
	}
	return ""
}
