package projects

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/lsegal/aviary/internal/projects/agents"
	"github.com/lsegal/aviary/internal/projects/core"
)

// configSettingsSection is the section of .glorp.config.json that carries
// default values for `glorp watch` switches (issue #614). Every flag the
// command takes can be given here under its own name, so a run's usual
// configuration lives in the file rather than in a shell alias.
const configSettingsSection = agents.SettingsSection

// unconfigurableFlags names the switches the settings section may not set,
// with the reason reported when one is found. --config chooses the file
// being read, so honouring it from inside that file would be circular.
var unconfigurableFlags = map[string]string{
	"config": "it selects this file",
}

// loadConfigSettings reads the settings section of the config file at path.
// A missing file, a missing section, and an empty path are all "no defaults"
// rather than errors, matching how agents.Load treats an absent file.
func loadConfigSettings(path string) (map[string]json.RawMessage, error) {
	if path == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil, fmt.Errorf("config %s: %w", path, err)
	}
	section, ok := top[configSettingsSection]
	if !ok || len(section) == 0 || strings.TrimSpace(string(section)) == "null" {
		return nil, nil
	}
	var settings map[string]json.RawMessage
	if err := json.Unmarshal(section, &settings); err != nil {
		return nil, fmt.Errorf("config %s: section %q must be an object keyed by switch name: %w", path, configSettingsSection, err)
	}
	return settings, nil
}

// applyConfigSettings fills in flags the command line did not set from the
// config file's settings section. A switch given on the command line always
// wins, so the file supplies defaults rather than overrides. It must be
// called after flags.Parse, since that is what records which flags were set.
func applyConfigSettings(flags *flag.FlagSet, path string, settings map[string]json.RawMessage) error {
	if len(settings) == 0 {
		return nil
	}
	explicit := map[string]bool{}
	flags.Visit(func(f *flag.Flag) { explicit[f.Name] = true })
	names := make([]string, 0, len(settings))
	for name := range settings {
		names = append(names, name)
	}
	// Sorted so a file with more than one bad key is always reported by the
	// same one rather than by whichever the map happened to yield first.
	sort.Strings(names)
	for _, name := range names {
		// A switch the file names with a leading dash is the same switch;
		// accepting both spellings avoids a confusing "unknown switch -x".
		key := strings.TrimLeft(name, "-")
		if reason, blocked := unconfigurableFlags[key]; blocked {
			return fmt.Errorf("config %s: section %q: %q cannot be set from this file because %s", path, configSettingsSection, name, reason)
		}
		if flags.Lookup(key) == nil {
			return fmt.Errorf("config %s: section %q: unknown switch %q", path, configSettingsSection, name)
		}
		if explicit[key] {
			continue
		}
		values, err := configSettingValues(settings[name])
		if err != nil {
			return fmt.Errorf("config %s: section %q: %q: %w", path, configSettingsSection, name, err)
		}
		for _, value := range values {
			if err := flags.Set(key, value); err != nil {
				return fmt.Errorf("config %s: section %q: %q: %w", path, configSettingsSection, name, err)
			}
		}
	}
	return nil
}

// configSettingValues renders one JSON value into the strings flag.Set takes.
// An array is a repeatable switch such as --agent or --filter, applied in the
// order it is written; anything else is a single value.
func configSettingValues(raw json.RawMessage) ([]string, error) {
	trimmed := strings.TrimSpace(string(raw))
	if strings.HasPrefix(trimmed, "[") {
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, err
		}
		values := make([]string, 0, len(items))
		for _, item := range items {
			value, err := configSettingScalar(item)
			if err != nil {
				return nil, err
			}
			values = append(values, value)
		}
		return values, nil
	}
	value, err := configSettingScalar(raw)
	if err != nil {
		return nil, err
	}
	return []string{value}, nil
}

func configSettingScalar(raw json.RawMessage) (string, error) {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", err
	}
	switch typed := value.(type) {
	case string:
		return typed, nil
	case bool:
		return strconv.FormatBool(typed), nil
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64), nil
	default:
		return "", fmt.Errorf("must be a string, number, boolean, or an array of those, not %s", strings.TrimSpace(string(raw)))
	}
}

