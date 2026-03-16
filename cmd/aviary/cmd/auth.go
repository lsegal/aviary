package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	authpkg "github.com/lsegal/aviary/internal/auth"
	"github.com/lsegal/aviary/internal/store"
)

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Manage authentication credentials",
}

var authLoginCode string
var authLoginPat string

var authLoginCmd = &cobra.Command{
	Use:   "login <provider>",
	Short: "Authorize with a provider. Providers: anthropic, openai, gemini, github-copilot",
	Args:  cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		provider := strings.ToLower(args[0])
		st := authStore()

		switch provider {
		case "anthropic":
			return loginAnthropic(st, authLoginCode)
		case "openai":
			return loginOpenAI(st)
		case "gemini":
			return loginGemini(st)
		case "github-copilot":
			return loginGitHubCopilot(st, authLoginPat)
		default:
			return fmt.Errorf("unknown provider %q; supported providers: anthropic, openai, gemini, github-copilot", provider)
		}
	},
}

func loginAnthropic(st authpkg.Store, presetCode string) error {
	pkce, err := authpkg.GeneratePKCE()
	if err != nil {
		return fmt.Errorf("generating PKCE: %w", err)
	}

	authURL := authpkg.AnthropicBuildAuthorizeURL(pkce, "max")
	fmt.Println("Opening browser for Anthropic Claude Pro/Max authorization...")
	fmt.Println()
	fmt.Println("  ", authURL)
	fmt.Println()

	if err := authpkg.OpenBrowser(authURL); err != nil {
		fmt.Println("(Could not open browser automatically; copy the URL above.)")
	}

	var code string
	if presetCode != "" {
		code = presetCode
	} else {
		fmt.Print("Paste the authorization code shown on the Anthropic page: ")
		code, err = readConsoleLine()
		if err != nil {
			return fmt.Errorf("reading authorization code: %w", err)
		}
		code = strings.TrimSpace(code)
	}
	if code == "" {
		return fmt.Errorf("no code entered; login cancelled")
	}

	fmt.Println("Exchanging code for tokens...")
	token, err := authpkg.AnthropicExchange(context.Background(), code, pkce.Verifier)
	if err != nil {
		return fmt.Errorf("completing Anthropic login: %w", err)
	}

	// Store the OAuth token JSON at a well-known key.
	tokenJSON, _ := marshalOAuthToken(token)
	if err := st.Set("anthropic:oauth", tokenJSON); err != nil {
		return fmt.Errorf("saving token: %w", err)
	}

	fmt.Println("Anthropic OAuth login successful. Token stored as anthropic:oauth")
	return nil
}

func loginGemini(st authpkg.Store) error {
	fmt.Println("Starting Gemini OAuth login — opening browser...")

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	token, err := authpkg.GeminiLogin(ctx)
	if err != nil {
		return fmt.Errorf("gemini login: %w", err)
	}

	tokenJSON, _ := marshalOAuthToken(token)
	if err := st.Set("gemini:oauth", tokenJSON); err != nil {
		return fmt.Errorf("saving token: %w", err)
	}

	fmt.Println("Gemini OAuth login successful. Credentials stored as gemini:oauth.")
	return nil
}

func loginOpenAI(st authpkg.Store) error {
	fmt.Println("Starting OpenAI OAuth login — opening browser...")
	fmt.Println("(If the browser does not open, copy the URL printed above manually.)")

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	token, err := authpkg.OpenAILogin(ctx)
	if err != nil {
		return fmt.Errorf("OpenAI login: %w", err)
	}

	tokenJSON, _ := marshalOAuthToken(token)
	if err := st.Set("openai:oauth", tokenJSON); err != nil {
		return fmt.Errorf("saving token: %w", err)
	}

	fmt.Println("OpenAI OAuth login successful.")
	fmt.Println("Credential stored as openai:oauth (access token is the API key).")
	return nil
}

var authSetCmd = &cobra.Command{
	Use:   "set <name> <value>",
	Short: "Store a credential (API key or token) by name",
	Args:  cobra.ExactArgs(2),
	RunE: func(_ *cobra.Command, args []string) error {
		st := authStore()
		if err := st.Set(args[0], args[1]); err != nil {
			return fmt.Errorf("storing credential: %w", err)
		}
		fmt.Printf("Credential %q stored.\n", args[0])
		return nil
	},
}

