package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTranscriptionConfigLoadsAndValidates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "aviary.yaml")
	require.NoError(t, os.WriteFile(path, []byte("transcription:\n  enabled: false\n  engine: local\n  model: small.en\n  language: en\n"), 0o600))
	cfg, err := Load(path)
	require.NoError(t, err)
	require.NotNil(t, cfg.Transcription.Enabled)
	assert.False(t, *cfg.Transcription.Enabled)
	assert.Equal(t, "small.en", cfg.Transcription.Model)
	assert.Equal(t, "en", cfg.Transcription.Language)
	assert.False(t, hasIssue(Validate(cfg, nil), "transcription"))

	bad := &Config{Transcription: TranscriptionConfig{Engine: "openai", Language: "english"}}
	issues := Validate(bad, nil)
	assert.True(t, hasIssue(issues, "only \"local\" is supported"))
	assert.True(t, hasIssue(issues, "ISO 639-1"))
}
