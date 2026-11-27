import assert from "node:assert/strict";
import { execFile, execFileSync } from "node:child_process";
import { chmodSync, existsSync, mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { createServer } from "node:http";
import { tmpdir } from "node:os";
import { delimiter, join } from "node:path";
import { after, test } from "node:test";
import { fileURLToPath } from "node:url";
import { publishSignedRelease, RulesetRejectedError, releaseToken } from "./signed-release-commit.mjs";

const releaseScript = fileURLToPath(new URL("./release.mjs", import.meta.url));
const dirs = [];
after(() => {
	for (const dir of dirs) rmSync(dir, { recursive: true, force: true });
});

function tempDir() {
	const dir = mkdtempSync(join(tmpdir(), "aviary-release-test-"));
	dirs.push(dir);
	return dir;
}

function git(cwd, ...args) {
	return execFileSync("git", args, { cwd }).toString().trim();
}

// A repository whose HEAD is a release commit on top of a "remote" tip,
// shaped like the one release.yml has after `pnpm package` bumps the version.
function releaseRepo() {
	const cwd = tempDir();
	git(cwd, "init", "-q", "-b", "main");
	git(cwd, "config", "user.name", "test");
	git(cwd, "config", "user.email", "test@example.com");
	git(cwd, "config", "commit.gpgsign", "false");
	writeFileSync(join(cwd, "package.json"), `${JSON.stringify({ version: "1.0.1" })}\n`);
	writeFileSync(join(cwd, "old.txt"), "old\n");
	git(cwd, "add", ".");
	git(cwd, "commit", "-q", "-m", "base");
	writeFileSync(join(cwd, "package.json"), `${JSON.stringify({ version: "1.0.2" })}\n`);
	git(cwd, "rm", "-q", "old.txt");
	git(cwd, "add", ".");
	git(cwd, "commit", "-q", "-m", "v1.0.2");
	return {
		cwd,
		parent: git(cwd, "rev-parse", "HEAD^"),
		tree: git(cwd, "rev-parse", "HEAD^{tree}"),
	};
}

const ruleViolation = {
	status: 422,
	body: {
		message: "Repository rule violations found\n\n12 of 12 required status checks are expected.",
	},
};

// Answers the Git Data API calls publishSignedRelease makes. `patchRef`
// overrides the response to the branch update.
function fakeGitHub({ parent, tree }, { patchRef } = {}) {
	const calls = [];
	function respond(method, path, auth) {
		calls.push({ method, path, auth });
		if (method === "GET" && path === "git/ref/heads/main") {
			return { status: 200, body: { object: { sha: parent } } };
		}
		if (method === "POST" && path === "git/blobs") {
			return { status: 201, body: { sha: "b".repeat(40) } };
		}
		if (method === "POST" && path === "git/trees") {
			return { status: 201, body: { sha: tree } };
		}
		if (method === "POST" && path === "git/commits") {
			return { status: 201, body: { sha: "c".repeat(40), verification: { verified: true } } };
		}
		if (method === "PATCH" && path === "git/refs/heads/main") {
			return patchRef ?? { status: 200, body: {} };
		}
		if (method === "POST" && path === "git/refs") {
			return { status: 201, body: {} };
		}
		return { status: 404, body: { message: "Not Found" } };
	}
	return { calls, respond };
}

function fetchFor(github) {
	return async (url, init) => {
		const path = new URL(url).pathname.replace(/^\/repos\/owner\/repo\//, "");
		const { status, body } = github.respond(init.method, path, init.headers.Authorization);
		return new Response(JSON.stringify(body), { status });
	};
}

test("releaseToken prefers the ruleset-bypassing RELEASE_TOKEN over GH_TOKEN", () => {
	assert.deepEqual(releaseToken({ RELEASE_TOKEN: "app", GH_TOKEN: "actions" }), {
		token: "app",
		source: "RELEASE_TOKEN",
	});
	assert.deepEqual(releaseToken({ RELEASE_TOKEN: "", GH_TOKEN: "actions" }), {
		token: "actions",
		source: "GH_TOKEN",
	});
	assert.equal(releaseToken({}), null);
});

test("publishes the commit with the given token, moving the branch before tagging", async () => {
	const repo = releaseRepo();
	const github = fakeGitHub(repo);
	const sha = await publishSignedRelease({
		repo: "owner/repo",
		token: "app-token",
		branch: "main",
		tag: "v1.0.2",
		apiUrl: "https://api.test",
		cwd: repo.cwd,
		fetch: fetchFor(github),
	});
	assert.equal(sha, "c".repeat(40));
	assert.ok(github.calls.every((call) => call.auth === "Bearer app-token"));
	const writes = github.calls.filter((call) => call.path.startsWith("git/refs")).map((call) => call.method);
	assert.deepEqual(writes, ["PATCH", "POST"]);
});

test("a ruleset rejecting the branch update explains the bypass setup and creates no tag", async () => {
	const repo = releaseRepo();
	const github = fakeGitHub(repo, { patchRef: ruleViolation });
	await assert.rejects(
		publishSignedRelease({
			repo: "owner/repo",
			token: "actions-token",
			tokenSource: "GH_TOKEN",
			branch: "main",
			tag: "v1.0.2",
			apiUrl: "https://api.test",
			cwd: repo.cwd,
			fetch: fetchFor(github),
		}),
		(err) => {
			assert.ok(err instanceof RulesetRejectedError);
			assert.match(err.message, /branch ruleset blocks it/);
			assert.match(err.message, /12 of 12 required status checks are expected/);
			assert.match(err.message, /bypass actor on the main ruleset/);
			assert.match(err.message, /RELEASE_APP_CLIENT_ID/);
			assert.match(err.message, /RELEASE_APP_PRIVATE_KEY/);
			assert.match(err.message, /This run used GH_TOKEN/);
			assert.doesNotMatch(err.message, /"message"/);
			return true;
		},
	);
	assert.ok(!github.calls.some((call) => call.method === "POST" && call.path === "git/refs"));
});

test("other 422 errors from the branch update keep the API error", async () => {
	const repo = releaseRepo();
	const github = fakeGitHub(repo, {
		patchRef: { status: 422, body: { message: "Update is not a fast forward" } },
	});
	await assert.rejects(
		publishSignedRelease({
			repo: "owner/repo",
			token: "t",
			branch: "main",
			tag: "v1.0.2",
			apiUrl: "https://api.test",
			cwd: repo.cwd,
			fetch: fetchFor(github),
		}),
		(err) => !(err instanceof RulesetRejectedError) && /PATCH git\/refs\/heads\/main failed \(422\)/.test(err.message),
	);
});

test("release.mjs leaves no tag or GitHub release behind when the ruleset rejects the update", async () => {
	const repo = releaseRepo();
	mkdirSync(join(repo.cwd, "dist", "release"), { recursive: true });
	writeFileSync(join(repo.cwd, "dist", "release", "checksums.txt"), "");

	// A gh stand-in that records any invocation, such as `gh release create`.
	const bin = tempDir();
	const ghLog = join(bin, "gh.log");
	writeFileSync(
		join(bin, "gh"),
		`#!/usr/bin/env node\nrequire("node:fs").appendFileSync(${JSON.stringify(ghLog)}, process.argv.slice(2).join(" ") + "\\n");\n`,
	);
	chmodSync(join(bin, "gh"), 0o755);

	const github = fakeGitHub(repo, { patchRef: ruleViolation });
	const server = createServer((req, res) => {
		const path = req.url.replace(/^\/repos\/owner\/repo\//, "");
		req.resume();
		req.on("end", () => {
			const { status, body } = github.respond(req.method, path, req.headers.authorization);
			res.writeHead(status, { "Content-Type": "application/json" });
			res.end(JSON.stringify(body));
		});
	});
	await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
	try {
		const result = await new Promise((resolve) => {
			execFile(
				process.execPath,
				[releaseScript],
				{
					cwd: repo.cwd,
					env: {
						...process.env,
						PATH: `${bin}${delimiter}${process.env.PATH}`,
						GITHUB_REPOSITORY: "owner/repo",
						GITHUB_API_URL: `http://127.0.0.1:${server.address().port}`,
						GH_TOKEN: "actions-token",
						RELEASE_TOKEN: "app-token",
					},
				},
				(error, stdout, stderr) => resolve({ error, stderr }),
			);
		});
		assert.ok(result.error, "release.mjs should fail");
		assert.match(result.stderr, /This run used RELEASE_TOKEN/);
		assert.ok(github.calls.every((call) => call.auth === "Bearer app-token"));
		assert.ok(!github.calls.some((call) => call.method === "POST" && call.path === "git/refs"));
		assert.equal(existsSync(ghLog), false, "gh release create must not run");
	} finally {
		server.close();
	}
});
