package memory

import (
	"strings"
	"testing"

	"github.com/lsegal/aviary/internal/store"

	"github.com/stretchr/testify/assert"
)

func setupMemoryDir(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	store.SetDataDir(tmp)
	store.SetWorkspaceDir(tmp)
	t.Cleanup(func() {
		store.SetDataDir("")
		store.SetWorkspaceDir("")
	})
}

func TestGetNotes_Missing(t *testing.T) {
	setupMemoryDir(t)
	m := New()
	notes, err := m.GetNotes("pool1")
	assert.NoError(t, err)
	assert.Equal(t, "", notes)

}

func TestSetNotesAndGetNotes(t *testing.T) {
	setupMemoryDir(t)
	m := New()
	err := m.SetNotes("pool1", "hello notes")
	assert.NoError(t, err)

	got, err := m.GetNotes("pool1")
	assert.NoError(t, err)
	assert.Equal(t, "hello notes", got)

}

func TestGetNotes_StripsCommentLines(t *testing.T) {
	setupMemoryDir(t)
	m := New()
	err := m.SetNotes("pool1", "visible\n<!-- hidden -->\nstill here")
	assert.NoError(t, err)

	got, err := m.GetNotes("pool1")
	assert.NoError(t, err)
	assert.Equal(t, "visible\nstill here", got)
}

func TestAppendNote(t *testing.T) {
	setupMemoryDir(t)
	m := New()

	// Append to empty
	err := m.AppendNote("pool1", "first note")
	assert.NoError(t, err)

	// Append second
	err = m.AppendNote("pool1", "second note")
	assert.NoError(t, err)

	got, err := m.GetNotes("pool1")
	assert.NoError(t, err)
	assert.True(t, strings.Contains(got, "- first note"))
	assert.True(t, strings.Contains(got, "- second note"))

}
