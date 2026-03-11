package config

import "strings"

// PermissionsPreset defines the maximum tool surface an agent may use.
type PermissionsPreset string

const (
	// PermissionsPresetFull allows every available tool group.
	PermissionsPresetFull PermissionsPreset = "full"
	// PermissionsPresetStandard blocks higher-risk local and server tools.
	PermissionsPresetStandard PermissionsPreset = "standard"
	// PermissionsPresetMinimal allows only the smallest safe subset of tools.
	PermissionsPresetMinimal PermissionsPreset = "minimal"
)

// EffectivePermissionsPreset returns the runtime preset, defaulting to standard.
func EffectivePermissionsPreset(perms *PermissionsConfig) PermissionsPreset {
	if perms == nil {
		return PermissionsPresetStandard
	}
	switch perms.Preset {
	case PermissionsPresetFull:
		return PermissionsPresetFull
	case PermissionsPresetMinimal:
		return PermissionsPresetMinimal
	case "", PermissionsPresetStandard:
		return PermissionsPresetStandard
	default:
		return PermissionsPresetStandard
	}
}

// IsValidPermissionsPreset reports whether preset is a recognized preset value.
func IsValidPermissionsPreset(preset PermissionsPreset) bool {
	switch preset {
	case "", PermissionsPresetFull, PermissionsPresetStandard, PermissionsPresetMinimal:
		return true
	default:
		return false
	}
}

// ToolGroup returns the logical tool group used by permissions presets and the UI.
func ToolGroup(name string) string {
	switch {
	case name == "ping", strings.HasPrefix(name, "server_"), strings.HasPrefix(name, "config_"):
		return "server"
	case strings.HasPrefix(name, "web_"):
		return "search"
	case name == "skills_list", strings.HasPrefix(name, "skill_"):
		return "skills"
	case name == "exec", strings.HasPrefix(name, "exec_"):
		return "exec"
	case strings.HasPrefix(name, "file_"):
		return "file"
	default:
		group, _, _ := strings.Cut(name, "_")
		if group == "" {
			return name
		}
		return group
	}
}

// IsToolAllowedByPreset reports whether toolName may be enabled for preset.
func IsToolAllowedByPreset(preset PermissionsPreset, toolName string) bool {
	group := ToolGroup(toolName)
	switch preset {
	case PermissionsPresetFull:
		return true
	case PermissionsPresetMinimal:
		switch group {
		case "agent", "auth", "exec", "file", "server", "browser", "skills", "usage":
			return false
		default:
			return true
		}
	case "", PermissionsPresetStandard:
		switch group {
		case "agent", "auth", "exec", "file", "server":
			return false
		default:
			return true
		}
	default:
		return true
	}
}

// ClampToolNamesForPreset filters tool names to those accessible under preset.
func ClampToolNamesForPreset(preset PermissionsPreset, names []string) []string {
	if len(names) == 0 {
		return nil
	}
	out := make([]string, 0, len(names))
	for _, name := range names {
		if IsToolAllowedByPreset(preset, name) {
			out = append(out, name)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
