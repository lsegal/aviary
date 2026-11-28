import { execFileSync } from "node:child_process";

// Publishes the local HEAD commit (the release commit staged on top of the
// remote branch tip) and its tag through the GitHub Git Data API instead of
// `git push`. Commits created through the API without an explicit author are
// signed by GitHub, so the release commit shows as Verified and PR branches
// that merge it still satisfy the signed-commits ruleset (#858).

// The release commit and the branch update use RELEASE_TOKEN when it is set.
// release.yml mints it from a GitHub App that is a bypass actor on the `main`
// ruleset, so moving main to the fresh commit is not blocked by required
// status checks that cannot have reported on it yet (#1562). Without it the
// workflow's GH_TOKEN is used, which only works while no ruleset blocks it.
export function releaseToken(env = process.env) {
	if (env.RELEASE_TOKEN) {
		return { token: env.RELEASE_TOKEN, source: "RELEASE_TOKEN" };
	}
	if (env.GH_TOKEN) {
		return { token: env.GH_TOKEN, source: "GH_TOKEN" };
	}
	return null;
}

export class RulesetRejectedError extends Error {
	constructor({ branch, tokenSource, detail }) {
		super(
			`GitHub rejected moving ${branch} to the release commit because a branch ruleset blocks it (${detail}). ` +
				`The release commit is created through the API right before this update, so required status checks can never have reported on it. ` +
				`Release with a token that can bypass the ruleset: set the RELEASE_APP_CLIENT_ID repository variable and the RELEASE_APP_PRIVATE_KEY secret ` +
				`for a GitHub App installed on this repository, and add that app as a bypass actor on the ${branch} ruleset ` +
				`(see "Publishing a Release" in CONTRIBUTING.md). This run used ${tokenSource}. No tag or GitHub release was created.`,
		);
		this.name = "RulesetRejectedError";
	}
}

function ruleViolationDetail(text) {
	try {
		const message = JSON.parse(text).message ?? "";
		if (/repository rule violations/i.test(message)) {
			return message.replace(/\s+/g, " ").trim();
		}
	} catch {}
	return null;
}

// Lists the paths HEAD changes relative to its parent, as tree entries for
// the Git Data API. Deleted paths get a null sha, which removes them.
function changedEntries(git) {
	const fields = git(
		"diff-tree",
		"-r",
		"-z",
		"--no-renames",
		"--no-commit-id",
		"HEAD^",
		"HEAD",
	)
		.toString()
		.split("\0");
	const entries = [];
	for (let i = 0; i + 1 < fields.length; i += 2) {
		const [oldMode, newMode, , newSha, status] = fields[i]
			.replace(/^:/, "")
			.split(" ");
		const path = fields[i + 1];
		if (status === "D") {
			entries.push({ path, mode: oldMode, type: "blob", sha: null });
		} else if (newMode === "160000") {
			entries.push({ path, mode: newMode, type: "commit", sha: newSha });
		} else {
			entries.push({ path, mode: newMode, type: "blob", localSha: newSha });
		}
	}
	return entries;
}

export async function publishSignedRelease({
	repo,
	token,
	tokenSource = "the provided token",
	branch,
	tag,
	apiUrl = "https://api.github.com",
	cwd,
	fetch = globalThis.fetch,
}) {
	const git = (...args) =>
		execFileSync("git", args, { cwd, maxBuffer: 256 * 1024 * 1024 });
	const gitText = (...args) => git(...args).toString().trim();

	async function api(method, path, body) {
		const res = await fetch(`${apiUrl}/repos/${repo}/${path}`, {
			method,
			headers: {
				Accept: "application/vnd.github+json",
				Authorization: `Bearer ${token}`,
				"X-GitHub-Api-Version": "2022-11-28",
			},
			body: body === undefined ? undefined : JSON.stringify(body),
		});
		const text = await res.text();
		if (!res.ok) {
			const detail = res.status === 422 && ruleViolationDetail(text);
			if (detail && method === "PATCH" && path === `git/refs/heads/${branch}`) {
				throw new RulesetRejectedError({ branch, tokenSource, detail });
			}
			throw new Error(`${method} ${path} failed (${res.status}): ${text}`);
		}
		return JSON.parse(text);
	}

	const parent = gitText("rev-parse", "HEAD^");
	const remote = await api("GET", `git/ref/heads/${branch}`);
	if (remote.object.sha !== parent) {
		throw new Error(
			`${branch} is at ${remote.object.sha}, but the release commit's parent is ${parent}; rerun the release from the current ${branch}`,
		);
	}

	const entries = [];
	for (const { localSha, ...entry } of changedEntries(git)) {
		if (localSha) {
			const blob = await api("POST", "git/blobs", {
				content: git("cat-file", "blob", localSha).toString("base64"),
				encoding: "base64",
			});
			entry.sha = blob.sha;
		}
		entries.push(entry);
	}

	const tree = await api("POST", "git/trees", {
		base_tree: gitText("rev-parse", "HEAD^^{tree}"),
		tree: entries,
	});
	const localTree = gitText("rev-parse", "HEAD^{tree}");
	if (tree.sha !== localTree) {
		throw new Error(
			`API tree ${tree.sha} does not match the release commit's tree ${localTree}`,
		);
	}

	// No author or committer: GitHub then signs the commit itself.
	const commit = await api("POST", "git/commits", {
		message: git("log", "-1", "--format=%B", "HEAD").toString(),
		tree: tree.sha,
		parents: [parent],
	});
	if (!commit.verification?.verified) {
		throw new Error(
			`release commit ${commit.sha} is not verified (${commit.verification?.reason})`,
		);
	}

	// Move the branch before creating the tag: if the update is rejected, no
	// tag exists yet, and release.mjs creates the GitHub release only after
	// this function returns, so a failed release leaves nothing behind.
	await api("PATCH", `git/refs/heads/${branch}`, {
		sha: commit.sha,
		force: false,
	});
	await api("POST", "git/refs", { ref: `refs/tags/${tag}`, sha: commit.sha });
	return commit.sha;
}
