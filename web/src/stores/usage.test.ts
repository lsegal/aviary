import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";

const callTool = vi.fn();
vi.mock("../composables/useMCP", () => ({ useMCP: () => ({ callTool }) }));

import {
	CODE_LOOP_BOOKKEEPING_LABEL,
	UNKNOWN_LABEL,
	type UsageRecord,
	usageModel,
	usageProvider,
	useUsageStore,
} from "./usage";

function rec(over: Partial<UsageRecord>): UsageRecord {
	return {
		timestamp: "2026-11-20T10:00:00Z",
		session_id: "s1",
		agent_id: "Aviary",
		model: "",
		provider: "",
		input_tokens: 0,
		output_tokens: 0,
		...over,
	};
}

async function load(records: UsageRecord[]) {
	callTool.mockResolvedValueOnce(JSON.stringify(records));
	const store = useUsageStore();
	await store.fetch();
	return store;
}

// #1470: the Usage page's Top Models and Top Providers cards showed nameless
// "0" rows and split one model into "opus" and "claude/opus".
describe("usage breakdowns", () => {
	beforeEach(() => {
		setActivePinia(createPinia());
		callTool.mockReset();
	});

	it("names a model provider/model and its provider from either form", () => {
		expect(usageModel({ model: "opus", provider: "claude" })).toBe(
			"claude/opus",
		);
		expect(usageModel({ model: "claude/opus", provider: "claude" })).toBe(
			"claude/opus",
		);
		expect(usageModel({ model: "claude", provider: "claude" })).toBe("claude");
		expect(usageProvider({ model: "codex/gpt-5", provider: "" })).toBe("codex");
	});

	it("merges bare and provider/model names into one row", async () => {
		const store = await load([
			rec({
				agent_id: "projects",
				model: "opus",
				provider: "claude",
				input_tokens: 600,
				output_tokens: 100,
				cost_usd: 1,
			}),
			rec({
				model: "claude/opus",
				provider: "claude",
				input_tokens: 200,
				output_tokens: 100,
				cost_usd: 0.5,
			}),
			rec({
				model: "codex/gpt-5",
				provider: "codex",
				input_tokens: 50,
				output_tokens: 50,
			}),
		]);
		expect(store.topModels).toEqual([
			{ name: "claude/opus", tokens: 1000, cost: 1.5 },
			{ name: "codex/gpt-5", tokens: 100, cost: null },
		]);
		// Each provider's model rows add up to its provider row.
		expect(store.topProviders).toEqual([
			{ name: "claude", tokens: 1000, cost: 1.5 },
			{ name: "codex", tokens: 100, cost: null },
		]);
	});

	it("gives no row to records that spent no tokens", async () => {
		const store = await load([
			rec({ model: "claude/opus", provider: "claude", input_tokens: 10 }),
			// A failed task editor analysis, Code Loop bookkeeping, and a
			// delivery log name no model and spend no tokens.
			rec({ has_error: true }),
			rec({ agent_id: "projects", project: { issue: "o/r#1" } }),
			rec({
				agent_id: "worker",
				delivery: { target: "slack", status: "ok", message: "" },
			}),
		]);
		expect(store.topModels.map((m) => m.name)).toEqual(["claude/opus"]);
		expect(store.topProviders.map((m) => m.name)).toEqual(["claude"]);
		expect(store.topAgents.map((m) => m.name)).toEqual(["Aviary"]);
		for (const list of [store.topModels, store.topProviders]) {
			expect(list.every((m) => m.name && m.tokens > 0)).toBe(true);
		}
	});

	it("labels token use with a blank model instead of a nameless row", async () => {
		const store = await load([
			rec({ agent_id: "projects", input_tokens: 5 }),
			rec({ session_id: "s2", input_tokens: 3 }),
		]);
		expect(store.topModels.map((m) => m.name)).toEqual([
			CODE_LOOP_BOOKKEEPING_LABEL,
			UNKNOWN_LABEL,
		]);
		expect(store.topProviders.map((m) => m.name)).toEqual([
			CODE_LOOP_BOOKKEEPING_LABEL,
			UNKNOWN_LABEL,
		]);
	});

	it("has no rows when no record in range spent tokens", async () => {
		const store = await load([rec({ has_error: true })]);
		expect(store.topModels).toEqual([]);
		expect(store.topProviders).toEqual([]);
		expect(store.topAgents).toEqual([]);
	});
});
