package transcribe

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/lsegal/aviary/internal/store"
)

// Binary is the state of one external program.
type Binary struct {
	Name      string `json:"name"`
	Installed bool   `json:"installed"`
	Path      string `json:"path,omitempty"`
}

// Model is the state of the whisper model file.
type Model struct {
	Name       string `json:"name"`
	Downloaded bool   `json:"downloaded"`
	Path       string `json:"path"`
}

// Status reports what local transcription needs and whether it is present.
type Status struct {
	Whisper Binary `json:"whisper"`
	FFmpeg  Binary `json:"ffmpeg"`
	Model   Model  `json:"model"`
}

// Missing lists what is not installed, in a form a user can act on.
func (s Status) Missing() []string {
	var missing []string
	if !s.Whisper.Installed {
		missing = append(missing, "whisper-cli")
	}
	if !s.Model.Downloaded {
		missing = append(missing, "whisper model "+s.Model.Name)
	}
	if !s.FFmpeg.Installed {
		missing = append(missing, "ffmpeg")
	}
	return missing
}

// Ready reports whether everything is installed.
func (s Status) Ready() bool { return len(s.Missing()) == 0 }

// whisperNames are the names whisper.cpp's CLI is installed under.
var whisperNames = []string{"whisper-cli", "whisper-cpp"}

var (
	lookPath = exec.LookPath
	// modelBaseURL serves ggml model files as ggml-<name>.bin.
	modelBaseURL = "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/"
	httpClient   = http.DefaultClient
	// packageInstall installs packages with the platform package manager.
	packageInstall = installPackages
)

// DepsDir holds programs and models Aviary installs itself.
func DepsDir() string {
	return store.SubDir("deps")
}

// ModelsDir holds downloaded whisper models.
func ModelsDir() string {
	return filepath.Join(DepsDir(), "whisper")
}

// ModelPath is where model is stored. A model given as a path to a file is
// used as is.
func ModelPath(model string) string {
	if strings.ContainsAny(model, `/\`) || strings.HasSuffix(model, ".bin") {
		return model
	}
	return filepath.Join(ModelsDir(), "ggml-"+model+".bin")
}

// Detect finds whisper-cli, ffmpeg, and model, first in Aviary's own deps
// directory and then on PATH.
func Detect(model string) Status {
	if model == "" {
		model = defaultModel()
	}
	st := Status{
		Whisper: findBinary("whisper-cli", whisperNames...),
		FFmpeg:  findBinary("ffmpeg", "ffmpeg"),
		Model:   Model{Name: model, Path: ModelPath(model)},
	}
	if info, err := os.Stat(st.Model.Path); err == nil && !info.IsDir() && info.Size() > 0 {
		st.Model.Downloaded = true
	}
	return st
}

func findBinary(display string, names ...string) Binary {
	bin := Binary{Name: display}
	for _, name := range names {
		local := filepath.Join(DepsDir(), "bin", name)
		if runtime.GOOS == "windows" {
			local += ".exe"
		}
		if info, err := os.Stat(local); err == nil && !info.IsDir() {
			bin.Path, bin.Installed = local, true
			return bin
		}
	}
	for _, name := range names {
		if path, err := lookPath(name); err == nil {
			bin.Path, bin.Installed = path, true
			return bin
		}
	}
	return bin
}

// Install installs what Detect reports missing: whisper-cli and ffmpeg
// through the platform package manager, and the model by download. logf
// receives progress lines.
func Install(ctx context.Context, model string, logf func(string)) (Status, error) {
	if logf == nil {
		logf = func(string) {}
	}
	st := Detect(model)
	var packages []string
	if !st.Whisper.Installed {
		packages = append(packages, "whisper-cpp")
	}
	if !st.FFmpeg.Installed {
		packages = append(packages, "ffmpeg")
	}
	var errs []string
	if len(packages) > 0 {
		logf("Installing " + strings.Join(packages, ", "))
		if err := packageInstall(ctx, packages, logf); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if !st.Model.Downloaded {
		if !slices.Contains(knownModels, st.Model.Name) {
			errs = append(errs, fmt.Sprintf("model %s is not at %s; download it there or choose a model such as %s", st.Model.Name, st.Model.Path, DefaultModel))
		} else {
			logf("Downloading whisper model " + st.Model.Name)
			if err := downloadModel(ctx, st.Model.Name, st.Model.Path); err != nil {
				errs = append(errs, err.Error())
			}
		}
	}
	st = Detect(model)
	if len(errs) > 0 {
		return st, fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return st, nil
}

// knownModels are the whisper.cpp models Install can download.
var knownModels = []string{
	"tiny", "tiny.en", "base", "base.en", "small", "small.en",
	"medium", "medium.en", "large-v3", "large-v3-turbo",
}

func downloadModel(ctx context.Context, name, dest string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelBaseURL+"ggml-"+name+".bin", nil)
	if err != nil {
		return err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("downloading model %s: %w", name, err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("downloading model %s: %s", name, resp.Status)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".download-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) //nolint:errcheck
	if _, err := io.Copy(tmp, resp.Body); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("downloading model %s: %w", name, err)
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), dest)
}

// installPackages uses Homebrew where it exists (macOS and Linuxbrew) and
// winget on Windows. Other systems get instructions instead.
func installPackages(ctx context.Context, packages []string, logf func(string)) error {
	if brew, err := lookPath("brew"); err == nil {
		out, err := exec.CommandContext(ctx, brew, append([]string{"install"}, packages...)...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("brew install %s failed: %w: %s", strings.Join(packages, " "), err, firstLine(out))
		}
		logf(strings.TrimSpace(string(out)))
		return nil
	}
	if winget, err := lookPath("winget"); err == nil && runtime.GOOS == "windows" {
		ids := map[string]string{"ffmpeg": "Gyan.FFmpeg", "whisper-cpp": "ggerganov.whisper.cpp"}
		for _, pkg := range packages {
			out, err := exec.CommandContext(ctx, winget, "install", "--silent", "--accept-package-agreements", "--accept-source-agreements", "--id", ids[pkg]).CombinedOutput()
			if err != nil {
				return fmt.Errorf("winget install %s failed: %w: %s", ids[pkg], err, firstLine(out))
			}
		}
		return nil
	}
	return fmt.Errorf("no supported package manager found; install %s yourself (for example from https://github.com/ggml-org/whisper.cpp and your system's ffmpeg package), or put the binaries in %s",
		strings.Join(packages, " and "), filepath.Join(DepsDir(), "bin"))
}
