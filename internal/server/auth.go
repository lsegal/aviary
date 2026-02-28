package server

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/lsegal/aviary/internal/store"
)

const tokenPrefix = "aviary_tok_"
const tokenFile = "token"

// tokenPath returns the path to the stored token file.
func tokenPath() string {
	return filepath.Join(store.DataDir(), tokenFile)
}

// GenerateToken creates a new random token and saves it.
func GenerateToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating token: %w", err)
	}
	tok := tokenPrefix + hex.EncodeToString(b)
	if err := os.WriteFile(tokenPath(), []byte(tok+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("saving token: %w", err)
	}
	return tok, nil
}

// LoadOrGenerateToken loads the stored token or generates a new one.
// Returns the token and a boolean indicating if it was newly generated.
func LoadOrGenerateToken() (string, bool, error) {
	data, err := os.ReadFile(tokenPath())
	if err == nil {
		tok := strings.TrimSpace(string(data))
		if tok != "" {
			return tok, false, nil
		}
	}
	tok, err := GenerateToken()
	if err != nil {
		return "", false, err
	}
	return tok, true, nil
}

// LoadToken returns the stored token, or an error if none is saved.
func LoadToken() (string, error) {
	data, err := os.ReadFile(tokenPath())
	if err != nil {
		return "", fmt.Errorf("no token found: run 'aviary start' first")
	}
	return strings.TrimSpace(string(data)), nil
}

// BearerMiddleware enforces Bearer token authentication.
func BearerMiddleware(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check Authorization header.
		if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
			if strings.TrimPrefix(auth, "Bearer ") == token {
				next.ServeHTTP(w, r)
				return
			}
		}

		// Check session cookie.
		if cookie, err := r.Cookie("aviary_session"); err == nil {
			if cookie.Value == token {
				next.ServeHTTP(w, r)
				return
			}
		}

		http.Error(w, "Unauthorized", http.StatusUnauthorized)
	})
}

// LoginHandler handles POST /api/login — validates token, sets session cookie.
func LoginHandler(token string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}
		submitted := r.FormValue("token")
		if submitted == "" {
			submitted = r.Header.Get("Authorization")
			submitted = strings.TrimPrefix(submitted, "Bearer ")
		}
		if submitted != token {
			http.Error(w, "Invalid token", http.StatusUnauthorized)
			return
		}
		http.SetCookie(w, &http.Cookie{
			Name:     "aviary_session",
			Value:    token,
			Path:     "/",
			HttpOnly: true,
			Secure:   true,
			SameSite: http.SameSiteStrictMode,
		})
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `{"ok":true}`)
	}
}
