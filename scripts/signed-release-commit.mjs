import { execFileSync } from "node:child_process";

// Publishes the local HEAD commit (the release commit staged on top of the
// remote branch tip) and its tag through the GitHub Git Data API instead of
// `git push`. Commits created through the API without an explicit author are
// signed by GitHub, so the release commit shows as Verified and PR branches
// that merge it still satisfy the signed-commits ruleset (#858).

function git(...args) {
	return execFileSync("git", args, { maxBuffer: 256 * 1024 * 1024 });
}

function gitText(...args) {
	return git(...args).toString().trim();
}

// Lists the paths HEAD changes relative to its parent, as tree entries for
// the Git Data API. Deleted paths get a null sha, which removes them.
function changedEntries() {
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
	branch,
	tag,
	apiUrl = "https://api.github.com",
}) {
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
	for (const { localSha, ...entry } of changedEntries()) {
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

	await api("PATCH", `git/refs/heads/${branch}`, {
		sha: commit.sha,
		force: false,
	});
	await api("POST", "git/refs", { ref: `refs/tags/${tag}`, sha: commit.sha });
	return commit.sha;
}
