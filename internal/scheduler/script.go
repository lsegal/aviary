package scheduler

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/store"
)

func executeScriptJob(ctx context.Context, job *domain.Job, cfg *config.AgentConfig, deliver func(agentName, route, text string) error) (string, error) {
	script := strings.TrimSpace(job.Script)
	if script == "" {
		return "", fmt.Errorf("script task %q has no script content", job.TaskID)
	}
	interp, interpArgs, ext, err := interpreterFromScript(script)
	if err != nil {
		return "", err
	}
	workdir := store.AgentDir(job.AgentID)
	if cfg != nil && strings.TrimSpace(cfg.WorkingDir) != "" {
		workdir = strings.TrimSpace(cfg.WorkingDir)
	}
	if err := os.MkdirAll(filepath.Join(store.AgentDir(job.AgentID), "jobs"), 0o755); err != nil {
		return "", fmt.Errorf("creating jobs dir: %w", err)
	}
	scriptPath := filepath.Join(store.AgentDir(job.AgentID), "jobs", job.ID+ext)
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		return "", fmt.Errorf("writing script: %w", err)
	}

	args := append(append([]string{}, interpArgs...), scriptPath)
	cmd := exec.CommandContext(ctx, interp, args...) //nolint:gosec
	cmd.Dir = workdir
	cmd.Env = append(os.Environ(), scriptEnv(job)...)
	out, err := cmd.CombinedOutput()
	output := strings.TrimSpace(string(out))
	if output != "" {
		if job.ReplyAgentID != "" && job.ReplySessionID != "" {
			if replyErr := agent.AppendReplyToSession(job.ReplyAgentID, job.ReplySessionID, output); replyErr != nil {
				return output, replyErr
			}
		}
		if agent.ShouldDeliverReply(output) && job.OutputChannel != "" && deliver != nil {
			if deliverErr := deliver(job.AgentID, job.OutputChannel, output); deliverErr != nil {
				return output, deliverErr
			}
		}
	}
	if err != nil {
		return output, fmt.Errorf("running script task: %w", err)
	}
	return output, nil
}

func scriptEnv(job *domain.Job) []string {
	env := []string{
		"AVIARY_JOB_ID=" + job.ID,
		"AVIARY_TASK_ID=" + job.TaskID,
		"AVIARY_AGENT_ID=" + job.AgentID,
		"AVIARY_AGENT_NAME=" + job.AgentID,
	}
	if exe, err := os.Executable(); err == nil && strings.TrimSpace(exe) != "" {
		env = append(env, "AVIARY_BIN="+exe)
	}
	if job.ReplySessionID != "" {
		env = append(env, "AVIARY_SESSION_ID="+job.ReplySessionID)
	}
	return env
}

func interpreterFromScript(script string) (string, []string, string, error) {
	scanner := bufio.NewScanner(strings.NewReader(script))
	if !scanner.Scan() {
		return "", nil, "", fmt.Errorf("script is empty")
	}
	line := strings.TrimSpace(scanner.Text())
	if !strings.HasPrefix(line, "#!") {
		return "", nil, "", fmt.Errorf("script tasks require a shebang (for example #!/usr/bin/env python3)")
	}
	return interpreterFromShebang(strings.TrimSpace(strings.TrimPrefix(line, "#!")))
}

func interpreterFromShebang(raw string) (string, []string, string, error) {
	fields := strings.Fields(strings.TrimSpace(raw))
	if len(fields) == 0 {
		return "", nil, "", fmt.Errorf("invalid shebang")
	}
	command := filepath.Base(fields[0])
	args := fields[1:]
	if command == "env" {
		if len(args) == 0 {
			return "", nil, "", fmt.Errorf("invalid env shebang")
		}
		command = args[0]
		args = args[1:]
	}
	command = strings.TrimSpace(command)
	if runtime.GOOS == "windows" {
		command = strings.TrimSuffix(command, ".exe")
	}
	ext := scriptExt(command)
	return command, args, ext, nil
}

func scriptExt(command string) string {
	switch strings.ToLower(strings.TrimSpace(command)) {
	case "python", "python3", "py":
		return ".py"
	case "sh", "bash":
		return ".sh"
	case "powershell", "pwsh":
		return ".ps1"
	default:
		return ".txt"
	}
}
