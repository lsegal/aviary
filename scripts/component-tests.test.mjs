import assert from "node:assert/strict";
import { test } from "node:test";
import {
	componentTestArgs,
	componentTestShards,
	FILES_PER_SHARD,
	MAX_SHARDS,
} from "./component-tests.mjs";

test("with no selection, the tests changed since the merge base run", () => {
	assert.deepEqual(componentTestArgs({ args: [] }), { needsBase: true });
	assert.deepEqual(componentTestArgs({ args: ["--shard", "1/2"], since: "abc" }), {
		args: ["--changed", "abc", "--passWithNoTests", "--shard", "1/2"],
	});
});

test("--all runs every test", () => {
	assert.deepEqual(componentTestArgs({ args: ["--all", "--shard=2/3"] }), {
		args: ["--shard=2/3"],
	});
});

test("an explicit selection runs as given", () => {
	for (const args of [
		["--changed", "abc"],
		["--changed=abc"],
		["--related", "web/src/App.vue"],
		["web/src/scenarios/nav"],
		["--shard=1/2", "nav"],
	]) {
		assert.deepEqual(componentTestArgs({ args }), { args });
	}
});

test("an option's value is not read as a filter", () => {
	assert.deepEqual(componentTestArgs({ args: ["--shard", "1/2"] }), {
		needsBase: true,
	});
});

test("shards hold about FILES_PER_SHARD files, capped at MAX_SHARDS", () => {
	assert.deepEqual(componentTestShards(0), [1]);
	assert.deepEqual(componentTestShards(1), [1]);
	assert.deepEqual(componentTestShards(FILES_PER_SHARD), [1]);
	assert.deepEqual(componentTestShards(FILES_PER_SHARD + 1), [1, 2]);
	assert.equal(componentTestShards(1000).length, MAX_SHARDS);
});