// settingsUpdateFlags renders a dashboard settings change into the switch
// names the settings section stores it under, so what the modal writes is
// read back as a default on the next start (issue #614).
func settingsUpdateFlags(update core.SettingsUpdate) map[string]any {
	values := map[string]any{}
	if update.Concurrency != nil {
		values["concurrency"] = *update.Concurrency
	}
	if update.ReadyState != nil {
		values["ready-state"] = *update.ReadyState
	}
	if update.AllowedCommenters != nil {
		values["allowed-commenters"] = strings.Join(*update.AllowedCommenters, ",")
	}
	if update.NoMerge != nil {
		values["no-merge"] = *update.NoMerge
	}
	if update.NoChangelog != nil {
		values["no-changelog"] = *update.NoChangelog
	}
	if update.AutoUpdate != nil {
		values["auto-update"] = *update.AutoUpdate
	}
	if update.TempDirs != nil {
		dirs, _ := normalizeTempDirs(*update.TempDirs)
		values["temp-dir"] = append([]string{}, dirs...)
	}
	if update.ActiveAgents != nil {
		values["agent"] = append([]string(nil), *update.ActiveAgents...)
	}
	if update.UnblockInterval != nil {
		values["unblock-interval"] = strings.TrimSpace(*update.UnblockInterval)
	}
	if update.UnblockAgent != nil {
		values["unblock-agent"] = strings.TrimSpace(*update.UnblockAgent)
	}
	if update.UnblockModel != nil {
		values["unblock-model"] = strings.TrimSpace(*update.UnblockModel)
	}
	if update.PromptAgent != nil {
		values["prompt-agent"] = strings.TrimSpace(*update.PromptAgent)
	}
	if update.PromptModel != nil {
		values["prompt-model"] = strings.TrimSpace(*update.PromptModel)
	}
	return values
}

// saveConfigSettings merges values into the settings section of the config
// file at path, leaving every other section -- and every switch it does not
// mention -- exactly as written. The file is rewritten through a temporary
// file in the same directory so an interrupted write cannot leave a
// half-written config behind.
func saveConfigSettings(path string, values map[string]any) error {
	if path == "" || len(values) == 0 {
		return nil
	}
	top := map[string]json.RawMessage{}
	raw, err := os.ReadFile(path)
	switch {
	case err == nil:
		if len(strings.TrimSpace(string(raw))) > 0 {
			if err := json.Unmarshal(raw, &top); err != nil {
				return fmt.Errorf("config %s: %w", path, err)
			}
		}
	case errors.Is(err, fs.ErrNotExist):
	default:
		return fmt.Errorf("read config %s: %w", path, err)
	}
	settings := map[string]json.RawMessage{}
	if section, ok := top[configSettingsSection]; ok && strings.TrimSpace(string(section)) != "null" {
		if err := json.Unmarshal(section, &settings); err != nil {
			return fmt.Errorf("config %s: section %q must be an object keyed by switch name: %w", path, configSettingsSection, err)
		}
	}
	for name, value := range values {
		encoded, err := json.Marshal(value)
		if err != nil {
			return fmt.Errorf("config %s: section %q: %q: %w", path, configSettingsSection, name, err)
		}
		settings[name] = encoded
	}
	encodedSettings, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	top[configSettingsSection] = encodedSettings
	out, err := json.MarshalIndent(top, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')
	dir := filepath.Dir(path)
	temp, err := os.CreateTemp(dir, ".glorp.config.*.json")
	if err != nil {
		return fmt.Errorf("write config %s: %w", path, err)
	}
	name := temp.Name()
	defer os.Remove(name)
	if _, err := temp.Write(out); err != nil {
		temp.Close()
		return fmt.Errorf("write config %s: %w", path, err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("write config %s: %w", path, err)
	}
	if err := os.Rename(name, path); err != nil {
		return fmt.Errorf("write config %s: %w", path, err)
	}
	return nil
}
