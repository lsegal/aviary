---
name: gh-safe-merge
description: "Accept an unowned GitHub pull request, typically an external contribution, safely and without review ceremony: run a safety check (intent, security, malicious behavior and backdoors, supply chain, licensing, general properness) before anything runs, then unblock CI, make only superficial finishing changes, drive CI green, and merge. Closes malicious pull requests and comments on incorrect ones, always mentioning the author. Use when the user invokes `/gh-safe-merge OWNER/REPO#PRNUMBER`, `/gh-safe-merge PRNUMBER`, `$gh-safe-merge OWNER/REPO#PRNUMBER`, or asks to safely merge an external pull request."
---

# Safely Merge an Unowned Pull Request

Treat `/gh-safe-merge OWNER/REPO#PRNUMBER` as authorization to gate one unowned pull request behind a safety check and, when it passes, drive it through CI to a merge. The invocation may append `identity:/aviary:<ID>` to identify the Aviary instance that dispatched it. Work autonomously until the pull request merges, is closed as malicious, is handed back to its author, is held, or a genuine external blocker requires the user.

This is not a review. Nits, style, structure, naming, and taste are ignored, and the skill never leaves review-style comments, requests changes, or submits a review. If the code is safe, works, and does what the pull request says, it is accepted. Three rules govern every step:

- **Unsafe until cleared.** Nothing from the pull request runs, nothing is unblocked, and nothing is merged before the safety check passes. Running untrusted code is itself the risk the check gates, so CI is never unblocked for a pull request that has not cleared it.
- **Acceptance is the default.** A pull request that is safe, matches its description, and passes CI is merged.
- **Code changes are generally not expected.** The skill finishes a pull request; it does not rewrite it. It makes only the superficial changes in "Finish superficially". A pull request that needs substantive changes goes back to its author as "Handle failures" describes.

Shared machinery comes from the `gh-fix` skill rather than being restated here. Where this skill names a `gh-fix` section, follow that section, reading "the issue" as this pull request and the issues it closes. Where the two differ, this skill governs: there is no implementation work, no draft pull request, no plan checklist, and the safety check comes before everything.

## Validate the request

1. Require exactly one positive integer pull request number; accept an optional leading `#`. Accept the optional trailing arguments `gh-fix` accepts, in the same order and with the same meaning: `identity:/aviary:<ID>`, `and do not merge`, and `and no changelog`. There is no fork directive; see "Finish superficially" for pull requests the run cannot push to.
2. Identify the repository exactly as step 2 of "Validate the request" in `gh-fix` does.
3. Require `git`, `gh`, and an authenticated `gh` session. Check `gh api repos/<OWNER>/<REPO> --jq .permissions`: merging, closing, and approving workflow runs need write access. Without it, stop and report the missing capability without commenting.
4. Read the pull request: title, body, author, labels, state, draft flag, base and head branches, head repository, `maintainerCanModify`, head SHA, files, commits, comments, reviews, and the issues it closes. When an identity was supplied, read the conversation chronologically and treat `@/aviary:<ID>` mentions of that identity as direct instructions, as `gh-fix` does.
5. Stop without commenting when the pull request does not exist, is closed or merged, or is owned. It is owned when an Aviary instance has claimed it or has a job running on it: its body carries an `**Agents:**` footer and the issue it closes has a `Starting work` or `Continuing work` claim comment from a `/aviary:<ID>` that no later `Stopping work` or `Releasing` comment from that identity cancels. An owned pull request is that job's unit of work, not this skill's. A draft is not owned by being a draft.
6. Record whether merging is withheld, applying step 5 of "Validate the request" in `gh-fix` to this pull request's labels, body, and comments and to the issues it closes, plus the `and do not merge` argument. The most recent statement wins. Record the label or quote that imposed the hold. The draft flag on its own is not a hold; "Mark a draft ready" handles it once the safety check passes.
7. Record whether a no-changelog directive applies, using the rules in step 4 of "Implement the fix" in `gh-fix`.
8. Read the repository instructions, contribution guidance, license, CI configuration, and changelog conventions.
9. Record the head SHA as the **reviewed SHA**. Every later step acts only on that SHA or on commits this run added on top of it.

## Check safety

