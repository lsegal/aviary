# Architecture

Major non-UI components written in Go, web UI should be written in Vue.js and packaged inside of the Go binary.

## Components

The architecture of the orchestrator is made up of 11 major components:

### Main Process

- All main operations are exposed through MCP server (schedule task, send message, read memory, etc) so that the agent can talk via MCP
- Server runs on 16677 by default (configurable)
- MCP hosted at /mcp
- Control plane hosted at /

### MCP Server

- Server must be hosted over HTTPS. No HTTP. Use self signed cert
- AUTHENTICATION IS REQUIRED. A token must be generated on first run and that token must be used in bearer auth.
- All API plane commands exposed through MCP command (discoverable) with necessary arguments.
- No REST, no GRPC, use MCP protocol for HTTP messaging.
- CLI interfaces with MCP server or it can bypass if it's more efficient.
- We need to make sure that we can direct agentic operations to these MCPs. The biggest challenge will be proxying the MCP because we don't want to expose the MCP HTTP service directly to the internet. It's only for the local client, control plane, and scheduler

### Control Plane

- Server must be hosted over HTTPS. No HTTP. Use self signed cert
- Token must be presented to interact with control plane! Login screen can be present with a token input box with [Sign in] and token should be stored in secured (long term) session cookies. Cookies don't need to expire, but token should be constantly validated when accessing data-- erroring with a 401 should kick you back to login, cookie should be validated on load, etc.
- Control plane runs a Vue.js (SPA) app packaged into go binary with state of the art approach
- App should hot reload in dev mode (not sure how to do this, vite?)-- if not possible, don't do this.
- Vue app uses shadcn for UI
- Control plane has a chat UI that has a window into any active session for any agent.
- Use pnpm for package management.

### CLI

- CLI either calls MCP operations directly in code or uses HTTP API to call to MCP server
- Every MCP command should be exposed to CLI with the same arguments, so that users can choose to use CLI or HTTP API as they see fit.
- `aviary serve start` / `aviary serve stop` should be disconnected from MCP, everything else should have an equivalent.

### Configuration Watcher

- Checks for changes to configuration (`~/.config/aviary/aviary.json`) and reloads all services on changes.
- All services must idempotently update on configuration change without "restart" or losing state.

#### Configuration Format

