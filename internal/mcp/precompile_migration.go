package mcp

import (
	"log/slog"
	"strings"

	"github.com/lsegal/aviary/internal/config"
)

// The notes older versions left on a task when they set its failing
// compiled script aside, or turned precompile off after repeated failures.
const (
	autoDisabledPrecompileNote = "Precompile was turned off after the compiled script failed"
	setAsideScriptNote         = "The compiled script failed on its first run"
)

// MigrateAutoDisabledPrecompile restores the tasks older versions changed on
// their own after compiled-script failures (#1568): it turns precompile back
// on for a task they turned it off for, and recompiles a task whose script
// they set aside, clearing the note either way. A task the user turned
// precompile off for carries no such note and is left alone. It returns the
// "<agent>/<task>" names it restored.
func MigrateAutoDisabledPrecompile() ([]string, error) {
	cfg, err := config.Load("")
	if err != nil {
		return nil, err
	}
	var restored []string
	for _, agentCfg := range cfg.Agents {
		for _, task := range agentCfg.Tasks {
			note := strings.TrimSpace(task.PrecompileNote)
			if note == "" {
				continue
			}
			switch {
			case strings.HasPrefix(note, autoDisabledPrecompileNote):
				on := true
				task.Precompile = &on
			case strings.HasPrefix(note, setAsideScriptNote):
			default:
				// An unknown note: drop it without touching the setting.
				task.PrecompileNote = ""
				if err := saveConfiguredTask(agentCfg, task); err != nil {
					return restored, err
				}
				continue
			}
			// The script was discarded, so compile a fresh one.
			task.Compiled, task.CompiledKey = "", ""
			task.ScriptVerified, task.ScriptFailures, task.ScriptLastError, task.PrecompileNote = false, 0, "", ""
			if err := saveConfiguredTask(agentCfg, task); err != nil {
				return restored, err
			}
			restored = append(restored, agentCfg.Name+"/"+task.Name)
			slog.Info("task_compile: restored precompile on a task an older version changed after script failures",
				"component", "task_compile", "agent", agentCfg.Name, "task", task.Name)
		}
	}
	return restored, nil
}
