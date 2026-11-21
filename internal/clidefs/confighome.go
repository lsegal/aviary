package clidefs

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ConfigHome describes a CLI that keeps its settings, sign-in and sessions in
// one directory and writes bookkeeping of its own into a settings file there.
// Codex, for one, records every workspace it runs with write access in as a
// trusted project in config.toml, and offers no way to turn that off.
//
// aviary launches such a CLI with Env pointing at an overlay of the user's
// directory that aviary keeps in its own data directory: each Private file is
// a copy, so the CLI reads the user's settings but its writes stay in the
// overlay, and every other entry links back to the user's, so sign-in, token
// refreshes and sessions are shared.
type ConfigHome struct {
	// Env is the variable the CLI reads its directory from, such as
	// CODEX_HOME.
	Env string `json:"env"`
	// Default is the directory used when Env is unset, with a leading ~/
	// standing for the user's home directory.
	Default string `json:"default"`
	// Private names the files, directly inside the directory, that each
	// launch gets its own copy of.
	Private []string `json:"private"`
}

func (c *ConfigHome) validate() error {
	if c == nil {
		return nil
	}
	if strings.TrimSpace(c.Env) == "" || strings.Contains(c.Env, "=") {
		return fmt.Errorf(`field "configHome.env": %q is not a usable variable name`, c.Env)
	}
	if strings.TrimSpace(c.Default) == "" {
		return fmt.Errorf(`field "configHome.default" is required`)
	}
	if len(c.Private) == 0 {
		return fmt.Errorf(`field "configHome.private" must name at least one file`)
	}
	for _, name := range c.Private {
		if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
			return fmt.Errorf(`field "configHome.private": %q must be a file name inside the directory`, name)
		}
	}
	return nil
}

// LaunchEnv is the environment a launch of the CLI adds to aviary's own: the
// definition's Env plus HomeEnv. When the overlay cannot be prepared, the
// error says why and the definition's Env is still returned, so the caller
// can launch against the user's directory instead.
func (d Definition) LaunchEnv(base string) ([]string, error) {
	home, err := d.HomeEnv(base)
	return append(d.EnvList(), home...), err
}

// HomeEnv points the CLI at its overlay under base when the definition
// declares a ConfigHome, and is empty otherwise.
func (d Definition) HomeEnv(base string) ([]string, error) {
	if d.ConfigHome == nil {
		return nil, nil
	}
	dir, err := d.ConfigHome.Overlay(base)
	if err != nil {
		return nil, err
	}
	return []string{d.ConfigHome.Env + "=" + dir}, nil
}

// Home is the user's directory: Env when set, otherwise Default.
func (c ConfigHome) Home() (string, error) {
	if dir := os.Getenv(c.Env); dir != "" {
		return filepath.Abs(dir)
	}
	if rest, ok := strings.CutPrefix(c.Default, "~/"); ok {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, filepath.FromSlash(rest)), nil
	}
	return filepath.Abs(c.Default)
}

// Overlay prepares the overlay of the user's directory under base and returns
// its path. The overlay is kept between launches rather than made fresh for
// each: a CLI can record absolute paths through it, as codex does for every
// session's rollout file, and those have to keep resolving for the session
// to be resumed. Each call refreshes it: the Private files are copied again,
// so edits the user made since are seen and the CLI's own writes from earlier
// launches are dropped; an entry the CLI created in the overlay itself is
// moved into the user's directory when that has none of the same name; and
// every other entry of the user's directory is linked.
func (c ConfigHome) Overlay(base string) (string, error) {
	return c.overlay(base, refreshCopy)
}

// OwnOverlay prepares an overlay under base like Overlay, except that each
// Private file is emptied rather than copied from the user's: the CLI reads
// aviary's settings only, as `codex --ignore-user-config` would, while still
// sharing the user's sign-in and sessions. Settings aviary adds for one
// launch go in files named with LaunchFilePrefix, such as a codex profile.
func (c ConfigHome) OwnOverlay(base string) (string, error) {
	return c.overlay(base, func(_, dst string) error { return replaceEmpty(dst) })
}

// OwnHomeEnv points the CLI at its OwnOverlay under base when the definition
// declares a ConfigHome, and is empty otherwise.
func (d Definition) OwnHomeEnv(base string) ([]string, error) {
	if d.ConfigHome == nil {
		return nil, nil
	}
	dir, err := d.ConfigHome.OwnOverlay(base)
	if err != nil {
		return nil, err
	}
	return []string{d.ConfigHome.Env + "=" + dir}, nil
}

// LaunchFilePrefix starts the name of a file aviary writes into an overlay
// for a single launch. Refreshing the overlay leaves such files alone.
const LaunchFilePrefix = "aviary-"

func (c ConfigHome) overlay(base string, fill func(src, dst string) error) (string, error) {
	home, err := c.Home()
	if err != nil {
		return "", fmt.Errorf("locating the %s directory: %w", c.Env, err)
	}
	sum := sha256.Sum256([]byte(home))
	dir := filepath.Join(base, strings.ToLower(c.Env)+"-"+hex.EncodeToString(sum[:6]))
	if err := os.MkdirAll(home, 0o700); err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	private := make(map[string]bool, len(c.Private))
	for _, name := range c.Private {
		private[name] = true
	}
	existing, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	for _, entry := range existing {
		name := entry.Name()
		path, target := filepath.Join(dir, name), filepath.Join(home, name)
		if private[name] || strings.HasPrefix(name, ".aviary-") || strings.HasPrefix(name, LaunchFilePrefix) {
			continue
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if _, err := os.Lstat(target); errors.Is(err, os.ErrNotExist) {
				_ = os.Remove(path) // the user removed it
			}
			continue
		}
		if _, err := os.Lstat(target); errors.Is(err, os.ErrNotExist) {
			_ = os.Rename(path, target)
		}
	}
	entries, err := os.ReadDir(home)
	if err != nil {
		return "", err
	}
	for _, name := range c.Private {
		if err := fill(filepath.Join(home, name), filepath.Join(dir, name)); err != nil {
			return "", fmt.Errorf("preparing the %s overlay: %w", c.Env, err)
		}
	}
	for _, entry := range entries {
		name := entry.Name()
		if private[name] {
			continue
		}
		path := filepath.Join(dir, name)
		if _, err := os.Lstat(path); err == nil {
			continue // linked already, or the CLI's own when both have one
		}
		if err := os.Symlink(filepath.Join(home, name), path); err != nil && !errors.Is(err, os.ErrExist) {
			return "", fmt.Errorf("preparing the %s overlay: %w", c.Env, err)
		}
	}
	return dir, nil
}

// refreshCopy replaces dst with a copy of src, or removes it when src does not
// exist. The copy is renamed into place, so a CLI already running against the
// overlay reads either the old copy or the new one.
func refreshCopy(src, dst string) error {
	if _, err := os.Stat(src); errors.Is(err, os.ErrNotExist) {
		if err := os.Remove(dst); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".aviary-"+filepath.Base(dst)+"-*")
	if err != nil {
		return err
	}
	_ = tmp.Close()
	_ = os.Remove(tmp.Name())
	if err := copyFile(src, tmp.Name()); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), dst); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return nil
}

// copyFile copies a regular file, keeping its permission bits. Anything else
// with a Private name, such as a symlink to a shared settings file, is
// copied through to its target's contents.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close() //nolint:errcheck
	info, err := in.Stat()
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a directory", src)
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// replaceEmpty replaces dst with an empty file, renamed into place like
// refreshCopy's copy.
func replaceEmpty(dst string) error {
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".aviary-"+filepath.Base(dst)+"-*")
	if err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), dst); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return nil
}
