package codingskills

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/lsegal/aviary"
)

// universalTarget is the skills.sh agent id for the global ~/.agents/skills
// directory, which Aviary and the CLIs that read .agents/skills discover.
const universalTarget = "universal"

// firstLaunchMarker is the file in the Aviary config directory whose presence
// means the first-launch install already ran.
const firstLaunchMarker = ".coding-skills-installed"

// InstallGlobal installs every skill Aviary ships globally with `npx skills
// add --global`: into ~/.agents/skills and the skills directory of every CLI
// provider on PATH. Skills already installed are refreshed in place, so
// running it again is safe. A release installs from its tag; a development
// build installs from the bundled copies.
func (m *Manager) InstallGlobal(ctx context.Context) (Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	result := Result{Installed: []string{}}
	agents := []string{universalTarget}
	for _, cli := range m.CLIs() {
		if m.Env.detected(cli.Binary) {
			agents = appendUnique(agents, cli.Target)
		}
	}
	var source func(string) string
	if version, release := m.releaseVersion(); release {
		source = func(skill string) string {
			return fmt.Sprintf("https://github.com/%s/tree/v%s/%s/%s", Repo, version, aviary.CodingSkillsRoot, skill)
		}
	} else {
		dir, cleanup, err := m.extractBundle()
		if err != nil {
			return result, err
		}
		defer cleanup()
		source = func(skill string) string {
			return filepath.Join(dir, filepath.FromSlash(aviary.CodingSkillsRoot), skill)
		}
	}
	for _, skill := range aviary.CodingSkillNames {
		runCtx, cancel := context.WithTimeout(ctx, installTimeout)
		err := m.Run(runCtx, addArgs(source(skill), agents)...)
		cancel()
		if err != nil {
			return result, fmt.Errorf("install %s: %w", skill, err)
		}
		result.Installed = append(result.Installed, skill)
	}
	return result, nil
}

// InstallOnFirstLaunch runs InstallGlobal the first time Aviary starts with
// configDir, and records that it did, so a later start skips it. A failed
// install is logged and retried on the next start.
func (m *Manager) InstallOnFirstLaunch(ctx context.Context, configDir string) {
	marker := filepath.Join(configDir, firstLaunchMarker)
	if _, err := os.Stat(marker); err == nil {
		return
	}
	result, err := m.InstallGlobal(ctx)
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			slog.Warn("coding skills: first-launch install failed; retrying on the next start (or run `aviary install-skills`)", "err", err)
		}
		return
	}
	slog.Info("coding skills: installed globally on first launch", "skills", strings.Join(result.Installed, ", "))
	if err = os.MkdirAll(configDir, 0o755); err == nil {
		err = os.WriteFile(marker, []byte(m.version()+"\n"), 0o644)
	}
	if err != nil {
		slog.Warn("coding skills: could not record the first-launch install", "err", err)
	}
}
