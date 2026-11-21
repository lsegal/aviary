package config

import "github.com/lsegal/aviary/internal/clidefs"

// ApprovalConfig is the approval: section of aviary.yaml: how much a CLI
// provider may do without asking, in Aviary's own terms. Every CLI launch
// translates its surface's effective mode into that CLI's native permission
// flags (see clidefs.Definition.ApprovalArgs); no CLI settings file is
// written to set it.
type ApprovalConfig struct {
	// Mode is the general setting, used by every surface without an
	// override of its own. Empty means clidefs.DefaultApprovalMode.
	Mode clidefs.ApprovalMode `yaml:"mode,omitempty" json:"mode,omitempty"`
	// Chat overrides Mode for agent chat turns run through a CLI provider:
	// the web UI chat, including the Code Loop board's prompt chat. Empty
	// follows Mode.
	Chat clidefs.ApprovalMode `yaml:"chat,omitempty" json:"chat,omitempty"`
}

// ChatApprovalMode is the effective approval mode of a CLI-backed chat turn.
func (c *Config) ChatApprovalMode() clidefs.ApprovalMode {
	if c == nil {
		return clidefs.DefaultApprovalMode
	}
	if c.Approval.Chat != "" {
		return c.Approval.Chat.Effective()
	}
	return c.Approval.Mode.Effective()
}

// ProjectsApprovalMode is the effective approval mode of the CLI runs
// Code Loop launches.
func (c *Config) ProjectsApprovalMode() clidefs.ApprovalMode {
	if c == nil {
		return clidefs.DefaultApprovalMode
	}
	return c.Projects.EffectiveApprovalMode(c.Approval.Mode)
}

// EffectiveApprovalMode resolves the Code Loop surface's approval mode against
// the general setting: the section's approval_mode wins, then yolo (which is
// the full mode), then general.
func (p ProjectsConfig) EffectiveApprovalMode(general clidefs.ApprovalMode) clidefs.ApprovalMode {
	switch {
	case p.ApprovalMode != "":
		return p.ApprovalMode.Effective()
	case p.Yolo:
		return clidefs.ApprovalFull
	}
	return general.Effective()
}
