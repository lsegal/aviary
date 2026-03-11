package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"

	"github.com/mattn/go-shellwords"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/commandpolicy"
	"github.com/lsegal/aviary/internal/config"
)

type execArgs struct {
	Command string `json:"command"`
	Cwd     string `json:"cwd,omitempty"`
}

type execResult struct {
	Command      string   `json:"command"`
	Argv         []string `json:"argv,omitempty"`
	Cwd          string   `json:"cwd,omitempty"`
	Stdout       string   `json:"stdout,omitempty"`
	Stderr       string   `json:"stderr,omitempty"`
	ExitCode     int      `json:"exit_code"`
	Shell        string   `json:"shell,omitempty"`
	Interpolated bool     `json:"shell_interpolate"`
}

var execCommandContext = exec.CommandContext

func registerExecTools(s *sdkmcp.Server) {
	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "exec",
		Description: "Execute a host OS command for the current agent when permissions.exec.allowedCommands allows it. Arguments: command (required), cwd (optional).",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args execArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		perms, _, err := resolveAllowedAgentExec(ctx, args.Command)
		if err != nil {
			return nil, struct{}{}, err
		}
		res, runErr := runExecCommand(ctx, perms, args)
		if runErr == nil {
			return jsonResult(res)
		}

		data := mustMarshalJSON(res)
		return &sdkmcp.CallToolResult{
			IsError: true,
			Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: string(data)}},
		}, struct{}{}, nil
	})
}

func resolveAllowedAgentExec(ctx context.Context, command string) (*config.ExecPermissionsConfig, *agent.AgentRunner, error) {
	if strings.TrimSpace(command) == "" {
		return nil, nil, fmt.Errorf("command is required")
	}
	deps := GetDeps()
	if deps == nil || deps.Agents == nil {
		return nil, nil, fmt.Errorf("agent manager not initialized; is the server running?")
	}
	agentID, ok := agent.SessionAgentIDFromContext(ctx)
	if !ok {
		return nil, nil, fmt.Errorf("exec requires an agent session context")
	}
	runner, ok := deps.Agents.GetByID(agentID)
	if !ok || runner == nil {
		return nil, nil, fmt.Errorf("agent %q not found", agentID)
	}
	cfg := runner.Config()
	if cfg == nil || cfg.Permissions == nil || cfg.Permissions.Exec == nil || len(cfg.Permissions.Exec.AllowedCommands) == 0 {
		return nil, nil, fmt.Errorf("agent %q has no exec allowedCommands configured", runner.Agent().Name)
	}
	policy, err := commandpolicy.New(cfg.Permissions.Exec.AllowedCommands)
	if err != nil {
		return nil, nil, err
	}
	if !policy.Allows(command) {
		return nil, nil, fmt.Errorf("command is outside the exec allowlist: %s", strings.TrimSpace(command))
	}
	return cfg.Permissions.Exec, runner, nil
}

func runExecCommand(ctx context.Context, perms *config.ExecPermissionsConfig, args execArgs) (execResult, error) {
	result := execResult{
		Command:      strings.TrimSpace(args.Command),
		Cwd:          strings.TrimSpace(args.Cwd),
		Interpolated: perms != nil && perms.ShellInterpolate,
		ExitCode:     0,
	}

	var cmd *exec.Cmd
	if perms != nil && perms.ShellInterpolate {
		shell, shellArgs, err := shellInvocation(perms.Shell, result.Command)
		if err != nil {
			return result, err
		}
		result.Shell = shell
		cmd = execCommandContext(ctx, shell, shellArgs...) //nolint:gosec
	} else {
		argv, err := parseExecCommand(result.Command)
		if err != nil {
			return result, err
		}
		result.Argv = append([]string{}, argv...)
		cmd = execCommandContext(ctx, argv[0], argv[1:]...) //nolint:gosec
	}

	if result.Cwd != "" {
		cmd.Dir = result.Cwd
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	result.Stdout = stdout.String()
	result.Stderr = stderr.String()

	if err == nil {
		return result, nil
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		result.ExitCode = exitErr.ExitCode()
		return result, err
	}
	return result, err
}

func parseExecCommand(command string) ([]string, error) {
	parser := shellwords.NewParser()
	parser.ParseEnv = false
	parser.ParseBacktick = false
	argv, err := parser.Parse(command)
	if err != nil {
		return nil, fmt.Errorf("parsing command: %w", err)
	}
	if len(argv) == 0 {
		return nil, fmt.Errorf("command is required")
	}
	return argv, nil
}

func shellInvocation(shell, command string) (string, []string, error) {
	name := strings.TrimSpace(shell)
	if name == "" {
		if runtime.GOOS == "windows" {
			name = "cmd"
		} else {
			name = "sh"
		}
	}

	switch strings.ToLower(name) {
	case "cmd", "cmd.exe":
		return name, []string{"/C", command}, nil
	case "powershell", "powershell.exe", "pwsh", "pwsh.exe":
		return name, []string{"-Command", command}, nil
	case "fish":
		return name, []string{"-c", command}, nil
	default:
		return name, []string{"-lc", command}, nil
	}
}

func mustMarshalJSON(v any) []byte {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return []byte(`{"error":"failed to marshal exec result"}`)
	}
	return data
}
