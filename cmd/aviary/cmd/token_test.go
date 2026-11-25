package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lsegal/aviary/internal/server"
	"github.com/lsegal/aviary/internal/store"
)

func setTokenDataDir(t *testing.T, dir string) {
	t.Helper()
	prev := store.DataDir()
	store.SetDataDir(dir)
	t.Cleanup(func() { store.SetDataDir(prev) })
}

func runTokenCommand(t *testing.T, args ...string) (string, error) {
	t.Helper()
	tokenNewFlag = false
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	rootCmd.SetArgs(append([]string{"token"}, args...))
	t.Cleanup(func() {
		tokenNewFlag = false
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
		rootCmd.SetArgs(nil)
	})
	_, err := rootCmd.ExecuteC()
	return out.String(), err
}

func TestTokenCommandWithoutStoredToken(t *testing.T) {
	setTokenDataDir(t, t.TempDir())

	_, err := runTokenCommand(t)
	if err == nil || !strings.Contains(err.Error(), "no token found") {
		t.Fatalf("expected missing token error, got %v", err)
	}
}

func TestTokenCommandPrintsStoredToken(t *testing.T) {
	dir := t.TempDir()
	setTokenDataDir(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "token"), []byte("aviary_tok_stored\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := runTokenCommand(t)
	if err != nil {
		t.Fatalf("token command failed: %v", err)
	}
	if out != "aviary_tok_stored\n" {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestTokenCommandNewGeneratesAndPersistsToken(t *testing.T) {
	setTokenDataDir(t, t.TempDir())
	old, err := server.GenerateToken()
	if err != nil {
		t.Fatalf("seeding token: %v", err)
	}

	out, err := runTokenCommand(t, "--new")
	if err != nil {
		t.Fatalf("token --new failed: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 || lines[0] != "New token generated:" {
		t.Fatalf("unexpected output: %q", out)
	}
	tok := lines[1]
	if tok == old {
		t.Fatalf("expected a new token, got the old one %q", tok)
	}
	stored, err := server.LoadToken()
	if err != nil {
		t.Fatalf("loading stored token: %v", err)
	}
	if stored != tok {
		t.Fatalf("stored token = %q, want %q", stored, tok)
	}

	out, err = runTokenCommand(t)
	if err != nil {
		t.Fatalf("token command failed: %v", err)
	}
	if out != tok+"\n" {
		t.Fatalf("token command printed %q, want %q", out, tok+"\n")
	}
}

func TestTokenCommandNewReportsWriteError(t *testing.T) {
	setTokenDataDir(t, filepath.Join(t.TempDir(), "missing"))

	out, err := runTokenCommand(t, "--new")
	if err == nil || !strings.Contains(err.Error(), "generating token:") {
		t.Fatalf("expected generating token error, got %v", err)
	}
	if strings.Contains(out, "New token generated:") {
		t.Fatalf("unexpected success output: %q", out)
	}
}
