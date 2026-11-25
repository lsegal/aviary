package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authpkg "github.com/lsegal/aviary/internal/auth"
	"github.com/lsegal/aviary/internal/store"
)

var errAuthTest = errors.New("boom")

// stubLoginDeps replaces every login dependency with one that fails the test
// if it is reached, so a test only exercises the fakes it installs itself.
func stubLoginDeps(t *testing.T) {
	t.Helper()
	pkce, browser, exchange := generatePKCE, openBrowser, anthropicExchange
	gemini, openAI, copilot := geminiLogin, openAILogin, copilotLogin
	console, stdin := openConsoleDevice, consoleStdin
	t.Cleanup(func() {
		generatePKCE, openBrowser, anthropicExchange = pkce, browser, exchange
		geminiLogin, openAILogin, copilotLogin = gemini, openAI, copilot
		openConsoleDevice, consoleStdin = console, stdin
	})

	unexpected := func(name string) error { return errors.New("unexpected call to " + name) }
	generatePKCE = func() (authpkg.PKCEParams, error) {
		return authpkg.PKCEParams{Verifier: "test-verifier", Challenge: "test-challenge"}, nil
	}
	openBrowser = func(string) error { return nil }
	anthropicExchange = func(context.Context, string, string) (*authpkg.OAuthToken, error) {
		return nil, unexpected("anthropicExchange")
	}
	geminiLogin = func(context.Context) (*authpkg.OAuthToken, error) { return nil, unexpected("geminiLogin") }
	openAILogin = func(context.Context) (*authpkg.OAuthToken, error) { return nil, unexpected("openAILogin") }
	copilotLogin = func(context.Context) (*authpkg.OAuthToken, error) { return nil, unexpected("copilotLogin") }
	openConsoleDevice = func() (*os.File, error) { return nil, unexpected("openConsoleDevice") }
	consoleStdin = iotest.ErrReader(unexpected("consoleStdin"))
}

// useAuthDir points authStore at a fresh temp directory and returns the
// credentials file path it will use.
func useAuthDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	store.SetAuthDir(dir)
	t.Cleanup(func() { store.SetAuthDir("") })
	return filepath.Join(dir, "credentials.json")
}

func newTestFileStore(t *testing.T) *authpkg.FileStore {
	t.Helper()
	st, err := authpkg.NewFileStore(filepath.Join(t.TempDir(), "credentials.json"))
	require.NoError(t, err)
	return st
}

// consoleFile returns an open file whose contents are input, standing in for
// the console device.
func consoleFile(t *testing.T, input string) func() (*os.File, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "console")
	require.NoError(t, os.WriteFile(path, []byte(input), 0o600))
	return func() (*os.File, error) { return os.Open(path) }
}

func storedToken(t *testing.T, st authpkg.Store, key string) authpkg.OAuthToken {
	t.Helper()
	raw, err := st.Get(key)
	require.NoError(t, err)
	var tok authpkg.OAuthToken
	require.NoError(t, json.Unmarshal([]byte(raw), &tok))
	return tok
}

func testToken(access string) *authpkg.OAuthToken {
	return &authpkg.OAuthToken{AccessToken: access, RefreshToken: access + "-refresh", ExpiresAt: 1234}
}

func TestLoginAnthropicWithPresetCode(t *testing.T) {
	stubLoginDeps(t)
	var opened string
	openBrowser = func(u string) error { opened = u; return nil }
	var gotCode, gotVerifier string
	anthropicExchange = func(_ context.Context, code, verifier string) (*authpkg.OAuthToken, error) {
		gotCode, gotVerifier = code, verifier
		return testToken("anthropic-access"), nil
	}
	st := newTestFileStore(t)

	out, err := captureStdout(t, func() error { return loginAnthropic(st, "preset-code") })
	require.NoError(t, err)

	assert.Equal(t, "preset-code", gotCode)
	assert.Equal(t, "test-verifier", gotVerifier)
	assert.Contains(t, opened, "code_challenge=test-challenge")
	assert.Contains(t, out, opened)
	assert.NotContains(t, out, "Paste the authorization code")
	assert.NotContains(t, out, "Could not open browser")
	assert.Contains(t, out, "Token stored as anthropic:oauth")
	assert.Equal(t, *testToken("anthropic-access"), storedToken(t, st, "anthropic:oauth"))
}

