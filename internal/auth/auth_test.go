package auth

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"
)

func TestErrNotFoundAndIsNotFound(t *testing.T) {
	err := &ErrNotFound{Key: "k1"}
	if err.Error() != `credential "k1" not found` {
		t.Fatalf("unexpected error string: %s", err.Error())
	}
	if !IsNotFound(err) {
		t.Fatal("IsNotFound should return true for ErrNotFound")
	}
	if IsNotFound(errors.New("other")) {
		t.Fatal("IsNotFound should return false for non-ErrNotFound")
	}
}

type mockStore struct {
	data map[string]string
}

func (m *mockStore) Set(key, value string) error {
	m.data[key] = value
	return nil
}

func (m *mockStore) Get(key string) (string, error) {
	v, ok := m.data[key]
	if !ok {
		return "", &ErrNotFound{Key: key}
	}
	return v, nil
}

func (m *mockStore) Delete(key string) error {
	delete(m.data, key)
	return nil
}

func (m *mockStore) List() ([]string, error) {
	keys := make([]string, 0, len(m.data))
	for k := range m.data {
		keys = append(keys, k)
	}
	return keys, nil
}

func TestResolve(t *testing.T) {
	store := &mockStore{data: map[string]string{"anthropic:default": "sk-abc"}}

	got, err := Resolve(store, "literal-token")
	if err != nil || got != "literal-token" {
		t.Fatalf("resolve literal got %q err %v", got, err)
	}

	got, err = Resolve(store, "auth:anthropic:default")
	if err != nil || got != "sk-abc" {
		t.Fatalf("resolve ref got %q err %v", got, err)
	}

	_, err = Resolve(store, "auth:")
	if err == nil {
		t.Fatal("expected invalid auth ref error")
	}

	_, err = Resolve(store, "auth:missing:key")
	if err == nil {
		t.Fatal("expected missing key error")
	}
}

func TestParseRef(t *testing.T) {
	provider, name, ok := ParseRef("auth:anthropic:default")
	if !ok || provider != "anthropic" || name != "default" {
		t.Fatalf("unexpected parse: %q %q %v", provider, name, ok)
	}

	provider, name, ok = ParseRef("auth:openai")
	if !ok || provider != "openai" || name != "" {
		t.Fatalf("unexpected parse short: %q %q %v", provider, name, ok)
	}

	provider, name, ok = ParseRef("literal")
	if ok || provider != "" || name != "" {
		t.Fatalf("unexpected non-auth parse: %q %q %v", provider, name, ok)
	}
}

func TestFileStoreCRUDAndReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	s, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("new filestore: %v", err)
	}

	if err := s.Set("a", "1"); err != nil {
		t.Fatalf("set a: %v", err)
	}
	if err := s.Set("b", "2"); err != nil {
		t.Fatalf("set b: %v", err)
	}

	v, err := s.Get("a")
	if err != nil || v != "1" {
		t.Fatalf("get a got %q err %v", v, err)
	}

	keys, err := s.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !reflect.DeepEqual(keys, []string{"a", "b"}) {
		t.Fatalf("keys = %v", keys)
	}

	if err := s.Delete("a"); err != nil {
		t.Fatalf("delete a: %v", err)
	}
	if _, err := s.Get("a"); !IsNotFound(err) {
		t.Fatalf("expected not found after delete, got %v", err)
	}
	if err := s.Delete("a"); !IsNotFound(err) {
		t.Fatalf("expected delete missing not found, got %v", err)
	}

	s2, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("new filestore #2: %v", err)
	}
	v, err = s2.Get("b")
	if err != nil || v != "2" {
		t.Fatalf("reloaded get b got %q err %v", v, err)
	}
}

func TestIntegration_FileStoreResolve(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	s, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("new filestore: %v", err)
	}
	if err := s.Set("openai:default", "sk-test"); err != nil {
		t.Fatalf("set: %v", err)
	}
	got, err := Resolve(s, "auth:openai:default")
	if err != nil || got != "sk-test" {
		t.Fatalf("resolve got %q err %v", got, err)
	}
}
