package store

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPreferences_RoundTrip(t *testing.T) {
	SetDataDir(t.TempDir())
	t.Cleanup(func() { SetDataDir("") })

	prefs, err := ReadPreferences()
	require.NoError(t, err)
	assert.Empty(t, prefs.Navigation.Order)
	assert.Empty(t, prefs.Navigation.Hidden)

	_, err = UpdatePreferences(func(p *Preferences) {
		p.Navigation.Order["agentPages"] = []string{"tasks", "chat"}
		p.Navigation.Hidden["systemPages"] = []string{"logs"}
	})
	require.NoError(t, err)

	prefs, err = ReadPreferences()
	require.NoError(t, err)
	assert.Equal(t, []string{"tasks", "chat"}, prefs.Navigation.Order["agentPages"])
	assert.Equal(t, []string{"logs"}, prefs.Navigation.Hidden["systemPages"])
}
