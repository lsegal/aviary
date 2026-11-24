package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/server"
)

var (
	certRegenerateFlag bool
	certJSONFlag       bool
)

var certCmd = &cobra.Command{
	Use:   "cert",
	Short: "Show or regenerate the server's TLS certificate",
	Long: `Print the paths of the TLS certificate and key the Aviary server serves with.

Without server.tls.cert and server.tls.key configured, this is a self-signed
cert covering localhost, 127.0.0.1, ::1, the hostname, the current LAN IPv4
addresses, and server.tls.sans. It is generated when missing and regenerated
when it no longer covers those names, such as after the LAN address changes.
Run with --regenerate to replace it with a fresh one. Restart the server to
serve a regenerated cert.`,
	RunE: func(_ *cobra.Command, _ []string) error {
		cfg, err := config.Load(cfgFile)
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}
		certFile, keyFile, err := server.TLSFiles(config.EffectiveServerTLS(cfg.Server), certRegenerateFlag)
		if err != nil {
			return fmt.Errorf("loading TLS cert: %w", err)
		}
		if certJSONFlag {
			return json.NewEncoder(rootCmd.OutOrStdout()).Encode(map[string]string{"cert": certFile, "key": keyFile, "server_url": config.ServerURL(cfg.Server)})
		}
		_, _ = fmt.Fprintf(rootCmd.OutOrStdout(), "cert: %s\nkey:  %s\n", certFile, keyFile)
		return nil
	},
}

func init() {
	certCmd.Flags().BoolVar(&certRegenerateFlag, "regenerate", false, "Replace the self-signed cert with a freshly generated one")
	certCmd.Flags().BoolVar(&certJSONFlag, "json", false, "Print the paths as JSON")
	rootCmd.AddCommand(certCmd)
}
