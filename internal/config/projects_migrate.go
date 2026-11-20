package config

import (
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/lsegal/aviary/internal/cliagent"
	"github.com/lsegal/aviary/internal/clidefs"
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
// project is attached to it. Projects that had no list of their own, in a
// section without one, keep the spec they used to default to. It reports whether anything changed.
func migrateProjectAgents(cfg *Config) bool {
	p := &cfg.Projects
	changed := false
	unattached := slices.ContainsFunc(p.List, func(project ProjectConfig) bool {
		return strings.TrimSpace(project.Agent) == "" && len(project.LegacyAgents) == 0
	})
	if p.Agent == "" && (len(p.LegacyAgents) > 0 || unattached) {
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

// ProjectJobModel returns the cli/ model that selects a coding-CLI spec. A
// spec that names no model gets its CLI's default, since a cli/ model always
// names one.
func ProjectJobModel(spec string) string {
	spec = strings.TrimSpace(spec)
	name, level, hasLevel := strings.Cut(spec, ":")
	if !strings.Contains(name, "/") {
		if registry, err := clidefs.Builtin(); err == nil {
			if definition, ok := registry.Lookup(name); ok {
				if model := definition.ModelOrDefault(""); model != "" {
					spec = name + "/" + model
					if hasLevel {
						spec += ":" + level
					}
				}
			}
		}
	}
	return cliagent.ModelPrefix + spec
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

// MigrateProjectAgents is the migration Load runs, for callers that change
// the projects: section in memory: it attaches projects added without an
// agent to the section's, creating that agent when there is none.
func MigrateProjectAgents(cfg *Config) bool { return migrateProjectAgents(cfg) }

// ProjectJobSpecs returns the coding-CLI specs, in fallback order, that jobs
// of a project attached to the named agent run on: the agent's model and
// fallbacks, after models.defaults, each cli/ model as its spec. A model
// that is not a CLI runs the job on the agent itself, as aviary/<agent>. An
// empty name, a project attached to no agent, takes the CLI models of
// models.defaults, as an agent without a model would. It returns nil when
// the named agent is not configured.
func ProjectJobSpecs(cfg *Config, agentName string) []string {
	agentName = strings.TrimSpace(agentName)
	var agent AgentConfig
	if agentName != "" {
		index := slices.IndexFunc(cfg.Agents, func(a AgentConfig) bool { return a.Name == agentName })
		if index < 0 {
			return nil
		}
		agent = cfg.Agents[index]
	}
	var specs []string
	add := func(spec string) {
		if spec != "" && !slices.Contains(specs, spec) {
			specs = append(specs, spec)
		}
	}
	for _, model := range append([]string{EffectiveAgentModel(agent, cfg.Models)}, EffectiveAgentFallbacks(agent, cfg.Models)...) {
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}
		if spec, ok := strings.CutPrefix(model, cliagent.ModelPrefix); ok {
			add(strings.TrimSpace(spec))
		} else if agent.Name != "" {
			add(aviarySpecPrefix + agent.Name)
		}
	}
	return specs
}