func TestLoginAnthropicPromptsForCode(t *testing.T) {
	stubLoginDeps(t)
	openBrowser = func(string) error { return errAuthTest }
	openConsoleDevice = consoleFile(t, "  typed-code \r\n")
	var gotCode string
	anthropicExchange = func(_ context.Context, code, _ string) (*authpkg.OAuthToken, error) {
		gotCode = code
		return testToken("typed"), nil
	}
	st := newTestFileStore(t)

	out, err := captureStdout(t, func() error { return loginAnthropic(st, "") })
	require.NoError(t, err)

	assert.Equal(t, "typed-code", gotCode)
	assert.Contains(t, out, "Could not open browser automatically")
	assert.Contains(t, out, "Paste the authorization code")
	assert.Equal(t, "typed", storedToken(t, st, "anthropic:oauth").AccessToken)
}

func TestLoginAnthropicErrors(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(t *testing.T)
		store   func(t *testing.T) authpkg.Store
		wantErr string
	}{
		{
			name: "pkce",
			setup: func(*testing.T) {
				generatePKCE = func() (authpkg.PKCEParams, error) { return authpkg.PKCEParams{}, errAuthTest }
			},
			wantErr: "generating PKCE: boom",
		},
		{
			name: "read code",
			setup: func(*testing.T) {
				openConsoleDevice = func() (*os.File, error) { return nil, errAuthTest }
				consoleStdin = iotest.ErrReader(errAuthTest)
			},
			wantErr: "reading authorization code: boom",
		},
		{
			name:    "empty code",
			setup:   func(t *testing.T) { openConsoleDevice = consoleFile(t, "   \n") },
			wantErr: "no code entered; login cancelled",
		},
		{
			name: "exchange",
			setup: func(*testing.T) {
				anthropicExchange = func(context.Context, string, string) (*authpkg.OAuthToken, error) {
					return nil, errAuthTest
				}
			},
			wantErr: "completing Anthropic login: boom",
		},
		{
			name: "save",
			setup: func(*testing.T) {
				anthropicExchange = func(context.Context, string, string) (*authpkg.OAuthToken, error) {
					return testToken("x"), nil
				}
			},
			store:   func(*testing.T) authpkg.Store { return &errStore{err: errAuthTest} },
			wantErr: "saving token: boom",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stubLoginDeps(t)
			tt.setup(t)
			var st authpkg.Store = newTestFileStore(t)
			if tt.store != nil {
				st = tt.store(t)
			}
			code := "preset"
			if tt.name == "read code" || tt.name == "empty code" {
				code = ""
			}

			_, err := captureStdout(t, func() error { return loginAnthropic(st, code) })
			require.EqualError(t, err, tt.wantErr)
			if fs, ok := st.(*authpkg.FileStore); ok {
				keys, listErr := fs.List()
				require.NoError(t, listErr)
				assert.Empty(t, keys)
			}
		})
	}
}

// browserLoginCase describes one of the login flows that delegates entirely
// to an internal/auth helper and stores the resulting token.
type browserLoginCase struct {
	name    string
	key     string
	success string
	errWrap string
	install func(func(context.Context) (*authpkg.OAuthToken, error))
	login   func(authpkg.Store) error
}

