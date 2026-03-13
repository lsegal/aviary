package mcp

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lsegal/aviary/internal/config"
)

var (
	gogLookPath = exec.LookPath
	gogCommand  = exec.CommandContext
)

const gogcliToolName = "skill_gogcli"

type gogcliRunArgs struct {
	Command []string `json:"command"`
	Account string   `json:"account,omitempty"`
}

var gogAllowedCommands = map[string]struct{}{
	"gmail":     {},
	"calendar":  {},
	"drive":     {},
	"contacts":  {},
	"tasks":     {},
	"sheets":    {},
	"docs":      {},
	"slides":    {},
	"forms":     {},
	"chat":      {},
	"classroom": {},
	"appscript": {},
	"people":    {},
	"groups":    {},
	"admin":     {},
	"keep":      {},
	"time":      {},
}

func registerPluginTools(s *sdkmcp.Server) {
	cfg, err := config.Load("")
	if err != nil || !isSkillEnabled(cfg, "gogcli") {
		return
	}
	registerGogcliTool(s)
}

func syncPluginTools(s *sdkmcp.Server, cfg *config.Config) {
	if s == nil {
		return
	}
	if isSkillEnabled(cfg, "gogcli") {
		registerGogcliTool(s)
		return
	}
	s.RemoveTools(gogcliToolName)
}

func registerGogcliTool(s *sdkmcp.Server) {
	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name: gogcliToolName,
		Description: "Run a gog CLI command for Google Workspace services. " +
			"Arguments: command (array of strings, required) and account (optional). " +
			"Only service commands such as gmail/calendar/drive/contacts/tasks/sheets/docs/slides/forms/chat/classroom/appscript/people/groups/admin/keep/time are allowed. " +
			"JSON output is forced automatically.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args gogcliRunArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		out, err := runGogCLI(ctx, args)
		if err != nil {
			return nil, struct{}{}, err
		}
		return text(out)
	})
}

func isSkillEnabled(cfg *config.Config, name string) bool {
	if cfg == nil || cfg.Skills == nil {
		return false
	}
	skill, ok := cfg.Skills[name]
	return ok && skill.Enabled
}

func runGogCLI(ctx context.Context, args gogcliRunArgs) (string, error) {
	command := normalizeGogCommand(args.Command)
	if len(command) == 0 {
		return "", fmt.Errorf("command is required")
	}

	topLevel := firstNonFlag(command)
	if topLevel == "" {
		return "", fmt.Errorf("a gog service command is required")
	}
	if _, ok := gogAllowedCommands[topLevel]; !ok {
		return "", fmt.Errorf("gog command %q is not allowed", topLevel)
	}

	bin, err := resolveGogBinary()
	if err != nil {
		return "", err
	}

	fullArgs := make([]string, 0, len(command)+4)
	fullArgs = append(fullArgs, "--json")
	if strings.TrimSpace(args.Account) != "" {
		fullArgs = append(fullArgs, "--account", strings.TrimSpace(args.Account))
	}
	fullArgs = append(fullArgs, command...)

	cmd := gogCommand(ctx, bin, fullArgs...)
	cmd.Env = append(os.Environ(), "GOG_ENABLE_COMMANDS="+topLevel)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		errText := strings.TrimSpace(stderr.String())
		if errText == "" {
			errText = err.Error()
		}
		return "", fmt.Errorf("gogcli failed: %s", errText)
	}

	out := strings.TrimSpace(stdout.String())
	if out == "" {
		return "", fmt.Errorf("gogcli returned no output")
	}
	return out, nil
}

func resolveGogBinary() (string, error) {
	if override := strings.TrimSpace(os.Getenv("AVIARY_GOGCLI_BIN")); override != "" {
		return override, nil
	}
	bin, err := gogLookPath("gog")
	if err != nil {
		return "", fmt.Errorf("gog binary not found in PATH; install gogcli or set AVIARY_GOGCLI_BIN")
	}
	return bin, nil
}

func normalizeGogCommand(command []string) []string {
	out := make([]string, 0, len(command))
	for _, part := range command {
		part = strings.TrimSpace(part)
		if part != "" && part != "--json" {
			out = append(out, part)
		}
	}
	return out
}

func firstNonFlag(args []string) string {
	for _, arg := range args {
		if !strings.HasPrefix(arg, "-") {
			return arg
		}
	}
	return ""
}
