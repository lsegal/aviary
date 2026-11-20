package config

import (
	"slices"
	"strings"

	"github.com/lsegal/aviary/internal/cliagent"
)

// aviarySpecPrefix marks a model spec that runs a job on an Aviary agent.
const aviarySpecPrefix = "aviary/"

// ProjectJobSpecs returns the model specs, in fallback order, that jobs
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
