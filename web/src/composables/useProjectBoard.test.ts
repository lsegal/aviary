// @vitest-environment happy-dom
import { enableAutoUnmount, flushPromises, mount } from "@vue/test-utils";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { defineComponent, h, ref } from "vue";

const callTool = vi.fn();
enableAutoUnmount(afterEach);
vi.mock("./useMCP", () => ({ useMCP: () => ({ callTool }) }));

const { CARD_OUTPUT_EVENTS, CARD_OUTPUT_LINES, useProjectBoard } = await import(
	"./useProjects"
);
const { applyProjectEvent, PROJECT_EVENT_BATCH_MS } = await import(
	"./projectListStore"
);
const { BOARD_LOAD_MEASURES } = await import("../lib/boardTiming");

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
// A delta the next jobs poll returns against revision r1, if set.
let delta: Record<string, unknown> | null = null;
function respond(tool: string, args: Record<string, unknown> = {}) {
	if (
		tool === "project_job_list" &&
		delta &&
		args.revision === "r1" &&
		args.delta === true &&
		!(args.kinds as string[] | undefined)?.includes("schedule")
	)
		return JSON.stringify(delta);
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
		delta = null;
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

	// #1661: a changed list is read as a delta and applied in place.
	it("applies a delta in place so unchanged cards keep their objects and don't re-render", async () => {
		const renders = new Map<string, number>();
		const Card = defineComponent({
			props: { job: { type: Object, required: true } },
			setup(props) {
				return () => {
					const id = (props.job as { id: string }).id;
					renders.set(id, (renders.get(id) ?? 0) + 1);
					return h("div", (props.job as { title: string }).title);
				};
			},
		});
		let board!: ReturnType<typeof useProjectBoard>;
		mount(
			defineComponent({
				setup() {
					board = useProjectBoard(
						ref(["o/r"]),
						ref([]),
						ref(""),
						ref(false),
						ref(true),
					);
					return () =>
						h(
							"div",
							board.jobs.value.map((job) => h(Card, { key: job.id, job })),
						);
				},
			}),
		);
		await flushPromises();
		const first = new Map(board.jobs.value.map((job) => [job.id, job]));
		expect(first.size).toBe(CARDS);
		expect([...renders.values()].every((n) => n === 1)).toBe(true);

		revision = "r2";
		delta = {
			delta: true,
			jobs: [{ ...job(5), title: "Job 5 changed" }],
			removed: ["o/r#12"],
			total: CARDS - 1,
			revision: "r2",
		};
		await board.refresh();
		await flushPromises();
		expect(calls("project_job_list").at(-2)?.[1]).toMatchObject({
			revision: "r1",
			delta: true,
		});
		const now = board.jobs.value;
		expect(now).toHaveLength(CARDS - 1);
		expect(board.total.value).toBe(CARDS - 1);
		expect(now.some((j) => j.id === "o/r#12")).toBe(false);
		for (const j of now) {
			if (j.id === "o/r#5") {
				expect(j.title).toBe("Job 5 changed");
				expect(j).not.toBe(first.get(j.id));
			} else expect(j).toBe(first.get(j.id));
		}
		expect(renders.get("o/r#5")).toBe(2);
		for (const [id, n] of renders) if (id !== "o/r#5") expect(n).toBe(1);
	});

	it("reads the board at once on a project-updated event for its project", async () => {
		const board = mountBoard();
		await flushPromises();
		const before = calls("project_job_list").length;
		applyProjectEvent({ type: "project-updated", target: "other/repo" });
		applyProjectEvent({ type: "project-updated", target: "o/r" });
		applyProjectEvent({ type: "project-updated", target: "o/r" });
		await new Promise((r) => setTimeout(r, PROJECT_EVENT_BATCH_MS + 50));
		await flushPromises();
		// One batched read of the board's two lists.
		expect(calls("project_job_list").length - before).toBe(2);
		expect(board().jobs.value).toHaveLength(CARDS);
	});

	it("records board-load timing for the request, parse and render", async () => {
		performance.clearMeasures();
		mountBoard();
		await flushPromises();
		for (const name of Object.values(BOARD_LOAD_MEASURES)) {
			const entries = performance.getEntriesByName(name, "measure");
			expect(entries, name).toHaveLength(1);
			expect(entries[0].duration).toBeGreaterThanOrEqual(0);
		}
	});
});
