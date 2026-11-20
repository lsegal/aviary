package projects

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/lsegal/aviary/internal/auth"
)

// useWebhookAuthStore points webhook secret resolution at a fresh credential
// store holding creds.
func useWebhookAuthStore(t *testing.T, creds map[string]string) {
	t.Helper()
	st, err := auth.NewFileStore(filepath.Join(t.TempDir(), "credentials.json"))
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range creds {
		if err := st.Set(key, value); err != nil {
			t.Fatal(err)
		}
	}
	prev := openWebhookAuthStore
	openWebhookAuthStore = func() (auth.Store, error) { return st, nil }
	t.Cleanup(func() { openWebhookAuthStore = prev })
}

// An auth:<key> webhook secret resolves through the credential store, so it
// need not sit in plaintext in aviary.yaml (issue #437).
func TestResolveWebhookSecretFromCredentialStore(t *testing.T) {
	useWebhookAuthStore(t, map[string]string{"projects:webhook": "s3cret"})
	got, err := resolveWebhookSecret("auth:projects:webhook")
	if err != nil {
		t.Fatal(err)
	}
	if got != "s3cret" {
		t.Fatalf("secret = %q, want %q", got, "s3cret")
	}
}

// A plain webhook secret keeps working and never opens the credential store.
func TestResolveWebhookSecretKeepsPlainValue(t *testing.T) {
	prev := openWebhookAuthStore
	openWebhookAuthStore = func() (auth.Store, error) {
		t.Fatal("plain secret opened the credential store")
		return nil, nil
	}
	t.Cleanup(func() { openWebhookAuthStore = prev })
	for _, secret := range []string{"", "plain-secret"} {
		got, err := resolveWebhookSecret(secret)
		if err != nil || got != secret {
			t.Fatalf("resolveWebhookSecret(%q) = %q, %v; want %q, nil", secret, got, err, secret)
		}
	}
}

// A missing credential fails the webhook start rather than verifying
// signatures against the literal "auth:<key>" text.
func TestResolveWebhookSecretReportsMissingCredential(t *testing.T) {
	useWebhookAuthStore(t, nil)
	_, err := resolveWebhookSecret("auth:projects:missing")
	if err == nil || !strings.Contains(err.Error(), "projects webhook secret") {
		t.Fatalf("err = %v, want a projects webhook secret error", err)
	}
}
