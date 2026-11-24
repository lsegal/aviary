package config

import (
	"fmt"
	"os/exec"
	"slices"
	"strings"

	"github.com/lsegal/aviary/internal/cliagent"
	"github.com/lsegal/aviary/internal/clidefs"
	"github.com/lsegal/aviary/internal/llm"
)

// aviarySpecPrefix marks a model spec that runs a job on an Aviary agent.
const aviarySpecPrefix = "aviary/"

// ProjectJobSpecs returns the model specs, in fallback order, that jobs
// of a project owned by the named agent run on: projects.model and
// projects.fallbacks when model is set, else the agent's model and
// fallbacks, after models.defaults, each CLI provider model as its spec:
// cli/claude/opus and claude/opus are both the spec claude/opus. A model
// that is not a CLI runs the job on the agent itself, as aviary/<agent>. An
// empty name, a runtime watch no agent owns, takes the CLI models of
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
	registry := cliRegistry(cfg.Models)
	var specs []string
	add := func(spec string) {
		if spec != "" && !slices.Contains(specs, spec) {
			specs = append(specs, spec)
		}
	}
	if models, ok := projectModelOverride(cfg); ok {
		// The override holds only CLI provider models; see
		// ValidateProjectModel.
		for _, model := range models {
			if spec, isCLI := cliModelSpec(registry, model); isCLI {
				add(spec)
			}
		}
		return specs
	}
	for _, model := range append([]string{EffectiveAgentModel(agent, cfg.Models)}, EffectiveAgentFallbacks(agent, cfg.Models)...) {
		if spec, isCLI := cliModelSpec(registry, model); isCLI {
			add(spec)
		} else if strings.TrimSpace(model) != "" && agent.Name != "" {
			add(aviarySpecPrefix + agent.Name)
		}
	}
	return specs
}

// projectModelOverride returns projects.model and projects.fallbacks, in
// order and normalized, and reports whether they override the owning
// agent's: they do whenever model is set.
func projectModelOverride(cfg *Config) ([]string, bool) {
	model := NormalizeProjectModel(cfg.Models, cfg.Projects.Model)
	if model == "" {
		return nil, false
	}
	models := []string{model}
	for _, fallback := range cfg.Projects.Fallbacks {
		if fallback = NormalizeProjectModel(cfg.Models, fallback); fallback != "" {
			models = append(models, fallback)
		}
	}
	return models, true
}

// Sources of a project's job model, as ProjectModelSource reports them.
const (
	ProjectModelSourceOverride = "project override"
	ProjectModelSourceDefaults = "from models.defaults"
)

// ProjectModelSource says where the models of ProjectJobSpecs come from:
// "project override", "from agent <name>", or "from models.defaults" for a
// runtime watch no agent owns.
func ProjectModelSource(cfg *Config, agentName string) string {
	if _, ok := projectModelOverride(cfg); ok {
		return ProjectModelSourceOverride
	}
	if agentName = strings.TrimSpace(agentName); agentName != "" {
		return "from agent " + agentName
	}
	return ProjectModelSourceDefaults
}

// NormalizeProjectModel trims a projects.model or projects.fallbacks entry
// and turns the provider:model shorthand, such as claude:opus, into the
// claude/opus form models use everywhere else. A model that already names
// a provider with a slash, such as codex/gpt-6.1-terra:high, is left alone.
func NormalizeProjectModel(models ModelsConfig, model string) string {
	model = strings.TrimSpace(model)
	if strings.Contains(model, "/") {
		return model
	}
	if provider, name, ok := strings.Cut(model, ":"); ok && strings.TrimSpace(name) != "" {
		if _, known := cliRegistry(models).Lookup(strings.TrimSpace(provider)); known {
			return strings.TrimSpace(provider) + "/" + strings.TrimSpace(name)
		}
	}
	return model
}

// ValidateProjectModel checks one projects.model or projects.fallbacks
// entry: it must be a model of a provider reached through a CLI, which is
// what Code Loop jobs run on, and that CLI must be installed.
func ValidateProjectModel(models ModelsConfig, model string) error {
	model = NormalizeProjectModel(models, model)
	if model == "" {
		return nil
	}
	registry := cliRegistry(models)
	unknown := func(provider string) error {
		return fmt.Errorf("model %q names unknown provider %q; CLI providers are %s", model, provider, strings.Join(registry.Names(), ", "))
	}
	spec, isCLI := cliModelSpec(registry, model)
	if !isCLI {
		provider, _, _ := strings.Cut(model, "/")
		if !llm.IsAPIProvider(provider) {
			return unknown(provider)
		}
		return fmt.Errorf("model %q is not a model of a provider reached through a CLI; project jobs run on CLI providers, such as claude/opus", model)
	}
	provider, _, _ := strings.Cut(spec, "/")
	provider, _, _ = strings.Cut(provider, ":")
	definition, ok := registry.Lookup(provider)
	if !ok {
		return unknown(provider)
	}
	binary := definition.Binary
	if binary == "" {
		binary = provider
	}
	if _, err := exec.LookPath(binary); err != nil {
		return fmt.Errorf("model %q: the %s CLI is not installed (%s not found in PATH)", model, provider, binary)
	}
	return nil
}

