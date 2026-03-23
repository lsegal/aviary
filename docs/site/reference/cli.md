# CLI Reference

## Global Flags

These flags apply to every `aviary` command.

| Flag | Default | Description |
| --- | --- | --- |
| `--config <path>` | `~/.config/aviary/aviary.yaml` | Path to the config file |
| `--data-dir <path>` | `~/.config/aviary` | Data directory |
| `--server <url>` | `https://localhost:16677` | Aviary server URL |
| `--token <token>` | _(stored token)_ | Authentication token (overrides stored token) |

---

## aviary start

Start the Aviary server over HTTPS on the configured port (default: 16677).

```
aviary start
```

No flags.

---

## aviary stop

Stop the running Aviary server.

```
aviary stop
```

No flags.

---

## aviary status

Show whether the Aviary server is currently running.

```
aviary status
```

No flags.

---

## aviary logs

Tail Aviary server logs from the filesystem.

```
aviary logs [flags]
```

| Flag | Default | Description |
| --- | --- | --- |
| `-f, --follow` | `true` | Follow log output as it is written |
| `-n, --lines <n>` | `200` | Number of trailing lines to show before following |

---

## aviary token

Show or regenerate the Aviary server authentication token. The token is used to authenticate with the Aviary web UI and API.

```
aviary token [flags]
```

| Flag | Default | Description |
| --- | --- | --- |
| `--new` | `false` | Generate and store a new token, replacing the existing one |

---

## aviary doctor

Validate `aviary.yaml` and verify that required credentials exist in the auth store. Exits with status 1 if any errors are found.

```
aviary doctor [flags]
```

| Flag | Default | Description |
| --- | --- | --- |
| `--disable-version-check` | `false` | Skip the GitHub release version check and upgrade prompt |

---

## aviary configure

Interactive configuration wizard for initial setup and ongoing configuration. Covers providers, agents, server settings, and more.

```
aviary configure [subcommand]
```

Running `aviary configure` with no subcommand runs the full onboarding wizard.

### aviary configure providers

Authenticate with AI providers via OAuth or API key. Alias: `auth`.

```
aviary configure providers
```

### aviary configure general

Configure shared runtime settings.

```
aviary configure general
```

### aviary configure agents

Add, view, or remove agents interactively.

```
aviary configure agents
```

### aviary configure skills

Enable and configure installed skills.

```
aviary configure skills
```

### aviary configure server

Configure server port and TLS options.

```
aviary configure server
```

### aviary configure browser

Configure browser automation settings.

```
aviary configure browser
```

### aviary configure scheduler

Configure task concurrency.

```
aviary configure scheduler
```

---

## aviary agent

Manage agents.

```
aviary agent <subcommand>
```

### aviary agent list

List all configured agents and their current state.

```
aviary agent list
```

### aviary agent run

Send a message to an agent and stream the response.

```
aviary agent run <name> [message] [flags]
```

| Flag | Default | Description |
| --- | --- | --- |
| `--file <path>` | | Read the prompt from a file instead of inline |
| `--bare` | `false` | Run without any system prompt, rules, memory, or tool preamble |
| `--history` | `true` | Include prior session history in the prompt |

### aviary agent stop

Immediately stop all work in progress for an agent.

```
aviary agent stop <name>
```

### aviary agent template-sync

Sync embedded template files into an agent directory. Missing files are added; existing files are preserved.

```
aviary agent template-sync <name>
```

---

## aviary task

Manage scheduled tasks.

```
aviary task <subcommand>
```

### aviary task list

List all tasks, their trigger type, and last run status.

```
aviary task list
```

### aviary task run

Manually trigger a task immediately.

```
aviary task run <name>
```

### aviary task stop

Stop all currently running scheduled task jobs.

```
aviary task stop
```

---

## aviary job

View job history and logs.

```
aviary job <subcommand>
```

### aviary job list

Show job history across all tasks.

```
aviary job list [flags]
```

| Flag | Default | Description |
| --- | --- | --- |
| `--task <name>` | | Filter results to a specific task name |