var authGetCmd = &cobra.Command{
	Use:   "get <name>",
	Short: "Show whether a credential is stored (value masked)",
	Args:  cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		st := authStore()
		val, err := st.Get(args[0])
		if err != nil {
			return fmt.Errorf("getting credential: %w", err)
		}
		masked := strings.Repeat("*", min(len(val), 8))
		fmt.Printf("Credential %q: %s\n", args[0], masked)
		return nil
	},
}

var authListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all stored credential names",
	RunE: func(_ *cobra.Command, _ []string) error {
		st := authStore()
		keys, err := st.List()
		if err != nil {
			return fmt.Errorf("listing credentials: %w", err)
		}
		if len(keys) == 0 {
			fmt.Println("No credentials stored.")
			return nil
		}
		for _, k := range keys {
			fmt.Println(" •", k)
		}
		return nil
	},
}

var authDeleteCmd = &cobra.Command{
	Use:   "delete <name>",
	Short: "Remove a stored credential",
	Args:  cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		st := authStore()
		if err := st.Delete(args[0]); err != nil {
			return fmt.Errorf("deleting credential: %w", err)
		}
		fmt.Printf("Credential %q deleted.\n", args[0])
		return nil
	},
}

func init() {
	authLoginCmd.Flags().StringVar(&authLoginCode, "code", "", "Authorization code (skips interactive prompt)")
	authLoginCmd.Flags().StringVar(&authLoginPat, "pat", "", "GitHub PAT for github-copilot (skips device flow)")
	authCmd.AddCommand(authLoginCmd, authSetCmd, authGetCmd, authListCmd, authDeleteCmd)
	rootCmd.AddCommand(authCmd)
}

// loginGitHubCopilot authenticates with GitHub Copilot.
// Default: GitHub device flow using the official Copilot OAuth app.
// --pat: store a raw PAT/token directly (skips device flow).
func loginGitHubCopilot(st authpkg.Store, patFlag string) error {
	if patFlag != "" {
		pat := strings.TrimSpace(patFlag)
		if pat == "" {
			return fmt.Errorf("no PAT provided; login cancelled")
		}
		if err := st.Set("github-copilot:default", pat); err != nil {
			return fmt.Errorf("saving PAT: %w", err)
		}
		fmt.Println("github-copilot PAT stored as github-copilot:default")
		return nil
	}

	// Device flow: no browser needed, just display the user code.
	fmt.Println("Authorizing GitHub Copilot via device flow...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	token, err := authpkg.CopilotLogin(ctx)
	if err != nil {
		return fmt.Errorf("github copilot login: %w", err)
	}
	tokenJSON, _ := marshalOAuthToken(token)
	if err := st.Set("github-copilot:oauth", tokenJSON); err != nil {
		return fmt.Errorf("saving token: %w", err)
	}
	fmt.Println("GitHub Copilot login successful. Credentials stored as github-copilot:oauth.")
	return nil
}

// readConsoleLine reads a line directly from the console device, bypassing
// whatever stdin the process was started with. This is reliable on Windows
// PowerShell where os.Stdin may not behave as a tty.
func readConsoleLine() (string, error) {
	// Try the console device first so pasting works on Windows PowerShell.
	cons, err := openConsole()
	if err == nil {
		defer cons.Close() //nolint:errcheck
		line, readErr := bufio.NewReader(cons).ReadString('\n')
		if readErr != nil && readErr != io.EOF {
			return "", readErr
		}
		return line, nil
	}
	// Fallback to os.Stdin.
	line, readErr := bufio.NewReader(os.Stdin).ReadString('\n')
	if readErr != nil && readErr != io.EOF {
		return "", readErr
	}
	return line, nil
}

// authStore returns the file-based auth store (keychain is optional).
func authStore() authpkg.Store {
	path := store.SubDir(store.DirAuth) + "/credentials.json"
	st, err := authpkg.NewFileStore(path)
	if err != nil {
		// Return an always-error store if we can't open the file.
		return &errStore{err: err}
	}
	return st
}

// errStore is an auth.Store that always returns an error.
type errStore struct{ err error }

func (e *errStore) Set(_, _ string) error        { return e.err }
func (e *errStore) Get(_ string) (string, error) { return "", e.err }
func (e *errStore) Delete(_ string) error        { return e.err }
func (e *errStore) List() ([]string, error)      { return nil, e.err }

// marshalOAuthToken serialises an OAuthToken to a JSON string.
func marshalOAuthToken(t *authpkg.OAuthToken) (string, error) {
	data, err := json.Marshal(t)
	return string(data), err
}
