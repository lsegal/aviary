package cmd

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/transcribe"
)

// depsDetect and depsInstall are seams so tests never shell out to package
// managers or download a model.
var (
	depsDetect  = transcribe.Detect
	depsInstall = transcribe.Install
)

var depsCmd = &cobra.Command{
	Use:   "deps",
	Short: "Show or install external programs Aviary uses",
}

var depsStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show whether whisper-cli, its model, and ffmpeg are installed",
	RunE: func(cmd *cobra.Command, _ []string) error {
		printDepsStatus(cmd.OutOrStdout(), depsDetect(depsModel()))
		return nil
	},
}

var depsInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install whisper-cli and ffmpeg, and download the whisper model",
	Long: `Install what voice transcription needs: whisper.cpp's whisper-cli and
ffmpeg through Homebrew (or winget on Windows), and the configured whisper
model (default base.en), downloaded to Aviary's deps directory. ffmpeg
converts channel voice notes; the web UI's mic doesn't need it.`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		out := cmd.OutOrStdout()
		st, err := depsInstall(cmd.Context(), depsModel(), func(line string) {
			_, _ = fmt.Fprintln(out, line)
		})
		printDepsStatus(out, st)
		return err
	},
}

func init() {
	depsCmd.AddCommand(depsStatusCmd, depsInstallCmd)
	rootCmd.AddCommand(depsCmd)
}

func depsModel() string {
	var tc config.TranscriptionConfig
	if cfg, err := config.Load(cfgFile); err == nil && cfg != nil {
		tc = cfg.Transcription
	}
	return transcribe.SettingsFrom(tc).Model
}

func printDepsStatus(w io.Writer, st transcribe.Status) {
	line := func(name string, ok bool, okText, missingText, path string) {
		state := missingText
		if ok {
			state = okText
		}
		if path != "" {
			state += "  " + path
		}
		_, _ = fmt.Fprintf(w, "  %-12s %s\n", name, state)
	}
	_, _ = fmt.Fprintln(w, "Transcription")
	line("whisper-cli", st.Whisper.Installed, "installed", "missing", st.Whisper.Path)
	line("model", st.Model.Downloaded, st.Model.Name+" downloaded", st.Model.Name+" missing", st.Model.Path)
	line("ffmpeg", st.FFmpeg.Installed, "installed", "missing", st.FFmpeg.Path)
	if !st.Ready() {
		_, _ = fmt.Fprintln(w, "Run `aviary deps install` to install what is missing.")
	}
}
