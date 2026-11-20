package config

import (
	"fmt"
	"log/slog"
	"slices"
	"strings"
)

// ProjectsJobsAgentName is the agent Load attaches projects to when it
// migrates the section's coding-CLI list, or the old built-in default, onto
// an agent.
const ProjectsJobsAgentName = "projects-jobs"

// legacyProjectsSpec is the coding-CLI spec projects ran on before they were
// attached to agents, when nothing else was configured.
const legacyProjectsSpec = "codex"

// aviarySpecPrefix marks a coding-CLI spec that ran jobs on an Aviary agent.
const aviarySpecPrefix = "aviary/"

// migrateProjectAgents moves the coding-CLI spec lists projects carried
// before they were attached to agents onto agents: each distinct list becomes
// an agent whose model and fallbacks are those specs as cli/ models, and the
// project is attached to it. A configured section without a list keeps the
// spec it used to default to. It reports whether anything changed.
func migrateProjectAgents(cfg *Config) bool {
	p := &cfg.Projects
	changed := false
	if p.Agent == "" && (len(p.LegacyAgents) > 0 || len(p.List) > 0) {
		specs := p.LegacyAgents
		if len(specs) == 0 {
			specs = []string{legacyProjectsSpec}
		}
		p.Agent = attachSpecs(cfg, specs, ProjectsJobsAgentName)
		changed = true
	}
	if len(p.LegacyAgents) > 0 {
		p.LegacyAgents = nil
		changed = true
	}
	for i := range p.List {
		project := &p.List[i]
		if len(project.LegacyAgents) == 0 {
			continue
		}
		if project.Agent == "" {
			project.Agent = attachSpecs(cfg, project.LegacyAgents, projectAgentName(project.Target))
		}
		project.LegacyAgents = nil
		changed = true
	}
	return changed
}

// attachSpecs returns the agent that runs specs: the Aviary agent an
// aviary/<agent> first spec named, an existing agent with the same models,
// or a new agent named after name.
func attachSpecs(cfg *Config, specs []string, name string) string {
	var models []string
	for _, spec := range specs {
		spec = strings.TrimSpace(spec)
		if spec == "" {
			continue
		}
		if agent, ok := strings.CutPrefix(spec, aviarySpecPrefix); ok {
			if len(models) == 0 {
				if len(specs) > 1 {
					slog.Warn("config: projects ran on an Aviary agent with fallbacks; attaching the project to the agent without them", "agent", agent, "specs", specs)
				}
				return agent
			}
			slog.Warn("config: dropping an Aviary agent fallback from a project's coding-CLI list", "spec", spec)
			continue
		}
		models = append(models, ProjectJobModel(spec))
	}
	if len(models) == 0 {
		models = []string{ProjectJobModel(legacyProjectsSpec)}
	}
	for _, agent := range cfg.Agents {
		if agent.Model == models[0] && slices.Equal(agent.Fallbacks, models[1:]) {
			return agent.Name
		}
	}
	name = uniqueAgentName(cfg, name)
	agent := AgentConfig{Name: name, Model: models[0]}
	if len(models) > 1 {
		agent.Fallbacks = slices.Clone(models[1:])
	}
	cfg.Agents = append(cfg.Agents, agent)
	return name
}

// ProjectJobModel returns the cli/ model that selects a coding-CLI spec.
func ProjectJobModel(spec string) string {
	return "cli/" + strings.TrimSpace(spec)
}

// projectAgentName names the agent migrated from a project's own list.
func projectAgentName(target string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(target)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			if s := b.String(); s != "" && !strings.HasSuffix(s, "-") {
				b.WriteByte('-')
			}
		}
	}
	slug := strings.Trim(b.String(), "-")
	slug = strings.TrimPrefix(slug, "https-github-com-")
	if slug == "" {
		return ProjectsJobsAgentName
	}
	return "project-" + slug
}

func uniqueAgentName(cfg *Config, name string) string {
	taken := func(candidate string) bool {
		return candidate == ProjectsAgentName || slices.ContainsFunc(cfg.Agents, func(a AgentConfig) bool { return a.Name == candidate })
	}
	if !taken(name) {
		return name
	}
	for i := 2; ; i++ {
		if candidate := fmt.Sprintf("%s-%d", name, i); !taken(candidate) {
			return candidate
		}
	}
}