func browserLoginCases() []browserLoginCase {
	return []browserLoginCase{
		{
			name:    "gemini",
			key:     "gemini:oauth",
			success: "Gemini OAuth login successful",
			errWrap: "gemini login: ",
			install: func(f func(context.Context) (*authpkg.OAuthToken, error)) { geminiLogin = f },
			login:   loginGemini,
		},
		{
			name:    "openai",
			key:     "openai:oauth",
			success: "OpenAI OAuth login successful",
			errWrap: "OpenAI login: ",
			install: func(f func(context.Context) (*authpkg.OAuthToken, error)) { openAILogin = f },
			login:   loginOpenAI,
		},
		{
			name:    "github-copilot",
			key:     "github-copilot:oauth",
			success: "GitHub Copilot login successful",
			errWrap: "github copilot login: ",
			install: func(f func(context.Context) (*authpkg.OAuthToken, error)) { copilotLogin = f },
			login:   func(st authpkg.Store) error { return loginGitHubCopilot(st, "") },
		},
	}
}

func TestBrowserLoginsStoreToken(t *testing.T) {
	for _, tc := range browserLoginCases() {
		t.Run(tc.name, func(t *testing.T) {
			stubLoginDeps(t)
			var hadDeadline bool
			tc.install(func(ctx context.Context) (*authpkg.OAuthToken, error) {
				_, hadDeadline = ctx.Deadline()
				return testToken(tc.name), nil
			})
			st := newTestFileStore(t)

			out, err := captureStdout(t, func() error { return tc.login(st) })
			require.NoError(t, err)

			assert.True(t, hadDeadline, "login context should time out")
			assert.Contains(t, out, tc.success)
			assert.Equal(t, *testToken(tc.name), storedToken(t, st, tc.key))
		})
	}
}

func TestBrowserLoginsReportErrors(t *testing.T) {
	for _, tc := range browserLoginCases() {
		t.Run(tc.name+"/login", func(t *testing.T) {
			stubLoginDeps(t)
			tc.install(func(context.Context) (*authpkg.OAuthToken, error) { return nil, errAuthTest })
			st := newTestFileStore(t)

			_, err := captureStdout(t, func() error { return tc.login(st) })
			require.EqualError(t, err, tc.errWrap+"boom")
			keys, listErr := st.List()
			require.NoError(t, listErr)
			assert.Empty(t, keys)
		})
		t.Run(tc.name+"/save", func(t *testing.T) {
			stubLoginDeps(t)
			tc.install(func(context.Context) (*authpkg.OAuthToken, error) { return testToken("x"), nil })

			out, err := captureStdout(t, func() error { return tc.login(&errStore{err: errAuthTest}) })
			require.EqualError(t, err, "saving token: boom")
			assert.NotContains(t, out, tc.success)
		})
	}
}

func TestLoginGitHubCopilotPAT(t *testing.T) {
	stubLoginDeps(t)
	st := newTestFileStore(t)

	out, err := captureStdout(t, func() error { return loginGitHubCopilot(st, "  ghp_token\n") })
	require.NoError(t, err)

	assert.Contains(t, out, "PAT stored as github-copilot:default")
	got, err := st.Get("github-copilot:default")
	require.NoError(t, err)
	assert.Equal(t, "ghp_token", got)
}

func TestLoginGitHubCopilotPATErrors(t *testing.T) {
	stubLoginDeps(t)

	st := newTestFileStore(t)
	_, err := captureStdout(t, func() error { return loginGitHubCopilot(st, "   ") })
	require.EqualError(t, err, "no PAT provided; login cancelled")
	keys, err := st.List()
	require.NoError(t, err)
	assert.Empty(t, keys)

	_, err = captureStdout(t, func() error { return loginGitHubCopilot(&errStore{err: errAuthTest}, "ghp_token") })
	require.EqualError(t, err, "saving PAT: boom")
}

