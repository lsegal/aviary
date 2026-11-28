package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/lsegal/aviary/internal/config"
)

var configCmd = &cobra.Command{
	Use:   "config <key> [value]",
	Short: "Get or set a configuration value",
	Long: `Get or set individual configuration values using dot-separated keys (git-style).

Get a value:
  aviary config browser.profile_directory
  aviary config server.port

Set a value:
	aviary config browser.profile_directory Aviary
  aviary config browser.binary /usr/bin/chromium
  aviary config browser.cdp_port 9333
  aviary config browser.reuse_tabs false
  aviary config server.port 8080
  aviary config models.defaults.model anthropic/claude-sonnet-5-5
  aviary config scheduler.concurrency 4`,
	Args: cobra.RangeArgs(1, 2),
	RunE: runConfigGetSet,
}

var configConvertTaskToScriptCmd = newAgentToolCmd(agentToolSpec{
	use:   "convert-task-to-script [agent] <task>",
	short: "Precompile a prompt task, keeping its prompt to fall back to",
	long: `Turn precompile on for a prompt task, project task or prompt workflow and
compile a fresh Lua script beside its prompt in the background, discarding any
existing script. The task keeps its type and prompt: runs execute the script
and fall back to the model if it fails. YAML-defined tasks stay in aviary.yaml
and file-based tasks stay in their .md file. The agent may be omitted when only
one agent exists.

Example:
  aviary config convert-task-to-script myagent weather-check`,
	args:     cobra.RangeArgs(1, 2),
	tool:     "config_task_convert_to_script",
	agentKey: "agent",
	restKeys: []string{"task"},
})

var configMoveTaskToFileCmd = newAgentToolCmd(agentToolSpec{
	use:   "move-task-to-file [agent] <task>",
	short: "Move a task from aviary.yaml to a markdown file in the agent's tasks/ directory",
	long: `Move a scheduled task that is currently defined inline in aviary.yaml to a
markdown file inside the agent's tasks/ directory. The task is removed from
aviary.yaml and written as <task-name>.md. The agent may be omitted when only
one agent exists.

Example:
  aviary config move-task-to-file myagent daily-report`,
	args:     cobra.RangeArgs(1, 2),
	tool:     "config_task_move_to_file",
	agentKey: "agent",
	restKeys: []string{"task"},
})

func init() {
	configCmd.AddCommand(configConvertTaskToScriptCmd, configMoveTaskToFileCmd)
	rootCmd.AddCommand(configCmd)
}

func runConfigGetSet(_ *cobra.Command, args []string) error {
	key := args[0]
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return err
	}

	if len(args) == 1 {
		val, err := configGetKey(cfg, key)
		if err != nil {
			return err
		}
		fmt.Println(val)
		return nil
	}

	if err := configSetKey(cfg, key, args[1]); err != nil {
		return err
	}
	return config.Save(cfgFile, cfg)
}

// configGetKey reads a dot-separated key from cfg (using YAML field names).
func configGetKey(cfg *config.Config, key string) (string, error) {
	m, err := configToMap(cfg)
	if err != nil {
		return "", err
	}
	val, err := getInMap(m, strings.Split(key, "."))
	if err != nil {
		return "", fmt.Errorf("config key %q: %w", key, err)
	}
	if val == nil {
		return "", nil
	}
	return fmt.Sprintf("%v", val), nil
}

// configSetKey writes value at the dot-separated key in cfg (using YAML field names).
func configSetKey(cfg *config.Config, key, value string) error {
	m, err := configToMap(cfg)
	if err != nil {
		return err
	}
	if err := setInMap(m, strings.Split(key, "."), value); err != nil {
		return fmt.Errorf("config key %q: %w", key, err)
	}
	return mapToConfig(m, cfg)
}

// configToMap marshals cfg through YAML to produce a map keyed by YAML field names.
func configToMap(cfg *config.Config) (map[string]any, error) {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	return m, yaml.Unmarshal(data, &m)
}

// mapToConfig round-trips a map back into cfg via YAML marshal/unmarshal.
func mapToConfig(m map[string]any, cfg *config.Config) error {
	data, err := yaml.Marshal(m)
	if err != nil {
		return err
	}
	return yaml.Unmarshal(data, cfg)
}

// getInMap navigates parts into m, returning the leaf value.
func getInMap(m map[string]any, parts []string) (any, error) {
	val, ok := m[parts[0]]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	if len(parts) == 1 {
		return val, nil
	}
	sub, ok := val.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("cannot traverse into %T at %q", val, parts[0])
	}
	return getInMap(sub, parts[1:])
}

// setInMap navigates parts into m and sets the leaf to value, decoding the
// scalar using YAML semantics so booleans, numbers, nulls, and collections
// round-trip as typed config values.
func setInMap(m map[string]any, parts []string, value string) error {
	p := parts[0]
	if len(parts) == 1 {
		var decoded any
		if err := yaml.Unmarshal([]byte(value), &decoded); err != nil {
			return fmt.Errorf("invalid config value %q: %w", value, err)
		}
		m[p] = decoded
		return nil
	}
	sub := m[p]
	subMap, ok := sub.(map[string]any)
	if !ok {
		subMap = map[string]any{}
		m[p] = subMap
	}
	return setInMap(subMap, parts[1:], value)
}
