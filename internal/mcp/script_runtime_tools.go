package mcp

import (
	"context"
	"fmt"
	"slices"

	"github.com/lsegal/aviary/internal/config"
)

func agentToolPermitted(ctx context.Context, name string) error {
	runner := runnerForAgentContext(ctx)
	if runner == nil {
		return nil
	}
	cfg := runner.Config()
	preset := config.EffectivePermissionsPreset(nil)
	if cfg != nil {
		preset = config.EffectivePermissionsPreset(cfg.Permissions)
	}
	if !config.IsToolAllowedByPreset(preset, name) {
		return fmt.Errorf("tool %q is not enabled for this agent", name)
	}
	if name == "exec" && !agentHasExecConfig(ctx) {
		return fmt.Errorf("tool %q is not enabled for this agent", name)
	}
	if cfg != nil && cfg.Permissions != nil {
		if len(cfg.Permissions.Tools) > 0 {
			allowed := config.ClampToolNamesForPreset(preset, cfg.Permissions.Tools)
			if !slices.Contains(allowed, name) {
				return fmt.Errorf("tool %q is not enabled for this agent", name)
			}
		}
		disabled := config.ClampToolNamesForPreset(preset, cfg.Permissions.DisabledTools)
		if slices.Contains(disabled, name) {
			return fmt.Errorf("tool %q is disabled for this agent", name)
		}
	}
	return nil
}
