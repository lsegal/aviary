package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/lsegal/aviary/internal/auth"
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
	if err != nil {
		if !os.IsNotExist(err) {
			return "", false, fmt.Errorf("reading token: %w", err)
		}
	} else {
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
		return "", fmt.Errorf("no token found: run 'aviary serve' first")
	}
	return strings.TrimSpace(string(data)), nil
}

// BearerMiddleware enforces token authentication.
//
// Accepts:
//   - Authorization: Bearer <token>
//   - aviary_session cookie
//   - ?token=<token> query param (for APIs like EventSource that cannot set
//     custom headers)
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

		// Check query token (used by EventSource/SSE where custom headers are
		// not available).
		if q := r.URL.Query().Get("token"); q != "" && q == token {
			next.ServeHTTP(w, r)
			return
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
		// Accept token from JSON body, form value, or Authorization header.
		var submitted string
		if strings.Contains(r.Header.Get("Content-Type"), "application/json") {
			var body struct {
				Token string `json:"token"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
				submitted = body.Token
			}
		}
		if submitted == "" {
			submitted = r.FormValue("token")
		}
		if submitted == "" {
			submitted = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
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
		_, _ = fmt.Fprintln(w, `{"ok":true}`)
	}
}

// makeAuthResolver returns a function that resolves "auth:<key>"
// references by looking them up in the file-backed credential store.
func makeAuthResolver() func(string) (string, error) {
	authPath := filepath.Join(store.SubDir(store.DirAuth), "credentials.json")
	return func(ref string) (string, error) {
		st, err := auth.NewFileStore(authPath)
		if err != nil {
			return "", fmt.Errorf("opening auth store: %w", err)
		}
		return auth.Resolve(st, ref)
	}
}