// ValidateProjectModels checks projects.model and projects.fallbacks as
// ValidateProjectModel does, and that fallbacks come with a model.
func ValidateProjectModels(models ModelsConfig, p ProjectsConfig) error {
	if strings.TrimSpace(p.Model) == "" && slices.ContainsFunc(p.Fallbacks, func(f string) bool { return strings.TrimSpace(f) != "" }) {
		return fmt.Errorf("projects.fallbacks apply only with projects.model set")
	}
	if err := ValidateProjectModel(models, p.Model); err != nil {
		return fmt.Errorf("projects.model: %w", err)
	}
	for i, fallback := range p.Fallbacks {
		if err := ValidateProjectModel(models, fallback); err != nil {
			return fmt.Errorf("projects.fallbacks[%d]: %w", i, err)
		}
	}
	return nil
}

// cliModelSpec returns the job spec of a CLI provider model: cli/claude/opus
// and claude/opus are both the spec claude/opus. It reports false for any
// other model, which runs on an Aviary agent instead.
func cliModelSpec(registry *clidefs.Registry, model string) (string, bool) {
	model = strings.TrimSpace(model)
	if spec, ok := strings.CutPrefix(model, cliagent.ModelPrefix); ok {
		spec = strings.TrimSpace(spec)
		return spec, spec != ""
	}
	provider, _, _ := strings.Cut(model, "/")
	if _, ok := registry.Lookup(provider); ok && provider != "" && !llm.IsAPIProvider(provider) {
		return model, true
	}
	return "", false
}

// ProjectModelSpec returns the model spec that runs a job of a project
// owned by the named agent with model overriding the agent's own, as
// ProjectJobSpecs maps the agent's model: a CLI provider model runs as its
// spec, and any other model runs on the agent itself, as aviary/<agent>, with
// the model returned as override for that turn. It reports false for a model
// that is not a CLI when the project is owned by no agent to run it on.
func ProjectModelSpec(cfg *Config, agentName, model string) (spec, override string, ok bool) {
	if spec, isCLI := cliModelSpec(cliRegistry(cfg.Models), model); isCLI {
		return spec, "", true
	}
	model, agentName = strings.TrimSpace(model), strings.TrimSpace(agentName)
	if model == "" || agentName == "" {
		return "", "", false
	}
	return aviarySpecPrefix + agentName, model, true
}

// ProjectCLIProviders returns the CLI providers, sorted, that a project
// owned by the named agent uses, so its skills only need installing for
// those: the CLIs of the jobs' models (projects.model and projects.fallbacks
// when model is set, else models, when non-nil, in place of the agent's
// saved model and fallbacks, as ProjectJobSpecs maps them), the Code
// Loop chat agent's model and fallbacks, the unblock agent, and the model of
// every scheduled task targeting the project.
func ProjectCLIProviders(cfg *Config, agentName, target string, models []string) []string {
	registry := cliRegistry(cfg.Models)
	var providers []string
	addSpec := func(spec string) {
		name, _, _ := strings.Cut(strings.TrimSpace(spec), "/")
		name, _, _ = strings.Cut(name, ":")
		name = strings.TrimSpace(name)
		if name != "" && !slices.Contains(providers, name) {
			providers = append(providers, name)
		}
	}
	addModels := func(models []string) {
		for _, model := range models {
			if spec, isCLI := cliModelSpec(registry, model); isCLI {
				addSpec(spec)
			}
		}
	}
	if override, ok := projectModelOverride(cfg); ok {
		addModels(override)
	} else if models != nil {
		addModels(models)
	} else {
		for _, spec := range ProjectJobSpecs(cfg, agentName) {
			if !strings.HasPrefix(spec, aviarySpecPrefix) {
				addSpec(spec)
			}
		}
	}
	for _, agent := range EffectiveAgents(cfg) {
		if agent.Name == ProjectsAgentName {
			addModels(append([]string{EffectiveAgentModel(agent, cfg.Models)}, EffectiveAgentFallbacks(agent, cfg.Models)...))
		}
	}
	if agent := strings.TrimSpace(cfg.Projects.UnblockAgent); agent != "" {
		addSpec(strings.TrimPrefix(agent, cliagent.ModelPrefix))
	}
	target = strings.TrimSpace(target)
	for _, agent := range cfg.Agents {
		for _, task := range agent.Tasks {
			if target == "" || !strings.EqualFold(strings.TrimSpace(task.Project), target) || strings.TrimSpace(task.Model) == "" {
				continue
			}
			if spec, _, ok := ProjectModelSpec(cfg, agentName, task.Model); ok && !strings.HasPrefix(spec, aviarySpecPrefix) {
				addSpec(spec)
			}
		}
	}
	slices.Sort(providers)
	return providers
}