func TestReadConsoleLine(t *testing.T) {
	t.Run("console device", func(t *testing.T) {
		stubLoginDeps(t)
		openConsoleDevice = consoleFile(t, "from console\nsecond line\n")

		line, err := readConsoleLine()
		require.NoError(t, err)
		assert.Equal(t, "from console\n", line)
	})
	t.Run("console device without newline", func(t *testing.T) {
		stubLoginDeps(t)
		openConsoleDevice = consoleFile(t, "partial")

		line, err := readConsoleLine()
		require.NoError(t, err)
		assert.Equal(t, "partial", line)
	})
	t.Run("console device read error", func(t *testing.T) {
		stubLoginDeps(t)
		open := consoleFile(t, "unread")
		openConsoleDevice = func() (*os.File, error) {
			f, err := open()
			if err == nil {
				err = f.Close()
			}
			return f, err
		}

		_, err := readConsoleLine()
		require.ErrorIs(t, err, os.ErrClosed)
	})
	t.Run("stdin fallback", func(t *testing.T) {
		stubLoginDeps(t)
		openConsoleDevice = func() (*os.File, error) { return nil, errAuthTest }
		consoleStdin = strings.NewReader("from stdin\nmore\n")

		line, err := readConsoleLine()
		require.NoError(t, err)
		assert.Equal(t, "from stdin\n", line)
	})
	t.Run("stdin fallback without newline", func(t *testing.T) {
		stubLoginDeps(t)
		openConsoleDevice = func() (*os.File, error) { return nil, errAuthTest }
		consoleStdin = strings.NewReader("tail")

		line, err := readConsoleLine()
		require.NoError(t, err)
		assert.Equal(t, "tail", line)
	})
	t.Run("stdin fallback read error", func(t *testing.T) {
		stubLoginDeps(t)
		openConsoleDevice = func() (*os.File, error) { return nil, errAuthTest }
		consoleStdin = iotest.ErrReader(errAuthTest)

		_, err := readConsoleLine()
		require.ErrorIs(t, err, errAuthTest)
	})
}

