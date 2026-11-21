package clidefs

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
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
//
// Claude, for another, ignores the permission allow entries of a workspace it
// has not recorded as trusted in .claude.json, even under -p, and a run's
// fresh clone never is. Trust names where the overlay's copy records that, so
// a launch's workspace is trusted there without touching the user's file.
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
	// Outside maps a Private file that lives outside the directory while Env
	// is unset to where it lives instead, with a leading ~/ standing for the
	// user's home directory. Claude keeps .claude.json in the home directory
	// beside ~/.claude, and inside CLAUDE_CONFIG_DIR once that is set.
	Outside map[string]string `json:"outside,omitempty"`
	// StorageEnv is a variable set to the user's own Env value, empty when it
	// is unset, for a CLI that keys stored sign-in to its directory: Claude
	// names its macOS keychain entry after CLAUDE_CONFIG_DIR unless
	// CLAUDE_SECURESTORAGE_CONFIG_DIR says otherwise.
	StorageEnv string `json:"storageEnv,omitempty"`
	// Trust records each launch's workspace as trusted in the overlay.
	Trust *WorkspaceTrust `json:"trust,omitempty"`
}

// WorkspaceTrust is where a CLI records the workspaces it trusts: Path, a
// chain of keys into the JSON object in File, holds true for a trusted
// workspace. A "{dir}" key stands for the workspace's absolute path, written
// with forward slashes.
type WorkspaceTrust struct {
	File string   `json:"file"`
	Path []string `json:"path"`
}

// trustedFile, kept in the overlay, lists every workspace a launch has
// trusted, so a later launch's refresh of the Private copy does not untrust
// the workspace of a run still going.
const trustedFile = ".aviary-trusted.json"

// overlayMu serializes overlay preparation, so concurrent launches neither
// race refreshing a copy nor lose each other's trusted workspaces.
var overlayMu sync.Mutex

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
	for name, path := range c.Outside {
		if !slices.Contains(c.Private, name) {
			return fmt.Errorf(`field "configHome.outside": %q is not one of "configHome.private"`, name)
		}
		if strings.TrimSpace(path) == "" {
			return fmt.Errorf(`field "configHome.outside.%s" is empty`, name)
		}
	}
	if c.StorageEnv != "" && (strings.TrimSpace(c.StorageEnv) != c.StorageEnv || strings.Contains(c.StorageEnv, "=")) {
		return fmt.Errorf(`field "configHome.storageEnv": %q is not a usable variable name`, c.StorageEnv)
	}
	if t := c.Trust; t != nil {
		if !slices.Contains(c.Private, t.File) {
			return fmt.Errorf(`field "configHome.trust.file": %q is not one of "configHome.private"`, t.File)
		}
		if !slices.Contains(t.Path, "{dir}") {
			return fmt.Errorf(`field "configHome.trust.path" must contain "{dir}"`)
		}
	}
	return nil
}

// LaunchEnv is the environment a launch of the CLI adds to aviary's own: the
// definition's Env plus HomeEnv. When the overlay cannot be prepared, the
// error says why and the definition's Env is still returned, so the caller
// can launch against the user's directory instead. Workspaces are the
// directories the launch runs in, which the overlay records as trusted when
// the ConfigHome declares Trust.
func (d Definition) LaunchEnv(base string, workspaces ...string) ([]string, error) {
	home, err := d.HomeEnv(base, workspaces...)
	return append(d.EnvList(), home...), err
}

// HomeEnv points the CLI at its overlay under base when the definition
// declares a ConfigHome, and is empty otherwise.
func (d Definition) HomeEnv(base string, workspaces ...string) ([]string, error) {
	if d.ConfigHome == nil {
		return nil, nil
	}
	dir, err := d.ConfigHome.Overlay(base, workspaces...)
	if err != nil {
		return nil, err
	}
	env := []string{d.ConfigHome.Env + "=" + dir}
	if d.ConfigHome.StorageEnv != "" {
		env = append(env, d.ConfigHome.StorageEnv+"="+os.Getenv(d.ConfigHome.Env))
	}
	return env, nil
}

// Home is the user's directory: Env when set, otherwise Default.
func (c ConfigHome) Home() (string, error) {
	if dir := os.Getenv(c.Env); dir != "" {
		return filepath.Abs(dir)
	}
	return expandHome(c.Default)
}

// source is where the user's own copy of the Private file name lives.
func (c ConfigHome) source(home, name string) (string, error) {
	if outside, ok := c.Outside[name]; ok && os.Getenv(c.Env) == "" {
		return expandHome(outside)
	}
	return filepath.Join(home, name), nil
}

