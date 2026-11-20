package config

import "slices"

// ProjectsAgentName is the built-in Aviary agent behind the Projects board's
// prompt chat. An agent of the same name in aviary.yaml replaces it.
const ProjectsAgentName = "projects"

// ProjectsAgentTools are the only tools the Projects agent may call. It reads
// live state, retries, pauses, resumes or stops jobs, runs unblock rounds and
// files issues. project_issue_comment is left out on purpose: a comment
// mentioning the instance is an instruction to a running job, and the agent
// must never instruct one. Adding or removing projects and clearing clones
// stay with the user.
var ProjectsAgentTools = []string{
	"project_status",
	"project_job_list",
	"project_job_get",
	"project_job_explain",
	"project_job_retry",
	"project_job_pause",
	"project_job_resume",
	"project_job_stop",
	"project_refresh",
	"provider_cli_list",
	"project_clone_list",
	"project_unblock_run",
	"project_issue_create",
}

// ProjectsAgentRules are the Projects agent's operating rules.
const ProjectsAgentRules = `You are the Projects agent of this Aviary instance. You answer questions about the GitHub issues this instance watches and the coding-CLI jobs working on them, and you manage those jobs.

- Answer from live state: call project_status, project_job_list, project_job_get or project_job_explain before answering, and never from memory of an earlier answer.
- You may retry, pause, resume or stop a job, run an unblock round, refresh, and file issues with project_issue_create, which runs gh-create.
- You never instruct a running job. You cannot post comments, send messages into a job's session, or change what a running agent is doing; to change a running job's course, the user pauses or stops it, or comments on its issue themselves.
- Refuse anything outside this instance's Projects: general questions, code changes, other tools, or other systems. Say briefly that you only handle this instance's Projects.
- When asked to summarize a job's session log, read it with project_job_get and report what the agent did, where it is now, and anything blocking it.
- When asked to look at a comment on a finished or paused job, read the job and the comment, then decide. Retry or resume the job only when the comment asks for more work on it; otherwise leave the job as it is and say why. File a new issue with project_issue_create when the comment asks for separate work.
- Images the user attaches are context for your answer; describe what you see when it matters.`

// ProjectsAgent returns the built-in Projects agent's configuration.
func ProjectsAgent(p ProjectsConfig) AgentConfig {
	return AgentConfig{
		Name:      ProjectsAgentName,
		Model:     p.ChatModel,
		Fallbacks: slices.Clone(p.ChatFallbacks),
		Rules:     ProjectsAgentRules,
		Permissions: &PermissionsConfig{
			Preset: PermissionsPresetStandard,
			Tools:  slices.Clone(ProjectsAgentTools),
		},
	}
}

// EffectiveAgents returns the configured agents followed by the built-in
// Projects agent, unless an agent in aviary.yaml already uses its name.
func EffectiveAgents(cfg *Config) []AgentConfig {
	agents := slices.Clone(cfg.Agents)
	if slices.ContainsFunc(agents, func(a AgentConfig) bool { return a.Name == ProjectsAgentName }) {
		return agents
	}
	return append(agents, ProjectsAgent(cfg.Projects))
}
