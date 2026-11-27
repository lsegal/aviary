package cmd

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lsegal/aviary/internal/browser"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/server"
	"github.com/lsegal/aviary/internal/store"
)

// watchDirPrefix starts the name of every temporary directory a watch
// session runs in, followed by the owning process's PID, so a later session
// can tell an abandoned one from a live one.
const watchDirPrefix = "aviary-watch-"

// watchAgentName is the agent a watch session's targets run under when no
// agent of the user's own owns them.
const watchAgentName = "watch"

// defaultWatchPort is where a watch session's server looks for a free port
// first.
const defaultWatchPort = 16677

// watchSession is one isolated `aviary watch` run: its own config and data
// in a temporary directory, sharing only the user's browser profile and
// provider credentials, which it reads in place.
type watchSession struct {
	// Dir is the temporary directory holding the session's config and data.
	Dir string
	// ConfigPath is the session's aviary.yaml inside Dir.
	ConfigPath string
	// Port is the port the session's server listens on.
	Port int
	// Agent is the agent the session's targets run under.
	Agent string
	// env holds the values the session's environment variables had before
	// it set them, restored when it ends.
	env map[string]*string
	// ended makes ending the session happen once.
	ended sync.Once
}

// setEnv sets an environment variable for the session, remembering the
// value it replaces.
func (s *watchSession) setEnv(key, value string) error {
	if s.env == nil {
		s.env = map[string]*string{}
	}
	if _, saved := s.env[key]; !saved {
		if old, ok := os.LookupEnv(key); ok {
			s.env[key] = &old
		} else {
			s.env[key] = nil
		}
	}
	return os.Setenv(key, value)
}

// globalWatchSources are the user's own locations a watch session reads
// from, resolved before the session points Aviary at its temporary
// directory.
type globalWatchSources struct {
	configPath string
	authDir    string
	profileDir string
}

// resolveGlobalWatchSources finds the user's config, provider credentials
// and browser profile. It only reads.
func resolveGlobalWatchSources(configFlag string) (globalWatchSources, *config.Config, error) {
	path := strings.TrimSpace(configFlag)
	if path == "" {
		path = config.DefaultPath()
	}
	cfg, err := config.Load(path)
	if err != nil {
		return globalWatchSources{}, nil, fmt.Errorf("reading your Aviary config: %w", err)
	}
	profile := strings.TrimSpace(cfg.Browser.ProfileDir)
	if profile == "" {
		profile = browser.DefaultProfileDir()
	}
	return globalWatchSources{
		configPath: path,
		authDir:    store.SubDir(store.DirAuth),
		profileDir: profile,
	}, cfg, nil
}

// sweepStaleWatchDirs removes the temporary directories under parent that
// earlier watch sessions left behind when they were killed or crashed: those
// whose owning PID is no longer alive. Directories of live sessions stay.
func sweepStaleWatchDirs(parent string) []string {
	entries, err := os.ReadDir(parent)
	if err != nil {
		return nil
	}
	var removed []string
	for _, entry := range entries {
		pid, ok := watchDirPID(entry.Name())
		if !ok || !entry.IsDir() || pid == os.Getpid() || server.ProcessAlive(pid) {
			continue
		}
		dir := filepath.Join(parent, entry.Name())
		if err := os.RemoveAll(dir); err == nil {
			removed = append(removed, dir)
		}
	}
	return removed
}

// watchDirPID reads the owning PID from a watch session directory's name,
// aviary-watch-<pid>-<random>.
func watchDirPID(name string) (int, bool) {
	rest, ok := strings.CutPrefix(name, watchDirPrefix)
	if !ok {
		return 0, false
	}
	digits, _, ok := strings.Cut(rest, "-")
	if !ok {
		return 0, false
	}
	pid, err := strconv.Atoi(digits)
	if err != nil || pid <= 0 {
		return 0, false
	}
	return pid, true
}

// newWatchSessionDir creates the session's temporary directory under parent.
func newWatchSessionDir(parent string) (string, error) {
	return os.MkdirTemp(parent, fmt.Sprintf("%s%d-", watchDirPrefix, os.Getpid()))
}

