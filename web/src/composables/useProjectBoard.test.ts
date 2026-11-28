// @vitest-environment happy-dom
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { defineComponent, h, ref } from "vue";

const callTool = vi.fn();
vi.mock("./useMCP", () => ({ useMCP: () => ({ callTool }) }));

const { CARD_OUTPUT_EVENTS, CARD_OUTPUT_LINES, useProjectBoard } =
	await import("./useProjects");

// #1657: a board load costs a fixed number of requests however many cards it
// shows, carries card summaries and their output in the list itself, and a
// poll that finds nothing changed keeps what the board shows.
const CARDS = 12;

function job(n: number) {
	return {
		key: `o/r#${n}`,
		target: "o/r",
		number: n,
		title: `Job ${n}`,
		status: "active",
		state: "running",
		kind: "issue",
		scrollback: `output of ${n}`,
	};
}

let revision = "r1";
function respond(tool: string, args: Record<string, unknown> = {}) {
	if (tool === "project_status")
		return JSON.stringify({ targets: ["o/r"], states: {} });
	if (tool === "project_job_list") {
		if (args.revision === revision)
			return JSON.stringify({ unchanged: true, revision });
		const jobs = (args.kinds as string[] | undefined)?.includes("schedule")
			? []
			: Array.from({ length: CARDS }, (_, i) => job(i + 1));
		return JSON.stringify({ jobs, total: jobs.length, revision });
	}
	throw new Error(`unexpected ${tool}`);
}

function mountBoard(kanban = false) {
	let board!: ReturnType<typeof useProjectBoard>;
	mount(
		defineComponent({
			setup() {
				board = useProjectBoard(
					ref(["o/r"]),
					ref([]),
					ref(""),
					ref(kanban),
					ref(true),
				);
				return () => h("div");
			},
		}),
	);
	return () => board;
}

const calls = (tool: string) =>
	callTool.mock.calls.filter(([name]) => name === tool);

describe("useProjectBoard", () => {
	beforeEach(() => {
		revision = "r1";
		callTool.mockReset();
		callTool.mockImplementation(async (tool: string, args) =>
			respond(tool, args),
		);
	});

	it("loads the board in three requests with each card's output in the list", async () => {
		const board = mountBoard();
		await flushPromises();
		expect(board().jobs.value).toHaveLength(CARDS);
		expect(callTool).toHaveBeenCalledTimes(3);
		expect(calls("project_status")).toHaveLength(1);
		expect(calls("project_job_list")).toHaveLength(2);
		expect(calls("project_job_get")).toHaveLength(0);
		const [, jobsArgs] = calls("project_job_list")[0];
		expect(jobsArgs).toMatchObject({
			output_lines: CARD_OUTPUT_LINES,
			output_events: CARD_OUTPUT_EVENTS,
			revision: "new",
		});
		expect(board().jobs.value[0].output).toEqual(["output of 1"]);
	});

	it("keeps an unchanged list on a poll and replaces a changed one", async () => {
		const board = mountBoard();
		await flushPromises();
		const first = board().jobs.value;
		await board().refresh();
		await flushPromises();
		const lists = calls("project_job_list");
		expect(lists.at(-1)?.[1]).toMatchObject({ revision: "r1" });
		expect(board().jobs.value).toBe(first);

		revision = "r2";
		await board().refresh();
		await flushPromises();
		expect(board().jobs.value).not.toBe(first);
		expect(board().jobs.value).toHaveLength(CARDS);
	});

	it("reads no card output for the kanban view", async () => {
		mountBoard(true);
		await flushPromises();
		for (const [, args] of calls("project_job_list")) {
			expect(args.output_lines).toBeUndefined();
			expect(args.output_events).toBeUndefined();
		}
		expect(callTool).toHaveBeenCalledTimes(3);
	});
});
