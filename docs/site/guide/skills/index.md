# Skills for Coding CLIs

Aviary ships four skills for coding CLIs such as Claude Code, Codex and Gemini CLI. Projects runs them: Aviary decides which issue or Discussion to work on, in which CLI and when, and the skill does the rest. They are ordinary [skills.sh](https://skills.sh/) skills installed into each CLI, so they also work on their own, with no Aviary at all.

| Skill | What it does | Who runs it |
| --- | --- | --- |
| [`gh-fix`](./gh-fix) | Fixes a GitHub issue end to end: draft PR, implementation, tests, changelog, CI repair and merge. | Projects, for issues on watched repositories and boards |
| [`gh-discuss`](./gh-discuss) | Researches one new GitHub Discussion and posts a single answer, or stays silent. Read-only. | Projects, for watched Discussions boards |
| [`gh-safe-merge`](./gh-safe-merge) | Accepts an unowned pull request, such as an outside contribution: a safety check gates everything, then CI goes green and it merges. Not a code review. | You |
| [`gh-create`](./gh-create) | Files a well-formed GitHub issue from a short request. | You. Projects never runs it, but the issues it files are what Projects picks up. |

These skills are separate from the skills Aviary's own agents use. They live in the repository under `.agents/skills/`.

## Installing and Updating

**System › Skills › For coding CLIs** in the control panel shows the installed version of each skill in each coding CLI, with **Install**, **Update** and **Update all**. Aviary installs through `npx skills add`, so Node.js must be on the `PATH`.

By default Aviary keeps the skills current on its own:

- **Update with Aviary.** At startup, including the restart after an upgrade, Aviary reinstalls any skill whose installed copy differs from the one the running release was built with. Each release installs the skills from its own tag, so they match what it was tested against.
- **Install for new CLIs.** When a coding CLI shows up later, Aviary installs the skills for it too.

Turn either off on the same tab, or in `aviary.yaml`:

```yaml
coding_skills:
  auto_update: false      # don't reinstall outdated skills at startup
  install_new_clis: false # don't install skills for CLIs found later
```

A job that is already running keeps the skill text it started with. Its CLI read the skill when the job began, so an update only affects jobs that start afterwards.

## Using Them Without Aviary

Install a skill once for your CLI:

```sh
npx skills add lsegal/aviary@gh-fix --global --agent claude-code -y
```

Use `--agent codex`, `--agent gemini-cli`, `--agent opencode` or `--agent cline` for other CLIs, or repeat `--agent` to install for several at once. Then run it in the CLI:

```text
/gh-fix owner/repo#123
```

When Aviary dispatches a skill it appends `identity:/aviary:<ID>` so that comments mentioning `@/aviary:<ID>` reach the right job. Without Aviary the identity is simply left out.
