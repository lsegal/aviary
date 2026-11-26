package cmd

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/lsegal/aviary/internal/transcribe"
)

func readyDepsStatus() transcribe.Status {
	return transcribe.Status{
		Whisper: transcribe.Binary{Name: "whisper-cli", Installed: true, Path: "/bin/whisper-cli"},
		FFmpeg:  transcribe.Binary{Name: "ffmpeg", Installed: true, Path: "/bin/ffmpeg"},
		Model:   transcribe.Model{Name: "base.en", Downloaded: true, Path: "/models/ggml-base.en.bin"},
	}
}

func TestPrintDepsStatus(t *testing.T) {
	const hint = "Run `aviary deps install` to install what is missing."
	tests := []struct {
		name   string
		mutate func(*transcribe.Status)
		want   []string
		hint   bool
	}{
		{
			name: "all installed",
			want: []string{
				"  whisper-cli  installed  /bin/whisper-cli",
				"  model        base.en downloaded  /models/ggml-base.en.bin",
				"  ffmpeg       installed  /bin/ffmpeg",
			},
		},
		{
			name:   "whisper missing",
			mutate: func(s *transcribe.Status) { s.Whisper = transcribe.Binary{Name: "whisper-cli"} },
			want:   []string{"  whisper-cli  missing\n"},
			hint:   true,
		},
		{
			name: "model missing",
			mutate: func(s *transcribe.Status) {
				s.Model.Downloaded = false
			},
			want: []string{"  model        base.en missing  /models/ggml-base.en.bin"},
			hint: true,
		},
		{
			name:   "ffmpeg missing",
			mutate: func(s *transcribe.Status) { s.FFmpeg = transcribe.Binary{Name: "ffmpeg"} },
			want:   []string{"  ffmpeg       missing\n"},
			hint:   true,
		},
		{
			name: "installed without paths",
			mutate: func(s *transcribe.Status) {
				s.Whisper.Path = ""
				s.FFmpeg.Path = ""
			},
			want: []string{"  whisper-cli  installed\n", "  ffmpeg       installed\n"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := readyDepsStatus()
			if tt.mutate != nil {
				tt.mutate(&st)
			}
			var buf bytes.Buffer
			printDepsStatus(&buf, st)
			out := buf.String()
			if !strings.HasPrefix(out, "Transcription\n") {
				t.Fatalf("output missing header:\n%s", out)
			}
			checkDepsOutputHas(t, out, tt.want)
			if got := strings.Contains(out, hint); got != tt.hint {
				t.Errorf("install hint present = %v, want %v:\n%s", got, tt.hint, out)
			}
		})
	}
}

// checkDepsOutputHas checks that out has every want.
func checkDepsOutputHas(t *testing.T, out string, wants []string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func setDepsConfig(t *testing.T, content string) {
	t.Helper()
	t.Setenv(transcribe.ModelEnv, "")
	prev := cfgFile
	t.Cleanup(func() { cfgFile = prev })
	dir := t.TempDir()
	cfgFile = filepath.Join(dir, "aviary.yaml")
	if content == "" {
		return
	}
	if err := os.WriteFile(cfgFile, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestDepsModel(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{name: "configured model", content: "transcription:\n  model: small.en\n", want: "small.en"},
		{name: "no transcription section", content: "agents: []\n", want: transcribe.DefaultModel},
		{name: "missing file", want: transcribe.DefaultModel},
		{name: "invalid file", content: "transcription: [\n", want: transcribe.DefaultModel},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setDepsConfig(t, tt.content)
			if got := depsModel(); got != tt.want {
				t.Fatalf("depsModel() = %q, want %q", got, tt.want)
			}
		})
	}
}

func stubDeps(t *testing.T) {
	t.Helper()
	prevDetect, prevInstall := depsDetect, depsInstall
	t.Cleanup(func() { depsDetect, depsInstall = prevDetect, prevInstall })
	depsDetect = func(string) transcribe.Status {
		t.Fatal("unexpected call to Detect")
		return transcribe.Status{}
	}
	depsInstall = func(context.Context, string, func(string)) (transcribe.Status, error) {
		t.Fatal("unexpected call to Install")
		return transcribe.Status{}, nil
	}
}

func runDepsCmd(t *testing.T, sub *cobra.Command) (string, error) {
	t.Helper()
	var buf bytes.Buffer
	c := &cobra.Command{}
	c.SetOut(&buf)
	c.SetContext(context.Background())
	err := sub.RunE(c, nil)
	return buf.String(), err
}

func TestDepsStatusCommand(t *testing.T) {
	setDepsConfig(t, "transcription:\n  model: tiny\n")
	stubDeps(t)
	var gotModel string
	depsDetect = func(model string) transcribe.Status {
		gotModel = model
		st := readyDepsStatus()
		st.Model.Name = model
		st.FFmpeg = transcribe.Binary{Name: "ffmpeg"}
		return st
	}

	out, err := runDepsCmd(t, depsStatusCmd)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if gotModel != "tiny" {
		t.Errorf("Detect model = %q, want tiny", gotModel)
	}
	for _, want := range []string{"Transcription\n", "tiny downloaded", "  ffmpeg       missing", "aviary deps install"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestDepsInstallCommand(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		setDepsConfig(t, "")
		stubDeps(t)
		var gotModel string
		depsInstall = func(_ context.Context, model string, logf func(string)) (transcribe.Status, error) {
			gotModel = model
			logf("installing ffmpeg")
			return readyDepsStatus(), nil
		}

		out, err := runDepsCmd(t, depsInstallCmd)
		if err != nil {
			t.Fatalf("install: %v", err)
		}
		if gotModel != transcribe.DefaultModel {
			t.Errorf("Install model = %q, want %q", gotModel, transcribe.DefaultModel)
		}
		if !strings.HasPrefix(out, "installing ffmpeg\nTranscription\n") {
			t.Errorf("progress line should precede status:\n%s", out)
		}
		if strings.Contains(out, "aviary deps install") {
			t.Errorf("ready status should not print install hint:\n%s", out)
		}
	})

	t.Run("failure still prints status", func(t *testing.T) {
		setDepsConfig(t, "")
		stubDeps(t)
		boom := errors.New("brew failed")
		depsInstall = func(context.Context, string, func(string)) (transcribe.Status, error) {
			st := readyDepsStatus()
			st.Whisper = transcribe.Binary{Name: "whisper-cli"}
			return st, boom
		}

		out, err := runDepsCmd(t, depsInstallCmd)
		if !errors.Is(err, boom) {
			t.Fatalf("install err = %v, want %v", err, boom)
		}
		for _, want := range []string{"  whisper-cli  missing", "aviary deps install"} {
			if !strings.Contains(out, want) {
				t.Errorf("output missing %q:\n%s", want, out)
			}
		}
	})
}

func TestDepsCommandsRegistered(t *testing.T) {
	got, _, err := rootCmd.Find([]string{"deps", "install"})
	if err != nil || got != depsInstallCmd {
		t.Fatalf("Find(deps install) = %v, %v", got, err)
	}
	got, _, err = rootCmd.Find([]string{"deps", "status"})
	if err != nil || got != depsStatusCmd {
		t.Fatalf("Find(deps status) = %v, %v", got, err)
	}
}
