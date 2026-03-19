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
	"github.com/lsegal/aviary/skills"
)

type skillRunArgs struct {
	Command []string `json:"command"`
}

type skillCommandFunc func(context.Context, string, ...string) *exec.Cmd

var (
	gogLookPath      = exec.LookPath
	himalayaLookPath = exec.LookPath
	notionLookPath   = exec.LookPath
	gogCommand       = exec.CommandContext
	himalayaCommand  = exec.CommandContext
	notionCommand    = exec.CommandContext
)

const (
	gogcliToolName   = "skill_gogcli"
	himalayaToolName = "skill_himalaya"
	notionToolName   = "skill_notion"
)

type gogcliRunArgs struct {
	Command []string `json:"command"`
	Account string   `json:"account,omitempty"`
}

type himalayaRunArgs struct {
	Command []string `json:"command"`
}

type notionRunArgs struct {
	Command []string `json:"command"`
}

func registerConfiguredSkillTools(s *sdkmcp.Server) {
	cfg, err := config.Load("")
	if err != nil {
		return
	}
	syncSkillTools(s, cfg)
}

func syncSkillTools(s *sdkmcp.Server, cfg *config.Config) {
	if s == nil {
		return
	}
	list, err := skills.ListInstalled(cfg)
	if err != nil {
		return
	}

	for _, skill := range list {
		s.RemoveTools(skillToolName(skill.Name))
	}
	if cfg != nil && cfg.Skills != nil {
		for name := range cfg.Skills {
			s.RemoveTools(skillToolName(name))
		}
	}

	for _, skill := range list {
		if !skill.Enabled || skill.Runtime == nil {
			continue
		}
		registerSkillRuntimeTool(s, skill)
	}
}

func registerSkillRuntimeTool(s *sdkmcp.Server, skill skills.Definition) {
	tool := &sdkmcp.Tool{
		Name:        skillToolName(skill.Name),
		Description: strings.TrimSpace(skill.Description + " Arguments: command (array of strings, required)."),
		InputSchema: skillToolInputSchema(),
	}
	addTool(s, tool, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args skillRunArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		out, err := runSkillCommand(ctx, skill, args)
		if err != nil {
			return nil, struct{}{}, err
		}
		return text(out)
	})
}

func skillToolName(name string) string {
	return "skill_" + strings.TrimSpace(name)
}

func skillToolInputSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"command"},
		"properties": map[string]any{
			"command": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Command arguments passed to the skill runtime.",
			},
		},
	}
}

func runSkillCommand(ctx context.Context, skill skills.Definition, args skillRunArgs) (string, error) {
	return runSkillCommandArgs(ctx, skill, args.Command)
}

func runSkillCommandArgs(ctx context.Context, skill skills.Definition, rawCommand []string) (string, error) {
	if skill.Runtime == nil {
		return "", fmt.Errorf("skill %q has no runtime metadata", skill.Name)
	}
	if skill.Runtime.Type != "" && skill.Runtime.Type != "command" {
		return "", fmt.Errorf("skill %q uses unsupported runtime type %q", skill.Name, skill.Runtime.Type)
	}

	command := normalizeSkillCommand(skill.Runtime, rawCommand)
	if len(command) == 0 {
		return "", fmt.Errorf("command is required")
	}

	topLevel := firstSkillCommand(skill.Runtime, command)
	if topLevel == "" {
		switch skill.Name {
		case "gogcli":
			return "", fmt.Errorf("a gog service command is required")
		case "notion":
			return "", fmt.Errorf("a notion-cli command is required")
		default:
			return "", fmt.Errorf("a %s command is required", skill.Name)
		}
	}
	if allowed := allowedSkillCommands(skill); len(allowed) > 0 {
		if _, ok := allowed[topLevel]; !ok {
			return "", fmt.Errorf("%s command %q is not allowed", skill.Name, topLevel)
		}
	}

	bin := resolveSkillBinary(skill)
	if strings.TrimSpace(bin) == "" {
		return "", fmt.Errorf("skill %q has no runtime binary configured", skill.Name)
	}
	lookup := skillLookPathFunc(skill.Name)
	commandFunc := skillCommandFuncForName(skill.Name)

	resolved, err := lookup(bin)
	if err == nil {
		bin = resolved
	} else if _, statErr := os.Stat(bin); statErr != nil {
		return "", fmt.Errorf("%s binary %q not found on PATH", skill.Name, bin)
	}

	fullArgs := append(append([]string{}, skill.Runtime.Args...), command...)
	env := map[string]string{}
	for key, value := range skill.Runtime.Env {
		env[key] = value
	}
	if key := strings.TrimSpace(skill.Runtime.EnvFromTopLevel); key != "" {
		env[key] = topLevel
	}

	cmd := commandFunc(ctx, bin, fullArgs...)
	cmd.Env = commandEnv(ctx, env)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		errText := strings.TrimSpace(stderr.String())
		if errText == "" {
			errText = err.Error()
		}
		return "", fmt.Errorf("%s failed: %s", skill.Name, errText)
	}

	out := strings.TrimSpace(stdout.String())
	if out == "" {
		return "", fmt.Errorf("%s returned no output", skill.Name)
	}
	return out, nil
}