// expandHome makes path absolute, reading a leading ~/ as the user's home
// directory.
func expandHome(path string) (string, error) {
	if rest, ok := strings.CutPrefix(path, "~/"); ok {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, filepath.FromSlash(rest)), nil
	}
	return filepath.Abs(path)
}

// Overlay prepares the overlay of the user's directory under base and returns
// its path. The overlay is kept between launches rather than made fresh for
// each: a CLI can record absolute paths through it, as codex does for every
// session's rollout file, and those have to keep resolving for the session
// to be resumed. Each call refreshes it: the Private files are copied again,
// so edits the user made since are seen and the CLI's own writes from earlier
// launches are dropped; an entry the CLI created in the overlay itself is
// moved into the user's directory when that has none of the same name; and
// every other entry of the user's directory is linked. Then each workspace
// given, along with those of earlier launches that still exist, is recorded
// as trusted in the copy Trust names.
func (c ConfigHome) Overlay(base string, workspaces ...string) (string, error) {
	overlayMu.Lock()
	defer overlayMu.Unlock()
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
		if private[name] || strings.HasPrefix(name, ".aviary-") {
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
		src, err := c.source(home, name)
		if err != nil {
			return "", fmt.Errorf("preparing the %s overlay: %w", c.Env, err)
		}
		if err := refreshCopy(src, filepath.Join(dir, name)); err != nil {
			return "", fmt.Errorf("preparing the %s overlay: %w", c.Env, err)
		}
	}
	if c.Trust != nil {
		if err := c.Trust.record(dir, workspaces); err != nil {
			return "", fmt.Errorf("trusting the workspace in the %s overlay: %w", c.Env, err)
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

// record marks the workspaces, and those an earlier launch trusted that still
// exist, as trusted in the overlay's copy of t.File.
func (t WorkspaceTrust) record(overlay string, workspaces []string) error {
	listPath := filepath.Join(overlay, trustedFile)
	var trusted []string
	if data, err := os.ReadFile(listPath); err == nil {
		_ = json.Unmarshal(data, &trusted)
	}
	for _, workspace := range workspaces {
		trusted = append(trusted, workspaceKeys(workspace)...)
	}
	trusted = slices.DeleteFunc(trusted, func(key string) bool {
		info, err := os.Stat(filepath.FromSlash(key))
		return err != nil || !info.IsDir()
	})
	slices.Sort(trusted)
	trusted = slices.Compact(trusted)
	if len(trusted) == 0 {
		if err := os.Remove(listPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	list, err := json.Marshal(trusted)
	if err != nil {
		return err
	}
	if err := writeReplacing(listPath, list); err != nil {
		return err
	}

	path := filepath.Join(overlay, t.File)
	settings := map[string]any{}
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if len(bytes.TrimSpace(data)) > 0 {
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.UseNumber()
		if err := decoder.Decode(&settings); err != nil {
			return fmt.Errorf("reading %s: %w", t.File, err)
		}
	}
	for _, key := range trusted {
		setPath(settings, t.Path, key)
	}
	data, err = json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return writeReplacing(path, data)
}

// workspaceKeys spells workspace the ways a CLI may key it: its absolute
// path, that path with links resolved, and the root of the git repository it
// is in, since Claude keys trust by repository root.
func workspaceKeys(workspace string) []string {
	if workspace == "" {
		return nil
	}
	abs, err := filepath.Abs(workspace)
	if err != nil {
		return nil
	}
	paths := []string{abs}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil && resolved != abs {
		paths = append(paths, resolved)
	}
	for _, path := range slices.Clone(paths) {
		for dir := path; ; dir = filepath.Dir(dir) {
			if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
				paths = append(paths, dir)
				break
			}
			if filepath.Dir(dir) == dir {
				break
			}
		}
	}
	keys := make([]string, len(paths))
	for i, path := range paths {
		keys[i] = filepath.ToSlash(path)
	}
	return keys
}

// setPath sets true at the chain of keys in object, with "{dir}" standing for
// dir, creating the objects along the way and keeping what they already hold.
func setPath(object map[string]any, path []string, dir string) {
	for i, key := range path {
		if key == "{dir}" {
			key = dir
		}
		if i == len(path)-1 {
			object[key] = true
			return
		}
		next, ok := object[key].(map[string]any)
		if !ok {
			next = map[string]any{}
			object[key] = next
		}
		object = next
	}
}

// writeReplacing writes data to path through a renamed temporary file, so a
// CLI already running against the overlay reads either the old file or the
// new one.
func writeReplacing(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".aviary-"+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	_, err = tmp.Write(data)
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
	}
	return err
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
