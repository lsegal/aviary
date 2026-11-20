# gh-create

`gh-create` turns a quick request into a GitHub issue another person or agent can act on, and files it the same way no matter which CLI you asked.

```text
/gh-create owner/repo the save button crashes on empty files, see ./crash.png
```

[Read the skill](https://github.com/lsegal/aviary/tree/main/.agents/skills/gh-create/SKILL.md)

## How It Works

1. **Check the repository and its visibility.**
2. **Research, if it helps.** This step is optional.
3. **Write, label, assign and attach.**
4. **File it, then check how it renders.**

## Expanded, but Never Rewritten Away

The agent writes a structured issue: a summary, steps to reproduce or numbered requirements, expected and actual behavior, and acceptance criteria. Your original prompt is quoted word for word at the bottom, so the issue can be reproduced, replayed and audited.

It applies every label the repository already has that fits, such as kind, platform and area, and never invents one. It assigns the issue to you with `@me` unless you ask for someone else or for nobody.

When a UI change has states worth seeing, it draws annotated wireframes of them. When systems interact in ways prose can't carry, it adds a deployment or sequence diagram with a written explanation. It skips both when words are enough.

When an Aviary job files an issue, the issue ends with a `**Created by Aviary:**` line naming the job's identity, CLI and model.

## Media That Renders, Secrets That Don't

- **Media actually attached.** Screenshots and videos you mention are uploaded with `gh issue create --attach`, or GitHub's upload endpoint as a fallback. The filed issue is read back to confirm no local path is left behind and no image appears twice.
- **Private data scrubbed.** Local paths, names, emails, hostnames and tokens stay out, even in the quoted prompt. A public issue never mentions a private repository or its code.
- **Research is optional.** With the repository at hand it can point to the files involved and link related issues, but it never holds up filing.

## Install

```sh
npx skills add lsegal/aviary@gh-create --global --agent claude-code -y
```

See [Skills for Coding CLIs](./) for other CLIs and for how Aviary keeps the skill current.