// freeWatchPort returns the first port from start up that nothing listens
// on. The session binds 127.0.0.1, but a server on the IPv6 loopback or
// every interface, as your own Aviary may be, holds the port for
// "localhost" too, so both loopbacks have to be free.
func freeWatchPort(start int) (int, error) {
	if start <= 0 {
		start = defaultWatchPort
	}
	for port := start; port < start+100 && port <= 65535; port++ {
		if watchPortFree(port) {
			return port, nil
		}
	}
	return 0, fmt.Errorf("no free port found in %d-%d", start, start+99)
}

// watchPortFree reports whether nothing answers on port on either loopback
// and the session can bind it on 127.0.0.1. Binding alone is not enough:
// listeners set SO_REUSEADDR, which on BSDs lets a specific address bind
// beside a server on every interface.
func watchPortFree(port int) bool {
	for _, host := range []string{"127.0.0.1", "::1"} {
		conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), 200*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return false
		}
	}
	ln, err := net.Listen("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}

// watchAgentChoice is the agent a watch session's targets run under, from
// --agent, and the --model and --fallback models that override the jobs'
// models.
type watchAgentChoice struct {
	Name      string
	Model     string
	Fallbacks []string
}

// watchSessionAgent picks the agent of the user's config the targets run
// under: the --agent one, else the one agent that owns them. It returns a
// copy holding only what jobs run with, so none of the agent's channels,
// tasks or other projects come along, or a model-less "watch" agent that
// falls back to models.defaults when no agent owns the targets.
func watchSessionAgent(global *config.Config, name string, targets []string) (config.AgentConfig, error) {
	pick := func(agent config.AgentConfig) config.AgentConfig {
		return config.AgentConfig{
			Name:        agent.Name,
			Model:       agent.Model,
			Fallbacks:   slices.Clone(agent.Fallbacks),
			Permissions: agent.Permissions,
		}
	}
	if name = strings.TrimSpace(name); name != "" {
		for _, agent := range global.Agents {
			if agent.Name == name {
				return pick(agent), nil
			}
		}
		return config.AgentConfig{}, fmt.Errorf("--agent %q is not an agent in your Aviary config", name)
	}
	var owner *config.AgentConfig
	for i, agent := range global.Agents {
		owned := agent.Projects.Targets()
		if !slices.ContainsFunc(targets, func(t string) bool { return slices.Contains(owned, t) }) {
			continue
		}
		if owner != nil && owner.Name != agent.Name {
			// Targets owned by different agents run under models.defaults.
			owner = nil
			break
		}
		owner = &global.Agents[i]
	}
	if owner != nil {
		return pick(*owner), nil
	}
	return config.AgentConfig{Name: watchAgentName}, nil
}

// watchSessionParams are the inputs a watch session is built from: the
// user's global config and shared sources, the chosen agent, the targets to
// watch with their mode, and the flags' Code Loop overrides.
type watchSessionParams struct {
	global   *config.Config
	sources  globalWatchSources
	choice   watchAgentChoice
	targets  []string
	mode     string
	settings config.ProjectsConfig
}

// watchSessionConfig builds the session's own config from the user's: their
// providers, browser, approval and Code Loop settings with the flags'
// overrides, one agent owning the targets, and a server on port that needs
// no token and serves plain HTTP on the loopback interface only.
func watchSessionConfig(p watchSessionParams, agent config.AgentConfig, port int) *config.Config {
	global := p.global
	cfg := &config.Config{
		Server: config.ServerConfig{
			Port:             port,
			NoTLS:            true,
			DisableTokenAuth: true,
			AutoUpdate:       new(bool),
		},
		Models:       global.Models,
		Browser:      global.Browser,
		Search:       global.Search,
		Approval:     global.Approval,
		CodingSkills: global.CodingSkills,
		Projects:     global.Projects.Override(p.settings),
	}
	cfg.Browser.ProfileDir = p.sources.profileDir
	for _, target := range p.targets {
		agent.Projects = append(agent.Projects, config.ProjectConfig{Target: target, Mode: p.mode})
	}
	cfg.Agents = []config.AgentConfig{agent}
	return cfg
}

// startWatchSession creates the session's directory, points Aviary's config,
// data and auth lookups at it and the shared credentials, and writes its
// config. The caller removes s.Dir when the session ends.
func startWatchSession(parent string, p watchSessionParams) (*watchSession, error) {
	global, choice := p.global, p.choice
	agent, err := watchSessionAgent(global, choice.Name, p.targets)
	if err != nil {
		return nil, err
	}
	// --model and --fallback are the session's job model override, laid
	// over projects.model and projects.fallbacks, never the agent's own.
	if choice.Model != "" {
		p.settings.Model = choice.Model
	}
	if len(choice.Fallbacks) > 0 {
		p.settings.Fallbacks = slices.Clone(choice.Fallbacks)
	}
	if err := config.ValidateProjectModels(global.Models, global.Projects.Override(p.settings)); err != nil {
		return nil, err
	}
	start := global.Server.Port
	if start == 0 {
		start = defaultWatchPort
	}
	port, err := freeWatchPort(start)
	if err != nil {
		return nil, err
	}
	dir, err := newWatchSessionDir(parent)
	if err != nil {
		return nil, fmt.Errorf("creating the watch session directory: %w", err)
	}
	s := &watchSession{Dir: dir, ConfigPath: filepath.Join(dir, "aviary.yaml"), Port: port, Agent: agent.Name}
	store.SetDataDir(dir)
	store.SetAuthDir(p.sources.authDir)
	config.SetDefaultPath(s.ConfigPath)
	// Aviary commands the session's jobs run reach the session's server.
	if err := s.setEnv("AVIARY_CONFIG_BASE_DIR", dir); err != nil {
		return s, fmt.Errorf("setting the config base directory: %w", err)
	}
	if err := s.setEnv("AVIARY_PID_FILE", filepath.Join(dir, "aviary.pid")); err != nil {
		return s, fmt.Errorf("setting the PID file: %w", err)
	}
	if err := store.EnsureDirs(); err != nil {
		return s, fmt.Errorf("initializing the watch session directory: %w", err)
	}
	cfg := watchSessionConfig(p, agent, port)
	if err := config.Save(s.ConfigPath, cfg); err != nil {
		return s, fmt.Errorf("writing the watch session config: %w", err)
	}
	return s, nil
}

// watchDirRemoveTimeout is how long ending a session keeps retrying to
// remove its directory. Windows refuses to remove a file something still
// has open, and an antivirus scanner or the indexer can hold one briefly
// after the session closed it.
var watchDirRemoveTimeout = 2 * time.Second

// removeWatchDir removes a session directory. Tests replace it.
var removeWatchDir = os.RemoveAll

// endWatchSession restores Aviary's own config and data lookups and removes
// the session's directory, once however many ways out call it. A directory
// it cannot remove gets one warning line on warn and is left for the next
// session's sweep of stale directories; it never fails the command.
func endWatchSession(s *watchSession, warn io.Writer) {
	if s == nil {
		restoreWatchLookups()
		return
	}
	s.ended.Do(func() {
		restoreWatchLookups()
		for key, old := range s.env {
			if old == nil {
				_ = os.Unsetenv(key)
			} else {
				_ = os.Setenv(key, *old)
			}
		}
		if s.Dir == "" {
			return
		}
		if err := removeWatchSessionDir(s.Dir, watchDirRemoveTimeout); err != nil {
			_, _ = fmt.Fprintf(warn, "warning: couldn't remove the watch session directory %s (%v); the next aviary watch removes it.\n", s.Dir, err)
		}
	})
}

// restoreWatchLookups points Aviary's config and data lookups back at the
// user's own.
func restoreWatchLookups() {
	store.SetDataDir(dataDir)
	store.SetAuthDir("")
	config.SetDefaultPath("")
}

// removeWatchSessionDir removes dir, retrying with a short backoff for up to
// timeout while removal fails.
func removeWatchSessionDir(dir string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	delay := 50 * time.Millisecond
	for {
		err := removeWatchDir(dir)
		if err == nil || errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if time.Now().Add(delay).After(deadline) {
			return err
		}
		time.Sleep(delay)
		delay = min(delay*2, 400*time.Millisecond)
	}
}
