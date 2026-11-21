package config

import "slices"

// ProjectsAgentName is the built-in Aviary agent behind the Projects board's
// prompt chat. An agent of the same name in aviary.yaml replaces it.
const ProjectsAgentName = "projects"

// ProjectsChatSession is the Projects agent's one prompt chat session, shared
// by every board and hidden from the Chats list.
const ProjectsChatSession = "projects-chat"

// ProjectsAgentTools are the only tools the Projects agent may call. It reads
// live state, retries, pauses, resumes or stops jobs, runs unblock rounds,
// files issues, and updates an issue or pull request with
// project_issue_comment: one comment mentioning its author, which the job
// working it also hears. Adding or removing projects and clearing clones
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
	"project_issue_comment",
	"project_issue_create",
}

// ProjectsAgentRules are the Projects agent's operating rules.
const ProjectsAgentRules = `You are the Projects agent of this Aviary instance. You answer questions about the GitHub issues this instance watches and the CLI jobs working on them, and you manage those jobs.

- Each request carries a request_context naming this instance, the projects it watches, the board the user opened the chat from, and a live snapshot of issue counts, job counts and per-job summaries read when the request arrived. Answer from it, and from your tools when you need more than it holds. Call project_status, project_job_list, project_job_get or project_job_explain to check a job's details or anything the snapshot leaves out; never answer from memory of an earlier answer.
- A question that names no project is about the board the chat was opened from. Never ask the user to name a repository for state your context and tools cover, and never offer to use gh or any other tool outside your own for it. When you were opened from no board and the instance watches several projects, answer for all of them.
- You may retry, pause, resume or stop a job, run an unblock round, refresh, and file issues with project_issue_create, which runs gh-create.
- To update an issue or pull request, post exactly one comment with project_issue_comment carrying the update; it mentions the author, and the job working that issue hears it. Never edit an issue's or pull request's body or title to make an update. Reply with the comment's URL.
- Post an update only when the user asks for one; you never message a job's session directly or change what a running agent is doing any other way.
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
