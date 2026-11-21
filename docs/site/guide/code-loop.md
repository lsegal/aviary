# The Code Loop on GitHub

This page follows one issue through its whole life in [Projects](./projects): from the moment a repository is watched to the merged pull request that closes the issue. Each stage says what happens, where it is configured, and where you watch it in Aviary.

The loop has two halves. Aviary decides **which** issue runs, **on which provider**, and **when**, and keeps the job alive. The [`gh-fix`](./skills/gh-fix) skill, running inside the provider, does the work on GitHub: the branch, the draft pull request, the fix, CI and the merge.

```text
watch ─► pick up ─► claim ─► clone, branch, draft PR ─► implement ─► CI green ─► merge ─► issue closed
```

## 1. A target is watched

A project is a watched target that belongs to one Aviary agent: a repository (`owner/repo`), a GitHub Project board, or a Discussions board. The agent's model and fallbacks decide which provider the project's jobs run on.

- **Configure:** the agent's tab in **System → Projects** (`/system/projects?agent=<agent>`), `aviary project add owner/repo --agent coder`, or the agent's `projects:` section in `aviary.yaml` ([`agents[].projects`](/reference/config#agents-projects)). `aviary watch owner/repo` watches a target for the life of the command without saving anything.
- **Delivery:** `poll` (the default) reads GitHub through `gh` every `interval`; `webhook` adds repository webhooks through an ngrok tunnel and still polls as a fallback.
- **Observe:** the agent's **GitHub Projects** page is one board of all its projects' jobs, and each project's own board opens beneath it; `aviary project status` reports every project.

## 2. An issue matches and is picked up

On every poll, Aviary looks for issues that are ready for work:

- A repository issue is open, carries the ready label (`ready_label`, `ready` by default) and matches the issue filter (`filter`, `assignee:@me` by default). On a project board, the item's status is the ready state (`ready_state`, `Todo` or `Ready`).
- Issues labeled `critical`, then `high`, go first, across all projects.
- An issue with open sub-issues is a tracking issue and is skipped.
- An issue blocked by open dependencies waits. Blocked by exactly one issue that already has an open pull request, on a repository that allows stacked pull requests, it runs stacked on that pull request instead.
- `concurrency` caps how many jobs run at once in total, across every project of every agent. It and the other settings named on this page live in the one shared [`projects:` section](/reference/config#projects), edited on System → Projects.

The job is dispatched to the first provider in the agent's model and fallbacks whose quota is not used up. Its prompt is the skill call itself, for example:

```text
/gh-fix owner/repo#123 identity:/aviary:<ID>
```

The shared [projects settings](/reference/config#projects) add to it: `and do not merge` when `no_merge` is on, `and no changelog` when `no_changelog` is on, and `and use fork <owner>/<repo>` when your `gh` account cannot write to the repository. The prompt also names the clone directory the job must use (from `temp_dirs`) and the allowed commenters.

- **Observe:** the issue appears as a job on the board; a board item moves to `In Progress`. `aviary project job list --status active` lists running jobs.

## 3. Ownership is claimed

Several Aviary instances may watch the same repository. Every instance has a random identity, and they coordinate through signed comments on the issue so two instances never work one issue:

- Before starting, an instance posts **Starting work on this issue**, signed `/aviary:<ID>`. The most recent claim wins: an instance that sees a newer claim from another identity backs off.
- A resumed job posts **Continuing work on this issue**; a live owner answers **I am working on this**.
- A graceful shutdown posts **Stopping work on this issue**, and an instance that gives up an issue posts **Releasing this issue**. Claims are counted, so work is free once every claim on it has been stopped or released, and another instance picks it up at once.
- An instance that was killed leaves its claim standing. When the newest claim from another instance is more than five minutes old, a periodic reap asks **Does anyone have this?** and waits at least two minutes for an answer before claiming the work.

A comment that mentions `@/aviary:<ID>` addresses one instance directly; see [Steering a running job](#steering-a-running-job).

- **Observe:** the claim comments on the issue, and the claim events in the job's transcript (`aviary project job get <id>`).

## 4. An isolated clone, a branch and a draft pull request

`gh-fix` never touches your own checkout. It validates the issue and reads the whole thread, then:

1. Creates a branch named `fix/issue-<N>-<slug>`, from the default branch, or from the blocker's branch when the job is stacked.
2. Opens a **draft pull request** linked to the issue with `Closes #<N>`, before writing any code. Its body carries a `## Plan` checklist of the steps ahead.
3. Clones the repository into the clone directory Aviary named, and reports the path back to Aviary with an `AVIARY_CHECKOUT_DIRECTORY=` line.

A job that is interrupted and dispatched again finds the open pull request for the issue, clones its branch, and carries on rather than starting over.

- **Configure:** `temp_dirs` picks where clones go.
- **Observe:** the job card links the draft pull request; `aviary project clone list` lists clones, and `aviary project clone clear` removes the ones no job uses.

## 5. The provider implements the fix

Inside the clone, `gh-fix` reproduces the problem when it can, makes the smallest complete fix, adds focused tests that fail without it, and adds a changelog entry when the repository keeps a changelog (unless told `and no changelog`). For UI changes it attaches screenshots or a recording to the pull request.

While it works it pushes a checkpoint commit at least every five minutes, marked so CI skips it, and checks off the plan. The final commit carries `Closes #<N>`, and the pull request is marked ready for review.

- **Configure:** the provider's [approval mode](/reference/config#approval) (`projects.approval_mode`, or `approval.mode`) decides how much the CLI may do unattended; each [CLI provider definition](/reference/cli-definitions) translates it into the CLI's own flags.
- **Observe:** the job's live session log on the board, and its structured transcript (messages, tool calls, shell commands, file changes). **Summarize** on a job card asks the Projects agent for a summary.

## 6. CI runs and failures are repaired

`gh-fix` watches the checks on the pull request's current head until every required check completes. A failure caused by the change is reproduced, repaired with the smallest correct change, and pushed; a merge conflict is resolved by updating the branch; a clearly transient failure gets one rerun. It never weakens tests to get a green build.

While a job waits on CI, the board shows it as waiting. A project with [reports](./projects#reports) can send a `waiting` report through an agent's channels.

## 7. The pull request merges and closes the issue

When every required check passes on the head that was tested, `gh-fix` merges with the repository's merge method, preferring squash, deletes the branch, and confirms that GitHub closed the issue through `Closes #<N>`. A board item moves to `Done`. Leftover work becomes linked follow-up issues; checks only a person can run go into a **Manual verification** checklist comment instead.

- **Observe:** the job finishes as merged; a `merged` report goes out when reports are on.

### Endings that are not a merge

- **Merge holds.** A merge-hold label on the issue (`do-not-merge`, `donotmerge`, `no-merge`, `hold` or `needs-review`, in any spelling), wording in the issue or its comments asking that the fix not be merged, or the shared `no_merge` setting stops the job short of the merge. Everything else still happens: the pull request is finished, marked ready and green, and `gh-fix` posts one comment saying it is ready and naming what held it. Follow-ups are listed in that comment rather than filed.
- **Fork.** When your `gh` account cannot write to the repository, the branch is pushed to your fork and the pull request opened from it. The job ends with the pull request ready and green and leaves the merge to the maintainers.
- **Stacked.** A stacked pull request cannot merge while its blocker is open. It ends ready and green, paused on the blocker, and gives up its slot; Aviary dispatches it again when the blocker's pull request merges.
- **Blocked on an answer.** When the work needs a decision only a person can make, `gh-fix` asks on the issue, labels it `answer-needed`, and stops. An answer in a comment removes the label on the next run.
- **Closed or rejected.** Closing the issue stops its job. A pull request closed without merging restarts the work.

## The supporting cast

### Steering a running job

Comments by allowed commenters (`allowed_commenters`, the signed-in `gh` user by default) on the issue or its pull request are instructions. New comments, edits to the issue description, and closures are relayed into the running session, so you steer a job by commenting on GitHub. Comments by anyone else are treated as information, not instructions.

A comment that mentions `@/aviary:<ID>` starts a fresh `gh-fix` pass on that instance. On a finished or paused job, the mention goes to the Projects agent, which decides whether to retry or resume it. See [Steering jobs](./projects#steering-jobs).

### Prompt chat

**Prompt chat** on the board talks to the built-in `projects` agent with a live snapshot of this instance's projects and jobs. It answers questions such as "status of issues?", retries, pauses, resumes or stops jobs, runs an unblock round, files issues through `gh-create`, and posts comments on issues or pull requests that the job working them also hears. See [Prompt chat](./projects#prompt-chat).

### Unblocking stuck jobs

- A run that exits while its issue is still open is resumed in the same session, or restarted with a recovery prompt when the session is gone.
- Resumes back off from 30 seconds to 15 minutes. Four in a row that change nothing on GitHub pause the job until you retry it.
- The stuck-job watchdog runs every `unblock_interval` (15 minutes) and makes one agent call (`unblock_agent`, `unblock_model`) that unblocks paused and silent jobs. The prompt chat, or the `project_unblock_run` tool, runs a round now.
- On shutdown, restart or upgrade, every job's work is committed and pushed first, and the jobs resume afterwards.

See [Keeping jobs moving](./projects#keeping-jobs-moving).

### Scheduled tasks on projects

A project can also run prompts on a timer, such as a nightly triage that labels issues `ready` for the loop to pick up. These are Aviary scheduled tasks of type `project`: each run takes a slot on the board and runs the project's agent in an isolated clone with the task's prompt in place of `/gh-fix`. Manage them in the project's **Schedules** tab. See [Scheduled prompts](./scheduled-tasks#scheduled-prompts).

## Where to go next

- [Projects](./projects) covers every setting the loop reads.
- [CLI provider definitions](/reference/cli-definitions) describes how Aviary launches each CLI a job runs on, and how to add one.
- [`gh-fix`](./skills/gh-fix) describes the skill's side of the loop.
- [Projects tools](/reference/mcp/projects) lists the MCP tools and `aviary project` commands that drive it.
