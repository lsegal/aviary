# Skills for providers

Aviary ships four skills for providers such as Claude Code, Codex and Gemini CLI. Code Loop runs them: Aviary decides which issue or Discussion to work on, in which CLI and when, and the skill does the rest. They are ordinary [skills.sh](https://skills.sh/) skills installed into each CLI, so they also work on their own, with no Aviary at all.

::: tip Use them without Aviary
Every skill has its own landing page under [Skills](/skills/) with copy-paste install commands for Claude Code, Codex, Gemini CLI and OpenCode. You don't need Aviary to use them.
:::

| Skill | What it does | Who runs it |
| --- | --- | --- |
| [`gh-fix`](./gh-fix) ([landing page](/skills/gh-fix/)) | Fixes a GitHub issue end to end: draft PR, implementation, tests, changelog, CI repair and merge. | Code Loop, for issues on watched repositories and boards |
| [`gh-discuss`](./gh-discuss) ([landing page](/skills/gh-discuss/)) | Researches one new GitHub Discussion and posts a single answer, or stays silent. Read-only. | Code Loop, for watched Discussions boards |
| [`gh-safe-merge`](./gh-safe-merge) ([landing page](/skills/gh-safe-merge/)) | Accepts an unowned pull request, such as an outside contribution: a safety check gates everything, then CI goes green and it merges. Not a code review. | You |
| [`gh-create`](./gh-create) ([landing page](/skills/gh-create/)) | Files a well-formed GitHub issue from a short request. | You. Code Loop never runs it, but the issues it files are what Code Loop picks up. |

These skills are separate from the skills Aviary's own agents use. They live in the repository under `.agents/skills/`.

## Installing and Updating

**System › Skills** in the control panel lists these skills with every other installed skill. Under **Installed in providers** it shows the installed version of each skill in each CLI provider, with **Install**, **Update** and **Update all**. Aviary installs through `npx skills add`, so Node.js must be on the `PATH`.

To install all four from the command line, run:

```sh
aviary install-skills
```

It installs `gh-create`, `gh-discuss`, `gh-fix` and `gh-safe-merge` globally with `npx skills`: into `~/.agents/skills`, where Aviary and the CLIs that read `.agents/skills` find them, and into the skills directory of every CLI provider on the `PATH`. Skills already installed are refreshed rather than duplicated, so it is safe to run again. The [install script](../getting-started) runs it right after installing the binary (pass `--skip-skills` or set `AVIARY_SKIP_SKILLS=1` to skip it), and Aviary runs the same install automatically on its first launch, so a fresh install can work GitHub issues with no manual step; if that install fails, for example with no network, Aviary tries again at the next start.

By default Aviary keeps the skills current on its own:

- **Update with Aviary.** At startup, including the restart after an upgrade, Aviary reinstalls any skill whose installed copy differs from the one the running release was built with. Each release installs the skills from its own tag, so they match what it was tested against.
- **Install for new CLIs.** When a CLI provider shows up later, Aviary installs the skills for it too.

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

Use `--agent codex`, `--agent gemini-cli`, `--agent opencode` or `--agent cline` for other CLIs, or repeat `--agent` to install for several at once. The GitHub CLI can install the same skill:

```sh
gh skill install lsegal/aviary gh-fix --allow-hidden-dirs --agent claude-code --scope user
```

Then run it in the CLI:

```text
/gh-fix owner/repo#123
```

When Aviary dispatches a skill it appends `identity:/aviary:<ID>` so that comments mentioning `@/aviary:<ID>` reach the right job. Without Aviary the identity is simply left out.
