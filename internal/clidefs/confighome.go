package clidefs

import (
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
// aviary launches such a CLI with Env pointing at a fresh overlay of the
// user's directory: each Private file is a copy, so the CLI reads the user's
// settings but its writes stay in the overlay, and every other entry links
// back to the user's, so sign-in, token refreshes and sessions are shared.
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
// definition's Env, plus the overlay directory when it declares a
// ConfigHome. cleanup removes the overlay once the process has exited and
// is never nil. When the overlay cannot be built, the error says why and env
// still carries the definition's Env, so the caller can launch against the
// user's directory instead.
func (d Definition) LaunchEnv() (env []string, cleanup func(), err error) {
	env = d.EnvList()
	if d.ConfigHome == nil {
		return env, func() {}, nil
	}
	dir, cleanup, err := d.ConfigHome.Overlay()
	if err != nil {
		return env, cleanup, err
	}
	return append(env, d.ConfigHome.Env+"="+dir), cleanup, nil
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

// Overlay builds a fresh overlay of the user's directory and returns its
// path. cleanup is never nil; it moves anything the CLI created at the top of
// the overlay, other than the Private files, into the user's directory when
// that has no entry of the same name, so a first run's sessions are kept,
// and then removes the overlay.
func (c ConfigHome) Overlay() (dir string, cleanup func(), err error) {
	cleanup = func() {}
	home, err := c.Home()
	if err != nil {
		return "", cleanup, fmt.Errorf("locating the %s directory: %w", c.Env, err)
	}
	if err := os.MkdirAll(home, 0o700); err != nil {
		return "", cleanup, err
	}
	entries, err := os.ReadDir(home)
	if err != nil {
		return "", cleanup, err
	}
	dir, err = os.MkdirTemp("", "aviary-"+strings.ToLower(c.Env)+"-*")
	if err != nil {
		return "", cleanup, err
	}
	remove := func() { _ = os.RemoveAll(dir) }
	private := make(map[string]bool, len(c.Private))
	for _, name := range c.Private {
		private[name] = true
	}
	linked := make(map[string]bool, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		src, dst := filepath.Join(home, name), filepath.Join(dir, name)
		if private[name] {
			err = copyFile(src, dst)
		} else {
			err = os.Symlink(src, dst)
			linked[name] = true
		}
		if err != nil {
			remove()
			return "", cleanup, fmt.Errorf("building the %s overlay: %w", c.Env, err)
		}
	}
	cleanup = func() {
		adopted, _ := os.ReadDir(dir)
		for _, entry := range adopted {
			name := entry.Name()
			if private[name] || linked[name] {
				continue
			}
			target := filepath.Join(home, name)
			if _, err := os.Lstat(target); errors.Is(err, os.ErrNotExist) {
				_ = os.Rename(filepath.Join(dir, name), target)
			}
		}
		remove()
	}
	return dir, cleanup, nil
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
