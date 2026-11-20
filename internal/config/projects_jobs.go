package config

import (
	"slices"
	"strings"

	"github.com/lsegal/aviary/internal/cliagent"
	"github.com/lsegal/aviary/internal/clidefs"
	"github.com/lsegal/aviary/internal/llm"
)

// aviarySpecPrefix marks a coding-CLI spec that runs a job on an Aviary agent.
const aviarySpecPrefix = "aviary/"

// ProjectJobSpecs returns the coding-CLI specs, in fallback order, that jobs
// of a project attached to the named agent run on: the agent's model and
// fallbacks, after models.defaults, each CLI provider model as its spec:
// cli/claude/opus and claude/opus are both the spec claude/opus. A model
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
	registry := cliRegistry(cfg.Models)
	var specs []string
	add := func(spec string) {
		if spec != "" && !slices.Contains(specs, spec) {
			specs = append(specs, spec)
		}
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

// ProjectModelSpec returns the coding-CLI spec that runs a job of a project
// attached to the named agent with model overriding the agent's own, as
// ProjectJobSpecs maps the agent's model: a CLI provider model runs as its
// spec, and any other model runs on the agent itself, as aviary/<agent>, with
// the model returned as override for that turn. It reports false for a model
// that is not a CLI when the project is attached to no agent to run it on.
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
