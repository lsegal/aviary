# CLI Provider Definitions

Aviary reaches some providers through their own CLI, such as Claude Code, Codex or Gemini CLI. Aviary has no CLI-specific launch code: every CLI provider is described by a **definition**, a document that says which executable to run, the argv for a fresh run, a resume and a vision call, the environment the child process gets, how its session ID comes to exist, how its output is decoded, how Aviary's approval modes map to its flags, where its quota is read, and which skills.sh target its skills install for.

Aviary ships definitions for the CLIs below and reads more from `models.cli_definitions` in `aviary.yaml`, so supporting another CLI is configuration, not a new release. Every surface that launches a CLI reads the same definitions: chat turns on an agent whose model is a CLI model, [Projects](/guide/projects) jobs, the Projects prompt chat and watchdog, and System → Providers & Auth.

This page is the reference for that schema. To see how a definition is used in the issue-to-merge loop, read [The code loop on GitHub](/guide/code-loop).

## The built-in definitions

| Name | CLI | Levels | Default model | Session | Output | Quota | skills.sh target |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `claude` | [Claude Code](https://docs.anthropic.com/en/docs/claude-code) | `low`, `medium`, `high` | `sonnet` | Aviary assigns the ID | `stream-json` | `claude` | `claude-code` |
| `codex` | [Codex CLI](https://developers.openai.com/codex/cli/) | `low`, `medium`, `high` | `gpt-5.6-terra` | captured from its output | `codex-json` | `codex` | `codex` |
| `gemini` | [Gemini CLI](https://github.com/google-gemini/gemini-cli) | none (`[]`) | `gemini-3.5-flash` | Aviary assigns the ID | `gemini-json` | none | `gemini-cli` |
| `muse` | [Meta Muse Code](https://dev.meta.ai/docs/muse-code) | `none` through `ultra` | `muse-spark-1.3` | Aviary assigns the ID | `jsonl` | none | `universal` |
| `opencode` | [opencode](https://opencode.ai) | `low`, `medium`, `high` | none (per-account catalog) | none | `jsonl` | none | `opencode` |
| `cline` | [Cline](https://cline.bot) | `none`, `low`, `medium`, `high`, `xhigh` | none (per-account catalog) | none | `jsonl` | none | `cline` |
| `agy` | [Antigravity CLI](https://antigravity.google/docs/cli/overview) | `low`, `medium`, `high` | `claude-sonnet-4-6` | none | `text` | none | `antigravity-cli` |
| `aviary` | one of your Aviary agents, run in-process | none | none | none | none | none | none |

The documents themselves are in the repository under [`internal/clidefs/builtin/`](https://github.com/lsegal/aviary/tree/main/internal/clidefs/builtin), and they are the best worked examples of the schema.

A model names a definition and, optionally, a model and a reasoning level: `claude/opus`, `codex/gpt-5.6-terra:high`, or the same with a `cli/` prefix (`cli/claude/opus`). An agent whose model or fallbacks name a CLI model runs on that CLI.

A CLI with no resumable session is not a degraded one. When its run exits early, the job is restarted with a recovery prompt, and `gh-fix` is re-entrant, so the restarted run adopts the branch and draft pull request the previous one left behind.

`agy` cannot run Projects jobs yet: it has no machine-readable output mode, so it cannot produce a structured job transcript. Its `projectsUnavailable` field says so, and dispatching a job to it fails with that reason.

`aviary/<agent>` is the `aviary` definition: it runs the turn on one of your Aviary agents inside the server rather than as a child process, so it declares no argv or output.

### Reporting on the CLIs

`aviary models clis` (the `provider_cli_list` tool), System → Providers & Auth and `aviary doctor` report every definition in force: whether its CLI is installed and where, its version, whether it is signed in, its quota, and the models it accepts as fully qualified `cli/<name>/<model>` names. A CLI that is not installed is still listed.

The models are asked of each CLI rather than carried in Aviary, since a list written down is stale the day a vendor ships: `opencode` and `agy` run their own `models` command, `codex` runs `codex debug models`, `gemini` and `cline` answer over their agent-client protocol (`--acp`), and `muse` answers `model/list` over `muse serve`. Claude Code has no way to be asked, so `claude` reports the aliases and ids its definition knows and says the CLI may accept others. A CLI signed out of its provider lists nothing, and the report names the command that could not list it.

## `models.cli_definitions`

Definitions are configured in `aviary.yaml`, keyed by name:

```yaml
models:
  cli_definitions:
    claude:
      binary: /opt/claude/bin/claude
    robo:
      binary: robo
      session: { assign: none }
      output: { format: text }
      args:
        run:
          - args: [run, --auto-approve]
          - args: ["{prompt}"]
```

Definitions load built-ins first, then `cli_definitions` on top:

- A key that matches a **built-in overrides it field by field**. Fields the entry does not mention keep the built-in's value, so pointing `claude` at a different binary is one line.
- A field set to **`null` drops what it inherited** and falls back to the schema's own default. That is how you remove a built-in's `env`, `quota` or `levels` rather than replace it.
- A **key the registry does not know registers a new CLI**, immediately usable as a model such as `cli/robo/big-model`.

Anything wrong with an entry is an error naming the agent and the field: a field the schema does not define, an invalid value, or a definition that fails validation. `aviary doctor` reports it. Nothing is dropped silently, because a CLI missing from the registry looks exactly like a typo in a model name.

## The definition schema

### Top level

| Field | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `name` | string | no | the key | The name used in models; when given it must match the key. Letters, digits, dot, dash and underscore only. |
| `binary` | string | yes | | The executable, a path or a name looked up on `PATH`. System → Providers & Auth and `aviary doctor` probe the same executable jobs run. |
| `inProcess` | bool | no | `false` | Runs the turn on an Aviary agent inside the server. Such a definition declares no `args` or `output`, and `session.assign` must be `none`. |
| `minVersion` | string | no | none | The lowest CLI version the argv works with. See [`minVersion`](#minversion). |
| `args` | object | yes | | The argv templates. See [`args`](#args). |
| `env` | map of string | no | none | Extra environment for the child process, on top of Aviary's own. |
| `configHome` | object | no | none | Keeps the CLI from writing to your own copy of its settings file. See [`configHome`](#confighome). |
| `session` | object | yes | | How the session ID is established. See [`session`](#session). |
| `levels` | string[] | no | any | Allow-list for the `:level` part of a model. See [Allow-lists](#allow-lists). |
| `models` | string[] | no | any | Allow-list for the model part. See [Allow-lists](#allow-lists). |
| `defaultModel` | string | no | none | The model used when none is named. See [`defaultModel`](#defaultmodel). |
| `approval` | map | no | none | The CLI's flags for each approval mode. See [`approval` and `extraDir`](#approval-and-extradir). |
| `extraDir` | map | no | none | The flags that add one more directory to the CLI's scope. See [`approval` and `extraDir`](#approval-and-extradir). |
| `permissions` | map | no | none | The flags that pass an agent's exec and filesystem rules to the CLI. See [`permissions`](#permissions). |
| `output` | object | yes | | How stdout is decoded. See [`output`](#output). |
| `missingSession` | string[] | no | none | Extra phrases that mean the session to resume is gone. See [`missingSession`](#missingsession). |
| `wakeup` | object | no | none | How the CLI ends a turn asking to be woken later. See [Deferred turns](/guide/projects#deferred-turns). |
| `projectsUnavailable` | string | no | none | Keeps the CLI out of Projects jobs, with the reason; it stays usable elsewhere. |
| `quota` | object | no | `{reader: none}` | Where the quota reading comes from. See [`quota`](#quota). |
| `skills` | object | no | none | The skills.sh target its skills install for. See [`skills`](#skills). |
| `promptStdin` | string[] | no | none | Lets long prompts go to stdin. See [`promptStdin`](#promptstdin). |
| `doctor` | object | no | none | The read-only probes the CLI report runs. See [`doctor`](#doctor). |

### Allow-lists

`levels` and `models` validate the two optional parts of a model such as `codex/gpt-5.6-terra:high`. Each has three distinct states, because "this definition names no list" and "this CLI has no such flag" are different claims:

| Value | Meaning |
| --- | --- |
| absent, or `null` | Accepts any value. |
| `[low, medium, high]` | Accepts exactly those, and rejects anything else with the list. |
| `[]` | Accepts nothing: the CLI has no such flag, so naming one is an error rather than a value silently dropped. |

`gemini` declares `levels: []`, so `gemini/gemini-3.5-flash:high` is rejected instead of parsed and then discarded for want of a `{level}` fragment to render it into. Overriding an allow-list with `null` restores accepting anything; write `[]` when you mean nothing.

None of the built-ins names a `models` list: the CLIs take a live catalog, and a list would reject a model released this morning.

### `defaultModel`

Most CLIs default to their largest model, which is the wrong thing to spend a queue of issues on. A definition can name the model Aviary runs it with when the configured model names none, so the choice is Aviary's rather than the CLI's. The default is resolved when the model is parsed, so it is the model the argv passes, the board shows and a job's saved state records; a resumed session keeps the model it started with even if the default moves. A model you name always wins.

The value must be one `models` admits when that list names values. An override that narrows `models` on a built-in must name a `defaultModel` the narrowed list allows, or drop the inherited one with `defaultModel: null`.

Leaving it out renders no `{model}` and lets the CLI decide. That is deliberate for `opencode` and `cline`, whose catalogs are fetched for the signed-in account: an id written into the definition would fail every dispatch on an account that cannot reach it. A default also leaves the level to `{level}`: `agy` uses `claude-sonnet-4-6`, the mid-tier id in its catalog with no reasoning level spelled into the id, so its `--effort` flag is the only thing that names one.

### `args`

One argv template per shape of invocation:

| Field | Required | Description |
| --- | --- | --- |
| `args.run` | yes | A fresh run. |
| `args.resume` | unless `session.assign` is `none` | Continuing an earlier session. Without one, a recovery renders `args.run` again with the recovery prompt. |
| `args.vision` | no | A one-shot call that reads a screenshot. A CLI without one is never asked to. |

Each template is a list of **fragments**, appended in order. A fragment is deliberately not an expression language: it tests one named value for presence, and nothing else.

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `when` | string | no | The condition. Empty always holds; a leading `!` negates it. |
| `args` | string[] | yes | The arguments added when the condition holds. Cannot be empty. |

#### Placeholders

`{name}` spans inside a fragment's arguments are substituted from the call. An unknown placeholder is rejected.

| Placeholder | Value |
| --- | --- |
| `{prompt}` | The text the CLI is asked to act on. |
| `{session}` | The session ID being assigned or resumed. |
| `{model}` | The model. |
| `{level}` | The reasoning level. |
| `{image}` | The screenshot a vision call reads. |
| `{approval}` | The definition's `approval` flags for the call's mode. Must be a whole argument. |
| `{settings}` | Claude Code's Remote Control settings payload. |
| `{sessionName}` | The name a Remote Control run appears under. |

#### Conditions

`when` accepts every placeholder name, which holds when that value is non-empty, plus these:

| Condition | Holds when |
| --- | --- |
| `yolo` | The call runs in the `full` approval mode. |
| `remoteControl` | The Projects job runs with Claude Code's Remote Control bridge. |
| `oneShot` | The call is an out-of-band one-shot outside any checkout, such as the Projects prompt chat or the stuck-job watchdog. Dispatched jobs never set it. |
| `noCheckout` | The CLI runs outside any checkout: every `oneShot` call, and a job whose clone does not exist yet. |

So `{when: model, args: [--model, "{model}"]}` passes a model only when one was named, and `{when: noCheckout, args: [--skip-git-repo-check]}` is how `codex` runs outside a repository.

### `approval` and `extraDir`

`approval` maps Aviary's [approval modes](/reference/config#approval) (`read-only`, `auto`, `full`) to the CLI's own flags, rendered where a template has an argument that is exactly `{approval}`. A mode left out falls back to the closest less permissive mode declared; with none, the flag is left off. Aviary never edits a CLI's settings files to set a mode. The built-in mappings are listed under [`approval`](/reference/config#approval).

`extraDir` maps the same modes to the flags that add one more directory to the CLI's scope, with `{dir}`, `{dirQuoted}` (a quoted TOML string) or `{dirPosix}` (`C:\a` becomes `/c/a`) standing for it. Chat turns use it to keep the agent's own directory in scope wherever it works.

### `permissions`

`permissions` maps the same modes, with the same fallback, to the flags that pass each of a chat agent's [exec and filesystem rules](/reference/config#agents-permissions) on to the CLI's own allow and deny rules. Each mode lists up to four templates, each rendered once per rule:

| Field | Rendered for | Placeholders |
| --- | --- | --- |
| `allowCommand` | each `exec.allowed_commands` rule | `{commandSpec}`: `(pattern)`, or nothing for `*` |
| `denyCommand` | each `!` command rule | `{commandSpec}` |
| `allowPath` | each `filesystem.allowed_paths` rule | `{path}` and `{pathPosix}` (the expanded pattern), `{dir}` and `{dirPosix}` (the directory its globs sit under) |
| `denyPath` | each `!` path rule | the same as `allowPath` |

A template left out passes nothing for that kind of rule. `claude` declares the allows for `auto` and `full` and the denies for every mode, as `[--allowedTools, "Bash{commandSpec}"]` and `[--disallowedTools, "Edit(/{pathPosix})"]`.

A mode may also list `enforce`: flags, with no placeholders, that replace the mode's `approval` flags when the agent's rules are narrower than allowing every command (`*`) and the whole Aviary directory (`@/**`). A mode that skips the CLI's own permission checks would otherwise skip its allow rules too, and only `!` rules would bind. `claude` declares it for `full` as `[--permission-mode, dontAsk]`, plus allows for the tools the agent's rules do not govern (`WebFetch`, `WebSearch`, `Skill`), so in `full` a command or write the rules leave out is refused while everything they allow runs without asking.

### `minVersion`

An argv is written against a particular release of its CLI. When an older one is installed, the flags do not exist yet and the run dies on an unrecognized-argument error that says nothing about the version. With `minVersion: "0.58.0"`, Aviary runs `binary --version` before each dispatch and compares it:

- **Below the minimum:** the dispatch fails before the CLI starts, naming the version found and the version required. Upgrade the CLI, or point `binary` at a newer install.
- **At or above it:** the run proceeds.
- **No readable version:** a warning goes into the run's output and the dispatch proceeds, rather than blocking every CLI that prints its version in an unfamiliar way.

Versions are compared component by component, so `0.9.0` is older than `0.58.0`, and a prerelease suffix such as `-beta.3` is ignored. A definition without `minVersion` is not checked and starts no extra process. Of the built-ins, only `gemini` declares one.

### `configHome`

Some CLIs keep their settings, sign-in and sessions in one directory and write bookkeeping into a settings file there; Codex records every workspace it runs with write access in as a trusted project in `config.toml`. `configHome` launches such a CLI against an overlay of that directory kept in Aviary's data directory: each `private` file is a copy, so the CLI reads your settings but its writes stay in the overlay, and every other entry links back to yours, so sign-in and sessions are shared.

Claude Code, for another, ignores the permission allow entries of a workspace it has not recorded as trusted in `.claude.json`, even under `-p`, and a job's fresh clone never is. `configHome.trust` records every workspace a launch runs in as trusted in the overlay's copy instead, so the workspace's settings apply in every approval mode while your own `.claude.json` is left untouched.

| Field | Description |
| --- | --- |
| `configHome.env` | The variable the CLI reads its directory from, such as `CODEX_HOME`. |
| `configHome.default` | The directory used when that variable is unset; a leading `~/` is your home directory. |
| `configHome.private` | Files directly inside the directory that each launch gets its own copy of. |
| `configHome.outside` | Optional. Maps a `private` file to where it lives while the variable is unset, when that is outside the directory; Claude Code keeps `~/.claude.json` beside `~/.claude`. |
| `configHome.storageEnv` | Optional. A variable set to your own value of `configHome.env`, empty when unset, for a CLI that keys stored sign-in to its directory, such as `CLAUDE_SECURESTORAGE_CONFIG_DIR` for Claude Code's macOS keychain entry. |
| `configHome.trust.file` | Optional. The `private` JSON file the CLI records trusted workspaces in. |
| `configHome.trust.path` | The keys, from the top of that file, to the value set to `true` for a trusted workspace; `{dir}` stands for the workspace's absolute path with forward slashes. |

### `session`

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `session.assign` | string | yes | `aviary` when Aviary generates the ID and passes it (Claude Code's `--session-id`), `capture` when the CLI mints its own and prints it (Codex), or `none` when it has no resumable session. |
| `session.capture` | regexp | when `assign` is `capture` | Reads the ID out of stdout; its first capture group is the ID. Rejected on the other values. |
| `session.clearOnResumeFailure` | bool | no | Drops the recorded ID when a resume fails because the session is gone, so the restarted run takes a fresh one. |

### `output`

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `output.format` | string | yes | `text` (alias `plain`) shows output as written; `stream-json` (alias `claude-stream-json`) decodes Claude Code's stream; `codex-json` decodes `codex exec --json`; `gemini-json` decodes Gemini CLI's JSON stream; `jsonl` decodes a line-delimited JSON stream described by `output.jsonl`. |
| `output.jsonl` | object | when `format` is `jsonl` | The generic decoder's field paths. Rejected on the other formats. |
| `output.transcript` | string | no | The mapper that turns a `jsonl` stream into a job's structured transcript: `opencode-json`, `cline-json` or `muse-json`. The other formats have mappers of their own. |

Projects keeps a structured transcript for every job, so a CLI whose output Aviary cannot map should declare `projectsUnavailable`, as `agy` does.

A **path** is dot-separated object keys, each optionally suffixed with `[]` to step into every element of an array: `message.content[].text` reads the text of every content block. A path that names nothing in an event contributes nothing, so an unfamiliar event is passed over rather than failing the stream, and a line that is not JSON is passed through as written.

| Field | Type | Description |
| --- | --- | --- |
| `jsonl.type` | path | Where the event's type is. Needed by `ignore` and `events`. |
| `jsonl.text` | path | The readable text an event carries. |
| `jsonl.toolName` | path | A tool call's name, rendered as a `Running: ...` line. |
| `jsonl.toolInput` | path | The tool call's input. Needs `toolName`; paths sharing a prefix are paired element by element. |
| `jsonl.toolNamePrefix` | string | Only a name starting with this is a call, and the prefix is trimmed. Needs `toolName`. |
| `jsonl.ignore` | string[] | Event types dropped before anything is read from them. |
| `jsonl.textDelta` | bool | Each event's text is a fragment of a message: fragments are joined, and the line ends on the first event with no text. Needs `text`. |
| `jsonl.events.<type>` | object | Per-type overrides of `text`, or of `toolName` with its `toolInput` and `toolNamePrefix`. A type listed in `ignore` cannot also be overridden. |

At least one of `text` or `toolName` is required, on the block or on one of its overrides. `events` is for an envelope that spreads one logical event over several typed events, where one set of paths cannot describe them all; `muse` uses it, with `toolNamePrefix: tool.`, to render `task.lifecycle.proposed` events as tool calls.

### `missingSession`

When a resume fails because the CLI no longer has the session, Aviary restarts the work instead of failing the job, so it has to recognize the message. These phrases are always matched, case-insensitively, anywhere in the output: `no conversation found`, `no session found`, `session not found`, `could not find session`, `unable to find session`. `missingSession` **adds** phrases for one CLI only, which keeps one CLI's wording out of every other CLI's detection.

### `quota`

A CLI that declares no quota reports none, shown as untracked, and costs no process.

| Field | Type | Default | Description |
| --- | --- | --- | --- |
| `quota.reader` | string | `none` | `none`, `codex`, `claude` or `command`. |
| `quota.command` | string[] | | The argv whose stdout is parsed as JSON; `{binary}` is the resolved executable. Needed by `command`. |
| `quota.percentUsed` | path | | The percentage of the window used. |
| `quota.resetAt` | path | | When the window resets. |
| `quota.format` | string | `{percentLeft}% left` | The display template, with `{percentUsed}`, `{percentLeft}` and `{resetAt}`. |
| `quota.timeout` | duration | `30s` | Bounds one read. |

Every field but `reader` belongs to the `command` reader and is rejected on the others, because a quota block that silently does nothing looks exactly like a working one. A failed read keeps the last good reading. A provider whose quota is used up is skipped in favor of the next fallback.

### `promptStdin`

Out-of-band calls, such as the Projects prompt chat and the stuck-job watchdog, send a prompt carrying the instance's jobs and their latest output, which can exceed what an OS command line allows. A definition with `promptStdin` writes the prompt to stdin instead and renders the template's `{prompt}` argument as the listed arguments: `[]` leaves it out (Claude Code's `-p` then reads stdin), and `["-"]` puts a dash there (how `codex exec` reads stdin). Without it, a long prompt is written to a temporary file and the CLI is asked to read that file. Dispatched jobs always pass their prompt as an argument.

### `skills`

`skills.target` is the skills.sh target id `npx skills add --agent` takes: a dedicated one such as `codex`, `claude-code`, `opencode`, `cline` or `gemini-cli`, or `universal` for a CLI skills.sh has no id for. Aviary installs and updates the [skills for providers](/guide/skills/) for the built-in CLIs' targets; a CLI you declare installs its own with `npx skills add`, shown in the tutorial below. Only the id's shape is checked, so a target Aviary has never heard of is yours to pass on.

### `doctor`

The CLI report can answer most questions from the definition. The two it cannot are whether the CLI is signed in and what a provider-agnostic CLI's model list is today; `doctor` names the commands that answer them. Neither is ever run by a dispatch.

| Field | Description |
| --- | --- |
| `doctor.auth` | The argv whose exit status says whether the CLI is signed in. `{binary}` is the resolved executable. |
| `doctor.signedIn` | A regexp the auth output must match, for CLIs that exit zero when signed out. |
| `doctor.signedInJSON` | The same as a JSON path that must be `true`, such as `loggedIn`. |
| `doctor.authDetail`, `doctor.authDetailJSON` | How the CLI is signed in, read from the auth output, so the report can say "signed in (claude.ai)". |
| `doctor.models` | The argv that lists models, one per line or as JSON. |
| `doctor.modelPattern` | A regexp narrowing which lines are models; its first capture group, when it has one, is the id. |
| `doctor.modelsJSON` | Where the ids are in JSON output, with `[]` walking an array and `[key=value]` walking only matching elements, such as `models[visibility=list].slug`. |
| `doctor.modelsStdin` | Lines written to the models command's stdin, for a CLI that only answers over a stdio protocol. The probe stops reading at the first reply that carries models. |
| `doctor.knownModels` | Models the definition carries for a CLI that cannot be asked, reported as known rather than validated. |
| `doctor.modelsNote` | Replaces the label on the models field, such as the caveat on a known list. |
| `doctor.timeout` | Bounds one probe (`20s`). A hung CLI is reported as unknown. |

A probe must be non-interactive and change nothing. A CLI whose only sign-in check starts a device-code flow has no usable probe and is better left undeclared, so the report says the state is unknown. Without `doctor.auth`, a CLI whose quota could be read is reported as signed in. A probe that ran and listed nothing is reported as one that could not list, naming the command, not as a CLI that accepts anything.

## Tutorial: adding a CLI Aviary has never heard of

Say the CLI is called `robo` and takes a prompt as a positional argument.

### 1. Find out what a headless run looks like

Answer these by running the CLI:

1. **What is the non-interactive invocation?** It must not open a TUI or need a terminal.
2. **How does it run without asking for approval?** A job cannot answer a permission prompt, and a CLI whose non-interactive default is to auto-reject cannot reach the clone `gh-fix` works in. Find its flags for each approval mode.
3. **Does it have a resumable session?** It accepts an ID you give it, prints one you can read back, or has neither.
4. **What does it print?** Plain text, or line-delimited JSON. Projects jobs need a machine-readable stream.
5. **Which skills.sh id does it have?** `universal` if none.

### 2. Write the minimal definition

`binary`, `args.run`, `session` and `output` are all that is required:

```yaml
models:
  cli_definitions:
    robo:
      binary: robo
      session: { assign: none }
      output: { format: text }
      args:
        run:
          - args: [run]
          - args: ["{prompt}"]
```

Run `aviary doctor` and `aviary models clis` to check it loaded. `robo` appears in the list, and an agent with `model: cli/robo` runs on it.

### 3. Map the approval modes

```yaml
    robo:
      approval:
        read-only: [--plan]
        auto: [--auto-approve]
        full: [--no-sandbox]
      args:
        run:
          - args: [run]
          - args: ["{approval}"]
          - args: ["{prompt}"]
```

### 4. Add the model and level

Guard each on its own value, so a call that names neither passes neither:

```yaml
          - when: model
            args: [--model, "{model}"]
          - when: level
            args: [--effort, "{level}"]
```

Add `levels: [low, medium, high]` when the CLI accepts a fixed set, so a bad level is rejected with the list when the model is configured rather than by `robo` on the next run.

### 5. Handle sessions

If `robo` accepts an ID you generate:

```yaml
      session: { assign: aviary }
      args:
        run:
          - args: [run]
          - when: session
            args: [--session-id, "{session}"]
          - args: ["{prompt}"]
        resume:
          - args: [run, --resume, "{session}"]
          - args: ["{prompt}"]
```

If it mints its own and prints it, capture it, and drop it when a resume fails:

```yaml
      session:
        assign: capture
        capture: '(?i)session:\s*([0-9a-f-]{36})'
        clearOnResumeFailure: true
```

With neither, keep `assign: none`: a recovery re-runs `args.run` with the recovery prompt.

### 6. Decode its output

If `robo` streams events like these:

```json
{"kind": "chunk", "delta": {"text": "Reading the issue"}}
{"kind": "tool", "call": {"name": "shell", "arguments": {"cmd": "go test ./..."}}}
{"kind": "usage", "tokens": 812}
```

describe where the fields are instead of writing a decoder:

```yaml
      output:
        format: jsonl
        jsonl:
          type: kind
          text: delta.text
          toolName: call.name
          toolInput: call.arguments
          ignore: [usage]
```

Without a transcript mapper for its stream, add `projectsUnavailable` with the reason until one exists, so Projects does not dispatch jobs it cannot record.

### 7. Add quota, if it reports any

If `robo usage --json` prints `{"limits": {"used_pct": 41, "resets": "2026-09-04T00:00:00Z"}}`:

```yaml
      quota:
        reader: command
        command: ["{binary}", usage, --json]
        percentUsed: limits.used_pct
        resetAt: limits.resets
        format: "{percentLeft}% left, resets {resetAt}"
        timeout: 10s
```

### 8. Let the report check sign-in and models

If `robo whoami` exits non-zero when signed out and `robo models` prints one model per line:

```yaml
      doctor:
        auth: ["{binary}", whoami]
        models: ["{binary}", models]
```

The models then appear in System → Providers & Auth and `aviary models clis` as names you can paste into an agent's model. Add `signedIn` if `whoami` exits zero while signed out, `modelPattern` if the list is decorated, `modelsJSON` for JSON output, and `modelsStdin` for a CLI that only answers over a stdio protocol.

### 9. Install the skills for it

Jobs send `/gh-fix owner/repo#N`, so `robo` needs the skills. Declare `skills: { target: universal }`, then install them, since Aviary's automatic installs cover the built-in CLIs only:

```sh
npx skills add lsegal/aviary@gh-fix --global --agent universal -y
npx skills add lsegal/aviary@gh-discuss --global --agent universal -y
npx skills add lsegal/aviary@gh-create --global --agent universal -y
npx skills add lsegal/aviary@gh-safe-merge --global --agent universal -y
```

### 10. Override a built-in instead

Pointing `claude` at another install and adding an environment variable is the whole entry:

```yaml
models:
  cli_definitions:
    claude:
      binary: /opt/claude/bin/claude
      env:
        CLAUDE_CODE_PRINT_BG_WAIT_CEILING_MS: "0"
        MY_VAR: "1"
```

Everything else stays the built-in's. Use `null` to remove something: `codex: { quota: null }`.

## Troubleshooting

**A definition is rejected.** The error names the agent and the field, for example:

```text
cli_definitions: agent "robo": field "session.assign": "sometimes" must be "aviary", "capture", or "none"
cli_definitions: agent "robo": unknown field "arg"
cli_definitions: agent "robo": field "args.run"[2].args: unknown placeholder {prmpt}; known placeholders are {approval}, {image}, {level}, {model}, {prompt}, {session}, {sessionName}, {settings}
```

Run `aviary doctor` after every edit; it loads the same definitions jobs and System → Providers & Auth do.

**A dispatch failed naming a version.** The definition's `minVersion` is newer than the installed CLI. Upgrade it, or set `binary` to a newer install. A warning that the version could not be read means the run went ahead; if it then fails on an unrecognized argument, check the version by hand.

**A resume restarted instead of continuing.** The CLI no longer holds the session (matched by the shared phrases plus `missingSession`), or its definition has `session.assign: none`. The board shows the restart, and the job's log says so. This is expected for `opencode`, `cline` and `agy`.

**The report says `not installed` or `unknown`.** `not installed` means `binary` is not on `PATH`; install the CLI or set `binary` to its path. `unknown` sign-in means there is no `doctor.auth` probe and no quota reading, which is not a claim the CLI is signed out. For models, it means the CLI's own command failed; run that argv by hand.

**No quota is shown.** That is the default without a `quota` block. With `reader: command`, run the command by hand and check its stdout is JSON with the paths `percentUsed` and `resetAt` name.

**The skills are missing for a CLI.** Check **System › Skills** › **Installed in providers** for a built-in CLI. For a CLI you declared, install them with `npx skills add` for its `skills.target`.
