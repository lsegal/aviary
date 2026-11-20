package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/clidefs"
)

// TestApprovalModeResolution checks each surface's override wins for its own
// surface and that a surface without one follows the general setting
// (issue #513).
func TestApprovalModeResolution(t *testing.T) {
	var none *Config
	assert.Equal(t, clidefs.ApprovalAuto, none.ChatApprovalMode())
	assert.Equal(t, clidefs.ApprovalAuto, (&Config{}).ProjectsApprovalMode(), "nothing set means auto")

	general := &Config{Approval: ApprovalConfig{Mode: clidefs.ApprovalReadOnly}}
	assert.Equal(t, clidefs.ApprovalReadOnly, general.ChatApprovalMode())
	assert.Equal(t, clidefs.ApprovalReadOnly, general.ProjectsApprovalMode())

	overridden := &Config{
		Approval: ApprovalConfig{Mode: clidefs.ApprovalReadOnly, Chat: clidefs.ApprovalFull},
		Projects: ProjectsConfig{ApprovalMode: clidefs.ApprovalAuto},
	}
	assert.Equal(t, clidefs.ApprovalFull, overridden.ChatApprovalMode())
	assert.Equal(t, clidefs.ApprovalAuto, overridden.ProjectsApprovalMode())

	// yolo is the Projects override's full mode, unless approval_mode is set.
	yolo := &Config{Approval: ApprovalConfig{Mode: clidefs.ApprovalReadOnly}, Projects: ProjectsConfig{Yolo: true}}
	assert.Equal(t, clidefs.ApprovalFull, yolo.ProjectsApprovalMode())
	assert.Equal(t, clidefs.ApprovalReadOnly, yolo.ChatApprovalMode(), "yolo is Projects-only")
	yolo.Projects.ApprovalMode = clidefs.ApprovalAuto
	assert.Equal(t, clidefs.ApprovalAuto, yolo.ProjectsApprovalMode())
}

func TestApprovalConfigLoadsAndValidates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "aviary.yaml")
	require.NoError(t, os.WriteFile(path, []byte("approval:\n  mode: read-only\n  chat: full\nprojects:\n  approval_mode: auto\n"), 0o600))
	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, ApprovalConfig{Mode: clidefs.ApprovalReadOnly, Chat: clidefs.ApprovalFull}, cfg.Approval)
	assert.Equal(t, clidefs.ApprovalAuto, cfg.Projects.ApprovalMode)
	assert.False(t, hasIssue(Validate(cfg, nil), "must be one of"))

	bad := &Config{Approval: ApprovalConfig{Chat: "yolo"}, Projects: ProjectsConfig{ApprovalMode: "ask"}}
	issues := Validate(bad, nil)
	var fields []string
	for _, iss := range issues {
		fields = append(fields, iss.Field)
	}
	assert.Contains(t, fields, "approval.chat")
	assert.Contains(t, fields, "projects.approval_mode")
}
