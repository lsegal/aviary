package cmd

import (
	"fmt"
	"strconv"
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
  aviary config server.port 8080
  aviary config models.defaults.model anthropic/claude-sonnet-4-5
  aviary config scheduler.concurrency 4`,
	Args: cobra.RangeArgs(1, 2),
	RunE: runConfigGetSet,
}

func init() {
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

// setInMap navigates parts into m and sets the leaf to value,
// inferring numeric types where possible.
func setInMap(m map[string]any, parts []string, value string) error {
	p := parts[0]
	if len(parts) == 1 {
		if n, err := strconv.Atoi(value); err == nil {
			m[p] = n
		} else {
			m[p] = value
		}
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