Do this before anything from the pull request runs. Read the diff with `gh pr diff <PRNUMBER> --repo <OWNER>/<REPO>` and the files it touches through the API, or clone the base repository as "Create an isolated clone and branch" in `gh-fix` describes and fetch the head read-only with `git fetch origin pull/<PRNUMBER>/head`. Emit `AVIARY_CHECKOUT_DIRECTORY=<absolute clone path>` after cloning, as `gh-fix` does. Until the check passes, never build, test, install, run a script, open the pull request's code in a tool that executes project configuration, or check it out into a working tree with project hooks enabled.

Examine the description, the full diff, every commit, and anything the diff touches for:

- **Intent:** the code does what the pull request says and nothing beyond it. No unrelated behavior rides along.
- **Security:** no new injection, exfiltration, privilege, or unsafe-default surface.
- **Malicious behavior and backdoors:** no hidden functionality, covert network calls, credential or environment harvesting, obfuscated or encoded payloads, binary blobs without a stated purpose, or logic that behaves differently under review than at runtime.
- **Supply chain:** new or changed dependencies are legitimate, spelled correctly, and pinned as the repository requires; none is redirected to an untrusted source. Install scripts, build hooks, test fixtures that execute, and CI workflow changes get specific scrutiny, above all anything that reads secrets, widens a workflow's permissions or triggers (such as `pull_request_target`), or weakens a check.
- **Licensing:** no code or asset whose license is incompatible with the repository's, and no unattributed copied code presented as original.
- **General properness:** the contribution is genuinely what it purports to be, not spam, vandalism, or nonsense dressed as a change.

The checks overlap; any one failing fails the step. Classify the outcome as **passed**, **failed without malice** (unsafe by mistake, incorrect, or contradicting its description), or **malicious** (a backdoor, supply-chain compromise, covert behavior, or bad-faith content). Keep the reasoning brief and concrete; it goes into the comment when the check fails. Then follow "Handle failures" for either failure outcome.

Whenever the head SHA changes during the run because the author pushed, stop, run this check again on the new commits, and record the new reviewed SHA before continuing.

## Mark a draft ready

Only after the safety check passed, and never before, mark a draft pull request ready for review with `gh pr ready <PRNUMBER> --repo <OWNER>/<REPO>`, so that "Merge" never meets a draft. Do this before unblocking CI. Running the skill is the authorization; the author having opened it as a draft does not need a separate go-ahead. Skip this step when the pull request is already ready. Never mark a pull request ready when the safety check failed, malicious or not: a pull request handed back to its author or closed keeps its draft state. An explicit hold recorded in validation still applies after this step; it stops only the merge.

Marking a pull request ready does not change its head, so CI that already ran on the reviewed SHA stays valid. When a workflow does trigger on `ready_for_review`, its new runs are on the reviewed SHA and are approved and watched like any other.

## Unblock CI and run it to green

1. Only after the safety check passed, unblock CI if GitHub is holding it. Find runs waiting on approval with `gh api "repos/<OWNER>/<REPO>/actions/runs?head_sha=<REVIEWEDSHA>&status=action_required"` and approve each with `gh api -X POST repos/<OWNER>/<REPO>/actions/runs/<RUNID>/approve`. Approve only runs whose `head_sha` is the reviewed SHA.
2. Drive CI to green as "Drive CI to completion" in `gh-fix` describes: confirm checks registered, watch them in the foreground, and read failing logs before deciding anything. A failure in the pull request's own code is not repaired here; it goes back to the author under "Handle failures". A clearly flaky or transient failure is rerun once. A failure that is also red on the base branch is not the author's fault: report it as a blocker and do not merge through it. A merge conflict alone is not a failure; "Finish superficially" resolves it.

## Finish superficially

Only once the safety check has passed and CI is initially green, make whichever of these the pull request needs, and nothing else:

- resolving merge conflicts,
- file formatting,
- auto-fixable linter passes,
- a changelog entry, following step 4 of "Implement the fix" in `gh-fix`, including its unreleased-section, user-visible-only, and no-changelog rules; keep an entry the author already wrote,
- README or documentation updates the change plainly requires.

A conflict whose resolution needs a judgement about the pull request's behavior is not superficial; hand it back to the author. When nothing is needed, skip this section.

Where the changes go depends on the pull request's permissions:

- **Maintainers may edit it** (`maintainerCanModify`, or the head branch is in this repository): check out the pull request with `gh pr checkout <PRNUMBER>` in the clone, commit the finishing changes on top of the reviewed SHA, and push to the pull request's own head branch. Never force-push and never rewrite the author's commits.
- **They may not:** push a branch named `safe-merge/pr-<PRNUMBER>-<short-slug>` to this repository from the reviewed SHA plus the finishing commits, keeping the author's commits so authorship stays with them. Open a ready pull request from it with the original's title, a body that carries the original's description and closing references, links the original, says what the finishing changes were, and ends with the `**Agents:**` footer as "Maintaining the contributing-agents footer" in `gh-fix` describes. Then close the original with a comment mentioning its author that links the new pull request and explains why, and post a matching note on the new pull request. Continue the run on the new pull request.

Use the same commit message rules as "Commit and push" in `gh-fix`: no skip-CI marker in any commit, title, or body. A body this run writes uses real line breaks.

## Run CI to completion

Run CI on the final head, including any finishing commits, as "Drive CI to completion" in `gh-fix` describes, and require every required check to pass on that head. Approve held runs again only for the reviewed SHA or this run's own commits on top of it. A failure caused by a finishing change is this run's to repair; any other failure follows "Unblock CI and run it to green".

## Merge

When validation recorded a hold, do not merge, even though the pull request is no longer a draft: post one comment on the pull request mentioning its author, saying it cleared the safety check and CI and that the merge was withheld, naming the label or quoting the wording that withheld it, never the draft state. Then report the hold as the outcome.

Otherwise, follow "Merge and verify" in `gh-fix`, with these differences:

1. Confirm again that the pull request is open, unowned, mergeable, still targets the default branch, and that its head is the reviewed SHA or this run's commits on top of it.
2. Merge with the method order `gh-fix` uses, pinned to the head that passed CI, and with the explicit commit message its "Merge and verify" describes: `gh pr merge <PRNUMBER> --repo <OWNER>/<REPO> --squash --match-head-commit <HEADSHA> --subject "<PR TITLE> (#<PRNUMBER>)" --body-file <FILE>`, where the file is the pull request's own body with every skip-CI marker removed, its closing references kept, no commit list, and the `Co-authored-by:` trailers from its commits. Never let GitHub fill in the default message, which lists every commit, and rebase only when the head commit message has no skip-CI marker.
3. Delete the head branch only when it is in this repository and policy permits; never touch a contributor's fork.
4. Verify the merge and that the issues it closes closed, then run the post-merge workflow check from step 6 of "Merge and verify" in `gh-fix`, dispatching any push workflow the merge skipped. Post no comment on a successful merge; the merge is the acceptance.

## Handle failures

Every comment this skill posts mentions the pull request's author as `@<AUTHOR>`, so the author, and any job following the pull request, hears it. Post at most one comment per outcome, state the finding plainly, and never pad it with review-style commentary.

- **Malicious:** close the pull request immediately with `gh pr close <PRNUMBER> --repo <OWNER>/<REPO> --comment <TEXT>`, where the comment mentions the author and states the finding. Never approve its workflow runs, never run its code, and never merge it, at any point. When one of its runs already ran without approval, such as for a collaborator, say so in the run's outcome so the user can check for leaked secrets.
- **Failed without malice, or incorrect:** behavior that contradicts the description, contradictory implementation details, a substantive problem the safety check found, a conflict that needs a behavioral judgement, or checks failing on the pull request's own code. Comment on the pull request mentioning the author and describing what is wrong and, briefly, what would resolve it. Do not close it, do not merge it, do not mark a draft ready, and do not push changes to it. When CI has not yet been unblocked, leave it blocked.
- **External blocker:** missing access, a base-branch CI failure, or a GitHub problem. Report it in the run's outcome and in one comment on the pull request mentioning the author only when the author needs to know; otherwise post nothing.

Any failure before CI is unblocked resolves to a comment mentioning the author; closing is reserved for the malicious case. File no follow-up issues for the author's work.

## Report the result

Lead with the outcome: merged, closed as malicious, handed back to the author, held, or blocked. Include the pull request URL (and the replacement pull request's URL when one was opened), the reviewed and final head SHAs, the merge SHA when merged, the safety check's result in one or two sentences, whether the run marked a draft ready for review, which CI runs were approved, the finishing changes made, the CI checks that passed, the URL of every comment posted, and the hold's label or quote when held. When blocked, name the failed step, the relevant URL or log, and the exact capability the run lacked.

## Clean up the clone

Remove the clone and any temporary files when the run ends. When the run is blocked, leave the clone and report its location.
