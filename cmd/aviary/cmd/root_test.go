package cmd

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// TestResolvedServerURLReachesServer checks the CLI talks to the local server
// with the scheme and port it serves, with TLS on and off.
func TestResolvedServerURLReachesServer(t *testing.T) {
	t.Setenv("AVIARY_CONFIG_SERVER_NO_TLS", "")
	for _, noTLS := range []bool{false, true} {
		t.Run(fmt.Sprintf("no_tls=%v", noTLS), func(t *testing.T) {
			srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			}))
			if noTLS {
				srv.Start()
			} else {
				srv.StartTLS()
			}
			t.Cleanup(srv.Close)
			_, port, err := net.SplitHostPort(srv.Listener.Addr().String())
			if err != nil {
				t.Fatal(err)
			}

			cfgPath := filepath.Join(t.TempDir(), "aviary.yaml")
			body := fmt.Sprintf("server:\n  port: %s\n  no_tls: %v\n", port, noTLS)
			if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			prevCfg, prevServer := cfgFile, serverURL
			cfgFile, serverURL = cfgPath, ""
			t.Cleanup(func() { cfgFile, serverURL = prevCfg, prevServer })

			scheme := "https"
			if noTLS {
				scheme = "http"
			}
			url := resolvedServerURL()
			if want := scheme + "://localhost:" + port; url != want {
				t.Fatalf("resolvedServerURL() = %q, want %q", url, want)
			}
			client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}} //nolint:gosec
			resp, err := client.Get(url + "/")                                                                         //nolint:noctx
			if err != nil {
				t.Fatalf("GET %s: %v", url, err)
			}
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusNoContent {
				t.Fatalf("GET %s = %d", url, resp.StatusCode)
			}
		})
	}
}

func TestResolvedServerURLPrefersFlag(t *testing.T) {
	prev := serverURL
	serverURL = "https://aviary.lan:9443"
	t.Cleanup(func() { serverURL = prev })
	if got := resolvedServerURL(); got != "https://aviary.lan:9443" {
		t.Fatalf("resolvedServerURL() = %q", got)
	}
}
