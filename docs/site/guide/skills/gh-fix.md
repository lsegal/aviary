# gh-fix

`gh-fix` takes ownership of a GitHub issue and drives the whole delivery loop: code, proof, CI, merge, and the loose ends after it. Give it an issue and it carries the ticket across the finish line.

```text
/gh-fix owner/repo#123
```

[Landing page](/skills/gh-fix/) · [Read the skill](https://github.com/lsegal/aviary/tree/main/.agents/skills/gh-fix/SKILL.md)

This skill works on its own, with no Aviary at all. Aviary just runs it for you.

## How It Works

1. **Read the full issue and its context.** It checks the request, the discussion history, linked work and direct `@/aviary:<ID>` instructions before acting.
2. **Open a draft PR before writing code.** It works in a fresh clone and makes progress visible right away with a linked draft pull request.
3. **Build, test, and show the finished UI.** It adds focused tests and a changelog note, and captures screenshots or a recording for UI work.
4. **Stabilize CI, merge, and close follow-ups.** It follows every required check, fixes what its change breaks, and merges only when GitHub agrees.

## Draft Means Protected Progress

A draft pull request is the workbench, not a review request. It links the issue, records the branch, and gives everyone a live place to see what is underway. While the work changes, `gh-fix` pushes checkpoint commits at least every five minutes. If the session stops, the next run resumes from the same branch instead of recreating the work.

## Nothing Falls Through

The loop runs from issue, to draft PR, to proof, to green CI, to merge, and on to follow-ups:

- **Context stays in scope.** It reads the issue, comments, linked work and direct instructions before it starts.
- **UI changes get shown.** Browser work earns representative screenshots or a recording in the pull request.
- **Nothing gets stranded.** Actionable leftovers become linked follow-up issues, routed back into the team's flow. Checks only a person can run, like testing in a desktop app or on real hardware, go into a **Manual verification** checklist comment instead of an issue no agent can close.
- **Questions are marked.** When the work is blocked on an answer only a person can give, it asks on the issue and labels it `answer-needed`. Steps it can do with its own access, such as dispatching a workflow, it does instead of leaving them for you.

## No Drive-By Patches

It merges only after the checks pass. The skill watches the current commit's checks and reads failures precisely before making the smallest correct change. Transient failures get one rerun; genuine blockers are documented plainly on the pull request. A `do-not-merge` label, or a comment asking it not to merge, leaves the finished pull request open for a person.

## Proof People Can Inspect

For browser work, the finished pull request includes a representative screenshot or recording. The caption says what changed, and the proof names the state, interaction and check that make it trustworthy.

## Install

```sh
npx skills add lsegal/aviary@gh-fix --global --agent claude-code -y
```

See [Skills for providers](./) for other CLIs and for how Aviary keeps the skill current.
