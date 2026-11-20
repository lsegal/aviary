# gh-safe-merge

`gh-safe-merge` accepts a pull request nobody on your side owns, typically an outside contribution. It checks that the change is safe and does what it says, then drives it through CI to a merge. It is not a code review.

```text
/gh-safe-merge owner/repo#123
```

[Read the skill](https://github.com/lsegal/aviary/tree/main/.agents/skills/gh-safe-merge/SKILL.md)

## How It Works

1. **Run the safety check before anything runs.** Nothing from the pull request is built, tested, installed or run until the check passes.
2. **Mark a draft ready.** Once the check passes, a draft pull request is marked ready for review so it can merge. A pull request that fails the check stays a draft.
3. **Unblock CI and run it to green.** Only after the check passes does it approve workflow runs GitHub is holding for the pull request, and only for the commit it checked.
4. **Finish superficially.** It resolves merge conflicts, applies formatting and auto-fixable linter passes, and adds a changelog entry and any README or docs updates the change plainly requires.
5. **Run CI to completion.** Every required check must pass on the final head, including its own finishing commits.
6. **Merge, unless told not to.** The merge is the acceptance; a safe, working pull request merges with no comment.

## The Safety Check Gates Everything

Running untrusted code is the risk itself, so CI is never unblocked before the check passes. The check reads the description, the full diff and every commit for:

- **Intent.** The code does what the pull request says and nothing beyond it. No unrelated behavior rides along.
- **Security.** No new injection, exfiltration, privilege or unsafe-default surface.
- **Malicious behavior and backdoors.** No hidden functionality, covert network calls, credential harvesting, obfuscated payloads or unexplained binary blobs.
- **Supply chain.** New dependencies are legitimate, spelled correctly and pinned. Install scripts, build hooks and CI workflow changes get extra scrutiny, above all anything that reads secrets or widens permissions.
- **Licensing.** No code or asset under an incompatible license, and no unattributed copied code presented as original.
- **General properness.** The contribution is what it claims to be, not spam, vandalism or nonsense dressed up as a change.

If the author pushes new commits mid-run, the check runs again on them before anything else happens.

## Merged, Handed Back or Closed

Every comment the skill posts mentions the pull request's author, and it posts at most one per outcome.

- **Safe and working.** CI goes green and the pull request merges with no comment.
- **Merely incorrect.** Behavior that contradicts the description, a substantive problem, or tests failing on the pull request's own code goes back to the author: one comment says what is wrong and, briefly, what would fix it. The pull request is not merged and not closed.
- **Malicious.** The pull request is closed at once with a comment. Its code never runs and its CI is never approved.

Closing is reserved for bad faith; incorrect is not malicious. Holds work the way they do in [`gh-fix`](./gh-fix): a `do-not-merge` label, wording in the pull request or its comments, or `and do not merge` stops it short of the merge, with one comment saying it cleared the safety check and CI and naming what held it. Being a draft is not a hold.

## Not a Review

Style, structure, naming and taste are ignored. It never requests changes, never submits a review and never leaves review-style comments. If the code is safe, works and does what the pull request says, it is accepted. Acceptance is the default.

It finishes a pull request rather than rewriting it. Anything more substantive than the finishing changes above goes back to the author. When the author has not allowed maintainers to edit their branch, the skill pushes the finishing changes to a new branch with the author's commits intact, opens a replacement pull request and closes the original with a comment linking it.

## Install

```sh
npx skills add lsegal/aviary@gh-safe-merge --global --agent claude-code -y
```

See [Skills for providers](./) for other CLIs and for how Aviary keeps the skill current.