func TestAuthStoreUsesCredentialsFile(t *testing.T) {
	path := useAuthDir(t)

	st := authStore()
	require.IsType(t, &authpkg.FileStore{}, st)
	require.NoError(t, st.Set("key", "value"))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"key": "value"`)

	got, err := authStore().Get("key")
	require.NoError(t, err)
	assert.Equal(t, "value", got)
}

func TestAuthStoreFallsBackToErrStore(t *testing.T) {
	path := useAuthDir(t)
	require.NoError(t, os.WriteFile(path, []byte("not json"), 0o600))

	st := authStore()
	es, ok := st.(*errStore)
	require.True(t, ok, "expected *errStore, got %T", st)
	require.Error(t, es.err)

	assert.Equal(t, es.err, st.Set("k", "v"))
	val, err := st.Get("k")
	assert.Equal(t, es.err, err)
	assert.Empty(t, val)
	assert.Equal(t, es.err, st.Delete("k"))
	keys, err := st.List()
	assert.Equal(t, es.err, err)
	assert.Nil(t, keys)
}

// runAuthCmd runs an auth subcommand's RunE with stdout captured.
func runAuthCmd(t *testing.T, run func() error) (string, error) {
	t.Helper()
	return captureStdout(t, run)
}

func TestAuthCredentialCommands(t *testing.T) {
	useAuthDir(t)

	out, err := runAuthCmd(t, func() error { return authListCmd.RunE(authListCmd, nil) })
	require.NoError(t, err)
	assert.Equal(t, "No credentials stored.\n", out)

	out, err = runAuthCmd(t, func() error { return authSetCmd.RunE(authSetCmd, []string{"openai:default", "sk-1234567890"}) })
	require.NoError(t, err)
	assert.Equal(t, "Credential \"openai:default\" stored.\n", out)

	out, err = runAuthCmd(t, func() error { return authSetCmd.RunE(authSetCmd, []string{"short", "abc"}) })
	require.NoError(t, err)
	assert.Contains(t, out, "stored")

	out, err = runAuthCmd(t, func() error { return authGetCmd.RunE(authGetCmd, []string{"openai:default"}) })
	require.NoError(t, err)
	assert.Equal(t, "Credential \"openai:default\": ********\n", out)
	assert.NotContains(t, out, "sk-")

	out, err = runAuthCmd(t, func() error { return authGetCmd.RunE(authGetCmd, []string{"short"}) })
	require.NoError(t, err)
	assert.Equal(t, "Credential \"short\": ***\n", out)

	out, err = runAuthCmd(t, func() error { return authListCmd.RunE(authListCmd, nil) })
	require.NoError(t, err)
	assert.Contains(t, out, " • openai:default\n")
	assert.Contains(t, out, " • short\n")

	out, err = runAuthCmd(t, func() error { return authDeleteCmd.RunE(authDeleteCmd, []string{"short"}) })
	require.NoError(t, err)
	assert.Equal(t, "Credential \"short\" deleted.\n", out)

	_, err = runAuthCmd(t, func() error { return authGetCmd.RunE(authGetCmd, []string{"short"}) })
	require.ErrorContains(t, err, "getting credential: ")
}

func TestAuthCredentialCommandsReportStoreErrors(t *testing.T) {
	path := useAuthDir(t)
	require.NoError(t, os.WriteFile(path, []byte("not json"), 0o600))

	tests := []struct {
		name    string
		run     func() error
		wantErr string
	}{
		{"set", func() error { return authSetCmd.RunE(authSetCmd, []string{"k", "v"}) }, "storing credential: "},
		{"get", func() error { return authGetCmd.RunE(authGetCmd, []string{"k"}) }, "getting credential: "},
		{"list", func() error { return authListCmd.RunE(authListCmd, nil) }, "listing credentials: "},
		{"delete", func() error { return authDeleteCmd.RunE(authDeleteCmd, []string{"k"}) }, "deleting credential: "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := runAuthCmd(t, tt.run)
			require.ErrorContains(t, err, tt.wantErr)
			assert.Empty(t, out)
		})
	}
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "not json", string(data), "a failing store must not overwrite the file")
}

func TestAuthLoginCommandDispatchesByProvider(t *testing.T) {
	savedCode, savedPat := authLoginCode, authLoginPat
	t.Cleanup(func() { authLoginCode, authLoginPat = savedCode, savedPat })

	tests := []struct {
		provider string
		setup    func()
		key      string
	}{
		{"Anthropic", func() {
			authLoginCode = "flag-code"
			anthropicExchange = func(_ context.Context, code, _ string) (*authpkg.OAuthToken, error) {
				return testToken(code), nil
			}
		}, "anthropic:oauth"},
		{"openai", func() { openAILogin = func(context.Context) (*authpkg.OAuthToken, error) { return testToken("o"), nil } }, "openai:oauth"},
		{"GEMINI", func() { geminiLogin = func(context.Context) (*authpkg.OAuthToken, error) { return testToken("g"), nil } }, "gemini:oauth"},
		{"github-copilot", func() { authLoginPat = "ghp_flag" }, "github-copilot:default"},
	}
	for _, tt := range tests {
		t.Run(tt.provider, func(t *testing.T) {
			stubLoginDeps(t)
			useAuthDir(t)
			authLoginCode, authLoginPat = "", ""
			tt.setup()

			_, err := captureStdout(t, func() error { return authLoginCmd.RunE(authLoginCmd, []string{tt.provider}) })
			require.NoError(t, err)

			keys, err := authStore().List()
			require.NoError(t, err)
			assert.Equal(t, []string{tt.key}, keys)
		})
	}

	t.Run("unknown", func(t *testing.T) {
		stubLoginDeps(t)
		useAuthDir(t)
		err := authLoginCmd.RunE(authLoginCmd, []string{"Nope"})
		require.EqualError(t, err, `unknown provider "nope"; supported providers: anthropic, openai, gemini, github-copilot`)
	})
}
