package auth

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zalando/go-keyring"
)

// useMockKeyring swaps the system keychain for go-keyring's in-memory mock so
// tests never touch real credentials. A fresh mock is installed on cleanup so
// an error-returning provider never leaks into other tests.
func useMockKeyring(t *testing.T) {
	t.Helper()
	keyring.MockInit()
	t.Cleanup(keyring.MockInit)
}

func TestKeychainStore_SetGetStoresValue(t *testing.T) {
	useMockKeyring(t)
	s := NewKeychainStore()

	require.NoError(t, s.Set("anthropic:default", "secret"))

	val, err := s.Get("anthropic:default")
	require.NoError(t, err)
	assert.Equal(t, "secret", val)

	stored, err := keyring.Get(keychainService, "anthropic:default")
	require.NoError(t, err)
	assert.Equal(t, "secret", stored)
}

func TestKeychainStore_SetTracksKeysSortedWithoutDuplicates(t *testing.T) {
	useMockKeyring(t)
	s := NewKeychainStore()

	require.NoError(t, s.Set("openai:default", "a"))
	require.NoError(t, s.Set("anthropic:default", "b"))
	require.NoError(t, s.Set("openai:default", "c"))

	keys, err := s.List()
	require.NoError(t, err)
	assert.Equal(t, []string{"anthropic:default", "openai:default"}, keys)

	val, err := s.Get("openai:default")
	require.NoError(t, err)
	assert.Equal(t, "c", val, "overwriting a key stores the new value")
}

func TestKeychainStore_ListReturnsCopy(t *testing.T) {
	useMockKeyring(t)
	s := NewKeychainStore()
	require.NoError(t, s.Set("k", "v"))

	keys, err := s.List()
	require.NoError(t, err)
	keys[0] = "mutated"

	keys, err = s.List()
	require.NoError(t, err)
	assert.Equal(t, []string{"k"}, keys)
}

func TestKeychainStore_ListEmpty(t *testing.T) {
	useMockKeyring(t)
	keys, err := NewKeychainStore().List()
	require.NoError(t, err)
	assert.Empty(t, keys)
}

func TestKeychainStore_SetErrorWrapsKeyAndSkipsTracking(t *testing.T) {
	useMockKeyring(t)
	backendErr := errors.New("keychain locked")
	keyring.MockInitWithError(backendErr)
	s := NewKeychainStore()

	err := s.Set("gemini:default", "v")
	require.Error(t, err)
	assert.ErrorIs(t, err, backendErr)
	assert.Contains(t, err.Error(), `keychain set "gemini:default"`)

	keys, err := s.List()
	require.NoError(t, err)
	assert.Empty(t, keys, "a failed Set must not track the key")
}

func TestKeychainStore_GetNotFound(t *testing.T) {
	useMockKeyring(t)
	_, err := NewKeychainStore().Get("missing")
	require.Error(t, err)
	assert.True(t, IsNotFound(err))
	var nf *ErrNotFound
	require.ErrorAs(t, err, &nf)
	assert.Equal(t, "missing", nf.Key)
}

func TestKeychainStore_GetBackendErrorReportsNotFound(t *testing.T) {
	useMockKeyring(t)
	keyring.MockInitWithError(errors.New("keychain unavailable"))

	_, err := NewKeychainStore().Get("k")
	assert.True(t, IsNotFound(err))
}

func TestKeychainStore_DeleteRemovesValueAndTrackedKey(t *testing.T) {
	useMockKeyring(t)
	s := NewKeychainStore()
	require.NoError(t, s.Set("a", "1"))
	require.NoError(t, s.Set("b", "2"))
	require.NoError(t, s.Set("c", "3"))

	require.NoError(t, s.Delete("b"))

	_, err := s.Get("b")
	assert.True(t, IsNotFound(err))
	keys, err := s.List()
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "c"}, keys)

	val, err := s.Get("a")
	require.NoError(t, err)
	assert.Equal(t, "1", val)
}

func TestKeychainStore_DeleteUntrackedKey(t *testing.T) {
	useMockKeyring(t)
	require.NoError(t, keyring.Set(keychainService, "external", "v"))
	s := NewKeychainStore()
	require.NoError(t, s.Set("tracked", "v"))

	require.NoError(t, s.Delete("external"))

	_, err := s.Get("external")
	assert.True(t, IsNotFound(err))
	keys, err := s.List()
	require.NoError(t, err)
	assert.Equal(t, []string{"tracked"}, keys)
}

func TestKeychainStore_DeleteMissingKeyErrors(t *testing.T) {
	useMockKeyring(t)
	err := NewKeychainStore().Delete("missing")
	require.Error(t, err)
	assert.ErrorIs(t, err, keyring.ErrNotFound)
	assert.Contains(t, err.Error(), `keychain delete "missing"`)
}

func TestKeychainStore_DeleteErrorKeepsTrackedKey(t *testing.T) {
	useMockKeyring(t)
	s := NewKeychainStore()
	require.NoError(t, s.Set("k", "v"))

	backendErr := errors.New("keychain locked")
	keyring.MockInitWithError(backendErr)

	err := s.Delete("k")
	require.Error(t, err)
	assert.ErrorIs(t, err, backendErr)
	assert.Contains(t, err.Error(), `keychain delete "k"`)

	keys, err := s.List()
	require.NoError(t, err)
	assert.Equal(t, []string{"k"}, keys)
}