### aviary job logs

Stream logs for a specific job run.

```
aviary job logs <job-id>
```

---

## aviary auth

Manage authentication credentials.

```
aviary auth <subcommand>
```

### aviary auth login

Authorize with an AI provider. Supported providers: `anthropic`, `openai`, `gemini`, `github-copilot`.

```
aviary auth login <provider> [flags]
```

| Flag | Default | Description |
| --- | --- | --- |
| `--code <code>` | | Authorization code (skips interactive browser prompt) |
| `--pat <token>` | | GitHub personal access token for `github-copilot` (skips device flow) |

### aviary auth set

Store a credential (API key or token) by name.

```
aviary auth set <name> <value>
```

### aviary auth get

Show whether a credential is stored (value is masked).

```
aviary auth get <name>
```

### aviary auth list

List all stored credential names.

```
aviary auth list
```

### aviary auth delete

Remove a stored credential.

```
aviary auth delete <name>
```

---

## aviary browser

Control a Chromium browser via the Chrome DevTools Protocol (CDP). All subcommands share the following persistent flags.

```
aviary browser <subcommand> [flags]
```

**Persistent flags:**

| Flag | Default | Description |
| --- | --- | --- |
| `--browser-binary <path>` | _(auto-detected)_ | Path to a Chromium or Chrome binary |
| `--cdp-port <port>` | `9222` | Chrome DevTools Protocol debugging port |
| `--profile-directory <name>` | | Chrome profile directory name (e.g. `Default`, `Work`) |
| `--tab <id>` | | CDP tab ID returned by `browser open` |

### aviary browser open

Open a URL in a new browser tab. Prints the tab ID for use with `--tab`.

```
aviary browser open <url>
```

### aviary browser tabs

List all open browser tabs.

```
aviary browser tabs
```

### aviary browser navigate

Navigate an existing tab to a new URL.

```
aviary browser navigate <url>
```

### aviary browser click

Click an element by CSS selector.

```
aviary browser click [flags]
```

| Flag | Description |
| --- | --- |
| `--selector <selector>` | CSS selector of the element to click |

### aviary browser type

Send keystrokes into an element.

```
aviary browser type [text] [flags]
```

| Flag | Description |
| --- | --- |
| `--selector <selector>` | CSS selector of the target element |
| `--text <text>` | Text to type |

### aviary browser fill

Fill text into an element (clears existing value first).

```
aviary browser fill [text] [flags]
```

| Flag | Description |
| --- | --- |
| `--selector <selector>` | CSS selector of the target element |
| `--text <text>` | Text to fill |

### aviary browser screenshot

Capture a screenshot of the current tab as a PNG.

```
aviary browser screenshot
```

### aviary browser wait

Wait for an element to become visible.

```
aviary browser wait [flags]
```

| Flag | Default | Description |
| --- | --- | --- |
| `--selector <selector>` | | CSS selector to wait for |
| `--timeout-ms <ms>` | `10000` | Wait timeout in milliseconds |

### aviary browser text

Extract normalized page text.

```
aviary browser text [flags]
```

| Flag | Default | Description |
| --- | --- | --- |
| `--selector <selector>` | | Optional CSS selector to scope extraction |
| `--max-length <n>` | `4000` | Maximum number of characters to return |

### aviary browser query

Extract structured element data by CSS selector.

```
aviary browser query [flags]
```

| Flag | Default | Description |
| --- | --- | --- |
| `--selector <selector>` | | CSS selector |
| `--count <n>` | `20` | Maximum number of elements to return |
| `--max-length <n>` | `500` | Maximum text length per element |
| `--include-html` | `false` | Include outer HTML for each element |

### aviary browser eval

Evaluate a JavaScript expression in the current tab.

```
aviary browser eval <expr>
```

---

## aviary models

Inspect supported provider and model pairs.

```
aviary models [flags]
aviary models list [flags]
```

| Flag | Description |
| --- | --- |
| `--provider <name>` | Filter results to a specific provider name |