func runGogCLI(ctx context.Context, args gogcliRunArgs) (string, error) {
	skill, err := builtinSkillDefinition("gogcli")
	if err != nil {
		return "", err
	}
	command := append([]string{}, args.Command...)
	if account := strings.TrimSpace(args.Account); account != "" {
		command = append([]string{"--account", account}, command...)
	}
	return runSkillCommandArgs(ctx, skill, command)
}

func runHimalayaCLI(ctx context.Context, args himalayaRunArgs) (string, error) {
	skill, err := builtinSkillDefinition("himalaya")
	if err != nil {
		return "", err
	}
	return runSkillCommandArgs(ctx, skill, args.Command)
}

func runNotionCLI(ctx context.Context, args notionRunArgs) (string, error) {
	skill, err := builtinSkillDefinition("notion")
	if err != nil {
		return "", err
	}
	return runSkillCommandArgs(ctx, skill, args.Command)
}

func builtinSkillDefinition(name string) (skills.Definition, error) {
	list, err := skills.ListInstalled(&config.Config{})
	if err != nil {
		return skills.Definition{}, err
	}
	for _, skill := range list {
		if skill.Name == name {
			return skill, nil
		}
	}
	return skills.Definition{}, fmt.Errorf("skill %q not found", name)
}

func skillLookPathFunc(name string) func(string) (string, error) {
	switch name {
	case "gogcli":
		return gogLookPath
	case "himalaya":
		return himalayaLookPath
	case "notion":
		return notionLookPath
	default:
		return exec.LookPath
	}
}

func skillCommandFuncForName(name string) skillCommandFunc {
	switch name {
	case "gogcli":
		return gogCommand
	case "himalaya":
		return himalayaCommand
	case "notion":
		return notionCommand
	default:
		return exec.CommandContext
	}
}

func resolveSkillBinary(skill skills.Definition) string {
	if skill.Runtime == nil {
		return ""
	}
	if cfg, err := config.Load(""); err == nil && cfg != nil && cfg.Skills != nil {
		if skillCfg, ok := cfg.Skills[skill.Name]; ok && skillCfg.Settings != nil {
			if key := strings.TrimSpace(skill.Runtime.BinarySetting); key != "" {
				if value, ok := skillCfg.Settings[key].(string); ok && strings.TrimSpace(value) != "" {
					return strings.TrimSpace(value)
				}
			}
		}
	}
	return strings.TrimSpace(skill.Runtime.Binary)
}

func allowedSkillCommands(skill skills.Definition) map[string]struct{} {
	commands := append([]string{}, skill.Runtime.AllowedCommands...)
	if cfg, err := config.Load(""); err == nil && cfg != nil && cfg.Skills != nil {
		if skillCfg, ok := cfg.Skills[skill.Name]; ok && skillCfg.Settings != nil {
			if key := strings.TrimSpace(skill.Runtime.AllowedCommandsSetting); key != "" {
				if raw, ok := skillCfg.Settings[key].([]any); ok {
					commands = commands[:0]
					for _, value := range raw {
						if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
							commands = append(commands, strings.TrimSpace(text))
						}
					}
				}
			}
		}
	}
	if len(commands) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(commands))
	for _, value := range commands {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		out[value] = struct{}{}
	}
	return out
}

func normalizeSkillCommand(runtime *skills.RuntimeConfiguration, command []string) []string {
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
		if containsString(runtime.StripArgs, part) {
			continue
		}
		if containsString(runtime.StripValueFlags, part) {
			skipNext = true
			continue
		}
		if hasAnyPrefix(part, runtime.StripArgPrefixes) {
			continue
		}
		out = append(out, part)
	}
	return out
}

func firstSkillCommand(runtime *skills.RuntimeConfiguration, args []string) string {
	skipNext := false
	for _, arg := range args {
		if skipNext {
			skipNext = false
			continue
		}
		if arg == "" {
			continue
		}
		if containsString(runtime.TopLevelSkipValueFlags, arg) {
			skipNext = true
			continue
		}
		if hasAnyPrefix(arg, runtime.TopLevelSkipArgPrefixes) {
			continue
		}
		if strings.HasPrefix(arg, "-") {
			continue
		}
		return arg
	}
	return ""
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func hasAnyPrefix(value string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}

func normalizeGogCommand(command []string) []string {
	return normalizeSkillCommand(&skills.RuntimeConfiguration{
		StripArgs: []string{"--json"},
	}, command)
}

func firstNonFlag(args []string) string {
	return firstSkillCommand(&skills.RuntimeConfiguration{}, args)
}