- Configuration should use YAML
- Errors should provide full validation and context for what went wrong, syntactically or semantically. We should have a JSON schema (yes, even though it's yaml, see gerald1248/validate-yaml)
- Need configuration for server, agents, scheduler, channels (where messages are checked / read from), models to use, and permissions.
- Configuration should be able to be updated on the fly and should be reflected in the system without needing to restart. For example, if I add a new agent or update an existing agent's configuration, that should be reflected in the system immediately without needing to restart.
- In most cases, configuration should be agent dependent, i.e. each agent can have its own configuration for things like channels, permissions, models, etc. But there should also be a global configuration that can set defaults for all agents.
- Models need their own configuration for auth, and in general, auth should be configured separately. This extends beyond models since some tools / channels may also require auth credentials. We should have like an `auth:XYZ` string syntax that can pull tokens from an auth configuration file or system keychain depending on config by name, i.e. `auth:openai:lsegal@soen.ca` would pull the OpenAI API key from the auth configuration / keychain by name "openai:lsegal@soen.ca".

### Scheduler (job queue)

- Runs a cron-style system that checks a job queue for in-progress and scheduled tasks
- Job queue should be re-entrant, retryable, and resilient enough to automatically continue on server restarts.
  - Basically a job should retry until it completes, runs into a non-retryable error, or has exhausted retries.
  - Note that an AI error like throttling/quota limit is not a non-retryable error.
  - Retry can be exponential backoff, as long as it queues a retry.
- Queue should run in parallel but configurable (default should max out available CPUs/threads)
- Basically the structural flow should be:
  - Task defines an operation(s) to run on a specific schedule (repeating, once) and with which arguments (and env vars)
  - Job is triggered by a Task and enters the Queue
  - Each execution of a Job in the Queue is called a Run
  - A Job can have multiple Runs
  - tldr: Task has many Jobs (through scheduling). Job has many Runs (through queue executions / retries).
  - We can start with a single Queue, but architecture should support multiple queues with different priorities.
- Since schedule is attached to an agent (in config), when a task is triggered, it should send the task details to the corresponding agent via MCP and the agent can execute the task and stream results back to the scheduler and any relevant channels.
- Task defines which exact channel route to send results to, including the specific delivery target, or whether to run in silent mode (no message).

### Browser Control

- Need ability to control chromium compatible browser via CDP (chrome devtools protocol) for agentic operations that require browser control (e.g. web navigation, form filling, etc).
- This should be exposed through CLI and MCP, i.e. `aviary browser open example.com`
- CDP default port / host should be configurable, and we should support multiple browser instances with different ports/hosts.
- Browser should use a dedicated profile name via `--profile-directory` (default `Aviary` when unset). We intentionally do not pass `--user-data-dir`, so Chrome uses its normal default user data location.
- Should allow configuring chrome binary and command line args (appended to the ones we use above).
- Every CDP operation should have a corresponding CLI and MCP command, e.g. `aviary browser click --selector "#submit-button"` or `POST /mcp/browser/click { "selector": "#submit-button" }`

### Agent Manager

- Responsible for communicating with agents via vLLM or OpenAI API or streaming stdio when using a CLI like claude/codex/gemini.
- Example: Message received on a channel that agent is subscribed to, message is forwarded as a prompt into the agent, streaming response back to the channel as it's received from the agent.
- Example: Scheduler triggers a task that is assigned to an agent, task details are forwarded as a prompt into the agent, streaming response back to the scheduler and any relevant channels as it's received from the agent.
- Agents should be able to parallelize prompts, i.e. multiple messages at the same time all part of the same session.
- There should be a way to kill work by saying "stop" or something equivalent, which should immediately stop all current work and clear the queue for that agent. i.e. this should be an MCP task.
- Example: "Do X" "Now do Y" should do both X and Y at the same time and stream results back as they come in, rather than doing X then Y sequentially. I should be able to say "stop" while they're running and it should stop both X and Y immediately.
- Stopping scheduled tasks should be a separate MCP ("stop running tasks").
- Text and image should be supported in all channels. Agent should respond to images and media and be able to provide media in responses.
- Each agent has a "main" session and separate sessions for each scheduled task.
- Multiple sessions should be able to share memory. Generally only the agent's main session should be configurable to share memory, not scheduled tasks, though architecturally it should be possible to share memory across scheduled tasks as well if needed.
- Agents should use "thinking model" style conversations to continue conversations over multiple messages, not just sending a single message and getting a reply.

### Memory Management

- All conversations should be added to persistent memory by agent / session.
- This component needs to manage memory efficiently (compacting, passing memory dumps, searching memory, etc) so that we can have long conversations without running into token limits.
- Memory should be searchable and retrievable by agent when needed via MCP.
- Multiple agents should be able to access the same memory
- Multiple messages and conversations should be able to append to the same memory and incrementally access the same memory immediately.
  - Example: if I say "do X" and "do Y" in parallel and "do X" completes first, the output of "do X" should be available to any followup messages in the do Y conversation.

### Tools / Skills

- Agents should be able to use tools/skills that are not necessarily tied to a specific channel, e.g. a "search" tool that can search the web and return results, or a "code execution" tool that can execute code and return results.
- Tools should be able to be added on the fly and should be reflected in the system immediately without needing to restart.
- Tools should use Skills via the [AgentSkill format](https://agentskills.io/home)
- Each agent can define which skills are accessible
- Skills can be configured at a toplevel, potentially with auth tokens.

## Development Environment

### Testing Approach

- Unit tests for all components with high coverage, especially for critical components like memory management and scheduler.
- Integration tests for major workflows, e.g. sending a message to an agent and getting a response, scheduling a task and having it execute, etc.
- End-to-end tests that run the entire system and test major user flows, e.g. starting the server, sending messages, scheduling tasks
- E2E UI tests for web interface with Playwright
- Always write and run tests on changes (go test, pnpm test:e2e)
- Always run linters and formatters on changes (go fmt, go vet, pnpm lint / biome check for web)
