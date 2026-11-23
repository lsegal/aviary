package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/store"
)

func navRequest(t *testing.T, method, body string) (int, store.NavPreferences) {
	t.Helper()
	rec := httptest.NewRecorder()
	navPreferencesHandler(rec, httptest.NewRequest(method, "/api/preferences/navigation", strings.NewReader(body)))
	var nav store.NavPreferences
	if rec.Code == http.StatusOK {
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &nav))
	}
	return rec.Code, nav
}

func TestNavPreferencesHandler_SaveReadReset(t *testing.T) {
	store.SetDataDir(t.TempDir())
	t.Cleanup(func() { store.SetDataDir("") })

	code, nav := navRequest(t, http.MethodGet, "")
	require.Equal(t, http.StatusOK, code)
	assert.Empty(t, nav.Order)

	code, _ = navRequest(t, http.MethodPut, `{"order":{"agentPages":["tasks","chat","tasks",""],"empty":[]},"hidden":{"systemPages":["logs"]}}`)
	require.Equal(t, http.StatusOK, code)

	code, nav = navRequest(t, http.MethodGet, "")
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, map[string][]string{"agentPages": {"tasks", "chat"}}, nav.Order)
	assert.Equal(t, map[string][]string{"systemPages": {"logs"}}, nav.Hidden)

	code, nav = navRequest(t, http.MethodDelete, "")
	require.Equal(t, http.StatusOK, code)
	assert.Empty(t, nav.Order)
	assert.Empty(t, nav.Hidden)

	code, nav = navRequest(t, http.MethodGet, "")
	require.Equal(t, http.StatusOK, code)
	assert.Empty(t, nav.Order)
}

func TestNavPreferencesHandler_RejectsBadInput(t *testing.T) {
	store.SetDataDir(t.TempDir())
	t.Cleanup(func() { store.SetDataDir("") })

	code, _ := navRequest(t, http.MethodPut, `{"order":`)
	assert.Equal(t, http.StatusBadRequest, code)
	code, _ = navRequest(t, http.MethodPost, `{}`)
	assert.Equal(t, http.StatusMethodNotAllowed, code)
}
