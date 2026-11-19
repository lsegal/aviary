# gh-discuss

`gh-discuss` is the careful counterpart to [`gh-fix`](./gh-fix): it researches one unanswered GitHub Discussion and replies only when the repository supports a complete, confident answer.

```text
/gh-discuss 123
```

[Read the skill](https://github.com/lsegal/aviary/tree/main/.agents/skills/gh-discuss/SKILL.md)

## How It Works

1. **Validate the single thread.**
2. **Read the question, not the noise.**
3. **Research only inside the repository.**
4. **Reply once, or leave it alone.**

## Helpful Without Making a Mess

It does not execute code, change files, open branches, or look through unrelated sources. Its only possible write is one top-level answer to one Discussion.

Already answered? Not actually a question? The repository can't settle it? It exits quietly. That restraint is the feature.

- **The thread stays small.** It ignores nested replies and never adds a second top-level comment.
- **Repository evidence only.** Answers come from the project's own files, docs and GitHub history, not guesswork.
- **Read-only by design.** No builds, no commits, no mutations. A warranted answer is the entire outcome.

## Install

```sh
npx skills add lsegal/aviary@gh-discuss --global --agent claude-code -y
```

See [Skills for Coding CLIs](./) for other CLIs and for how Aviary keeps the skill current.
