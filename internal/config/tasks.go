package config

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// taskFileFrontmatter holds the YAML frontmatter fields recognised in a task
// markdown file.  All fields are optional.  When absent, the consuming code
// falls back to defaults (e.g. Name derived from the filename stem).
// The task body (below the closing "---") is always the content field:
// the prompt text for prompt tasks, the Lua source for script tasks.
type taskFileFrontmatter struct {
	Name            string `yaml:"name,omitempty"`
	Enabled         *bool  `yaml:"enabled,omitempty"`
	Type            string `yaml:"type,omitempty"`
	Schedule        string `yaml:"schedule,omitempty"`
	StartAt         string `yaml:"start_at,omitempty"`
	RunOnce         bool   `yaml:"run_once,omitempty"`
	Trigger         string `yaml:"trigger,omitempty"`
	Every           string `yaml:"every,omitempty"`
	At              string `yaml:"at,omitempty"`
	Days            string `yaml:"days,omitempty"`
	Channel         string `yaml:"channel,omitempty"`
	Entry           string `yaml:"entry,omitempty"`
	Match           string `yaml:"match,omitempty"`
	Timezone        string `yaml:"timezone,omitempty"`
	Overlap         string `yaml:"overlap,omitempty"`
	Missed          string `yaml:"missed,omitempty"`
	Watch           string `yaml:"watch,omitempty"`
	Target          string `yaml:"target,omitempty"`
	Project         string `yaml:"project,omitempty"`
	Model           string `yaml:"model,omitempty"`
	IdleTimeout     string `yaml:"idle_timeout,omitempty"`
	Precompile      *bool  `yaml:"precompile,omitempty"`
	Compiled        string `yaml:"compiled,omitempty"`
	CompiledKey     string `yaml:"compiled_key,omitempty"`
	ScriptVerified  bool   `yaml:"script_verified,omitempty"`
	ScriptFailures  int    `yaml:"script_failures,omitempty"`
	ScriptLastError string `yaml:"script_last_error,omitempty"`
	PrecompileNote  string `yaml:"precompile_note,omitempty"`
	// LegacyRunAgent and LegacyCLI read the old keys of Model (see
	// LegacyTaskModel). They are never written: saving a task moves the
	// value to model.
	LegacyRunAgent string `yaml:"run_agent,omitempty"`
	LegacyCLI      string `yaml:"cli,omitempty"`
}

// ParseMarkdownTask parses a task definition from markdown content with
// optional YAML frontmatter.  defaultName is used when the frontmatter does
// not set a name.  The markdown body (the text below the closing "---") is
// used as the task prompt.
func ParseMarkdownTask(defaultName string, data []byte) (TaskConfig, error) {
	content := string(data)
	task := TaskConfig{Name: defaultName, FromFile: true}

	if !strings.HasPrefix(content, "---\n") {
		task.Prompt = strings.TrimSpace(content)
		return task, nil
	}

	rest := strings.TrimPrefix(content, "---\n")
	idx := strings.Index(rest, "\n---\n")
	if idx < 0 {
		task.Prompt = strings.TrimSpace(content)
		return task, nil
	}

	var fm taskFileFrontmatter
	if err := yaml.Unmarshal([]byte(rest[:idx]), &fm); err != nil {
		return TaskConfig{}, fmt.Errorf("parsing task frontmatter: %w", err)
	}

	if strings.TrimSpace(fm.Name) != "" {
		task.Name = strings.TrimSpace(fm.Name)
	}
	task.Enabled = fm.Enabled
	task.Type = fm.Type
	task.Schedule = fm.Schedule
	task.StartAt = fm.StartAt
	task.RunOnce = fm.RunOnce
	task.Trigger = fm.Trigger
	task.Every = fm.Every
	task.At = fm.At
	task.Days = fm.Days
	task.Channel = fm.Channel
	task.Entry = fm.Entry
	task.Match = fm.Match
	task.Timezone = fm.Timezone
	task.Overlap = fm.Overlap
	task.Missed = fm.Missed
	task.Watch = fm.Watch
	task.Target = fm.Target
	task.Project = fm.Project
	task.Model = fm.Model
	task.IdleTimeout = fm.IdleTimeout
	task.Precompile = fm.Precompile
	task.Compiled = fm.Compiled
	task.CompiledKey = fm.CompiledKey
	task.ScriptVerified = fm.ScriptVerified
	task.ScriptFailures = fm.ScriptFailures
	task.ScriptLastError = fm.ScriptLastError
	task.PrecompileNote = fm.PrecompileNote
	if task.Model == "" {
		task.Model = LegacyTaskModel(fm.LegacyRunAgent, fm.LegacyCLI)
	}
	task.Prompt = strings.TrimSpace(rest[idx+5:])
	return task, nil
}

// LoadMarkdownTask reads a task markdown file and parses it.  The default task
// name is derived from the filename stem (everything before the final ".md").
func LoadMarkdownTask(filename string) (TaskConfig, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return TaskConfig{}, fmt.Errorf("reading task file %s: %w", filename, err)
	}
	stem := strings.TrimSuffix(filepath.Base(filename), ".md")
	return ParseMarkdownTask(stem, data)
}

