package projects

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/lsegal/aviary/internal/projects/agents"
	"github.com/lsegal/aviary/internal/projects/core"
)

// writeConfig drops a config file into a temp dir and returns its path.
func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), agents.DefaultConfigPath)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

type flagSetUnderTest struct {
	flags    *flag.FlagSet
	specs    *agentFlag
	binaries *agentBinaryFlag
	filter   *filterFlag
}

// TestMissingConfigSettingsAreNotAnError checks that no file, no section, and
// an empty path all leave the defaults alone, matching agents.Load.
func TestMissingConfigSettingsAreNotAnError(t *testing.T) {
	for _, test := range []struct{ name, path string }{
		{"empty path", ""},
		{"missing file", filepath.Join(t.TempDir(), "absent.json")},
		{"no section", writeConfig(t, `{"agents": []}`)},
		{"null section", writeConfig(t, `{"settings": null}`)},
	} {
		t.Run(test.name, func(t *testing.T) {
			settings, err := loadConfigSettings(test.path)
			if err != nil {
				t.Fatalf("load config settings: %v", err)
			}
			if len(settings) != 0 {
				t.Errorf("settings = %v, want none", settings)
			}
		})
	}
}

// TestAgentLoaderIgnoresSettingsSection checks the agent loader tolerates the
// new section beside its own instead of rejecting the file as unknown.
func TestAgentLoaderIgnoresSettingsSection(t *testing.T) {
	path := writeConfig(t, `{"settings": {"concurrency": 4}, "agents": []}`)
	if _, err := agents.Load(path); err != nil {
		t.Fatalf("load agents alongside a settings section: %v", err)
	}
	path = writeConfig(t, `{"nonsense": {}}`)
	if _, err := agents.Load(path); err == nil {
		t.Fatal("expected an unknown section to still be rejected")
	}
}

// TestSaveConfigSettingsCreatesFile checks a run with no config file yet gets
// one when the dashboard saves a setting.
func TestSaveConfigSettingsCreatesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), agents.DefaultConfigPath)
	if err := saveConfigSettings(path, map[string]any{"concurrency": 3}); err != nil {
		t.Fatalf("save config settings: %v", err)
	}
	settings, err := loadConfigSettings(path)
	if err != nil {
		t.Fatalf("load config settings: %v", err)
	}
	if string(settings["concurrency"]) != "3" {
		t.Errorf("concurrency = %s, want 3", settings["concurrency"])
	}
}

// TestSettingsUpdateFlagsMapsDashboardFields checks each live-editable
// setting is stored under the switch that configures the same thing.
func TestSettingsUpdateFlagsMapsDashboardFields(t *testing.T) {
	concurrency, readyState := 4, "Ready"
	commenters := []string{"alice", "bob"}
	active := []string{"claude/opus"}
	noMerge := true
	noChangelog := true
	autoUpdate := false
	values := settingsUpdateFlags(core.SettingsUpdate{
		Concurrency:       &concurrency,
		ReadyState:        &readyState,
		AllowedCommenters: &commenters,
		ActiveAgents:      &active,
		NoMerge:           &noMerge,
		NoChangelog:       &noChangelog,
		AutoUpdate:        &autoUpdate,
	})
	if values["concurrency"] != 4 {
		t.Errorf("concurrency = %v, want 4", values["concurrency"])
	}
	if values["ready-state"] != "Ready" {
		t.Errorf("ready-state = %v, want Ready", values["ready-state"])
	}
	if values["allowed-commenters"] != "alice,bob" {
		t.Errorf("allowed-commenters = %v, want alice,bob", values["allowed-commenters"])
	}
	if got, _ := json.Marshal(values["agent"]); string(got) != `["claude/opus"]` {
		t.Errorf("agent = %s, want [\"claude/opus\"]", got)
	}
	if values["no-merge"] != true {
		t.Errorf("no-merge = %v, want true", values["no-merge"])
	}
	if values["no-changelog"] != true {
		t.Errorf("no-changelog = %v, want true", values["no-changelog"])
	}
	if values["auto-update"] != false {
		t.Errorf("auto-update = %v, want false", values["auto-update"])
	}
	if len(settingsUpdateFlags(core.SettingsUpdate{})) != 0 {
		t.Error("an empty update should write nothing")
	}
}