// SaveMarkdownTask writes task as a markdown file inside dir.  The filename is
// derived from task.Name.  Returns the full path of the written file.
func SaveMarkdownTask(dir string, task TaskConfig) (string, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", fmt.Errorf("creating tasks dir: %w", err)
	}

	filename := taskNameToFilename(task.Name) + ".md"
	fullPath := filepath.Join(dir, filename)

	// Build frontmatter — only include fields that are set.
	// Task content (prompt text or Lua script) always lives in the file body.
	fm := taskFileFrontmatter{
		Enabled:         task.Enabled,
		Type:            task.Type,
		Target:          task.Target,
		Schedule:        task.Schedule,
		StartAt:         task.StartAt,
		RunOnce:         task.RunOnce,
		Trigger:         task.Trigger,
		Every:           task.Every,
		At:              task.At,
		Days:            task.Days,
		Channel:         task.Channel,
		Entry:           task.Entry,
		Match:           task.Match,
		Timezone:        task.Timezone,
		Overlap:         task.Overlap,
		Missed:          task.Missed,
		Watch:           task.Watch,
		Project:         task.Project,
		Model:           task.Model,
		IdleTimeout:     task.IdleTimeout,
		Precompile:      task.Precompile,
		Compiled:        task.Compiled,
		CompiledKey:     task.CompiledKey,
		ScriptVerified:  task.ScriptVerified,
		ScriptFailures:  task.ScriptFailures,
		ScriptLastError: task.ScriptLastError,
		PrecompileNote:  task.PrecompileNote,
	}
	// Only include name in frontmatter when it differs from the filename stem.
	stem := strings.TrimSuffix(filename, ".md")
	if task.Name != stem {
		fm.Name = task.Name
	}

	fmData, err := yaml.Marshal(fm)
	if err != nil {
		return "", fmt.Errorf("marshaling task frontmatter: %w", err)
	}

	var buf bytes.Buffer
	buf.WriteString("---\n")
	buf.Write(fmData)
	buf.WriteString("---\n")
	if body := strings.TrimSpace(task.Prompt); body != "" {
		buf.WriteString("\n")
		buf.WriteString(body)
		buf.WriteString("\n")
	}

	if err := os.WriteFile(fullPath, buf.Bytes(), 0o640); err != nil {
		return "", fmt.Errorf("writing task file %s: %w", fullPath, err)
	}
	return fullPath, nil
}

// AgentTasksDir returns the tasks/ directory under the agent's Aviary data
// directory. Task definitions are always stored with the agent metadata rather
// than in the agent's working directory.
func AgentTasksDir(agentCfg AgentConfig) string {
	dir := filepath.Join(BaseDir(), "agents", agentCfg.Name)
	return filepath.Join(dir, "tasks")
}

// LoadAgentTaskFiles globs <AgentTasksDir>/*.md and returns the parsed task
// configs.  Files whose base name starts with "_" are treated as placeholders
// and skipped.  A missing tasks directory is silently ignored.
func LoadAgentTaskFiles(agentCfg AgentConfig) ([]TaskConfig, error) {
	dir := AgentTasksDir(agentCfg)
	pattern := filepath.Join(dir, "*.md")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("globbing task files %s: %w", pattern, err)
	}
	var tasks []TaskConfig
	for _, f := range files {
		if strings.HasPrefix(filepath.Base(f), "_") {
			continue
		}
		task, taskErr := LoadMarkdownTask(f)
		if taskErr != nil {
			slog.Warn("config: skipping unreadable task file", "path", f, "err", taskErr)
			continue
		}
		tasks = append(tasks, task)
	}
	return tasks, nil
}

// FindMarkdownTask returns the path of the task file in dir that defines the
// task named name.
func FindMarkdownTask(dir, name string) (string, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.md"))
	if err != nil {
		return "", err
	}
	for _, f := range files {
		if strings.HasPrefix(filepath.Base(f), "_") {
			continue
		}
		if task, err := LoadMarkdownTask(f); err == nil && task.Name == name {
			return f, nil
		}
	}
	return "", fmt.Errorf("no task file defines %q in %s", name, dir)
}

// mergeTasksByName merges fileTasks onto base: file tasks override base tasks
// with the same name; new names are appended.
func mergeTasksByName(base []TaskConfig, fileTasks []TaskConfig) []TaskConfig {
	result := make([]TaskConfig, 0, len(base)+len(fileTasks))
	nameIdx := map[string]int{}
	for _, t := range base {
		nameIdx[t.Name] = len(result)
		result = append(result, t)
	}
	for _, ft := range fileTasks {
		if idx, ok := nameIdx[ft.Name]; ok {
			result[idx] = ft
		} else {
			result = append(result, ft)
		}
	}
	return result
}

// ValidateTaskName rejects task names that task lookups cannot resolve:
// "/" separates the agent from the task in task IDs (<agent>/<task>), so a
// name containing it could only ever be addressed by its full ID.
func ValidateTaskName(name string) error {
	if strings.Contains(name, "/") {
		return fmt.Errorf("task name %q cannot contain \"/\", which separates the agent from the task in task IDs", name)
	}
	return nil
}

// taskNameToFilename converts a task name to a safe filename component by
// lower-casing and replacing whitespace and path-unsafe characters with hyphens.
func taskNameToFilename(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	var b strings.Builder
	prevHyphen := false
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
			prevHyphen = false
		default:
			if !prevHyphen {
				b.WriteRune('-')
			}
			prevHyphen = true
		}
	}
	result := strings.Trim(b.String(), "-")
	if result == "" {
		return "task"
	}
	return result
}
