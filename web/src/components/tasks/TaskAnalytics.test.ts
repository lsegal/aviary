// @vitest-environment happy-dom
import { flushPromises, mount, type VueWrapper } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ref } from "vue";
import type { LogEntry } from "../../composables/useLogs";

// Every store call goes through the MCP; route it by tool name.
const callTool = vi.fn();
vi.mock("../../composables/useMCP", () => ({ useMCP: () => ({ callTool }) }));

// The live log feed; tests push SSE entries into it.
const logEntries = ref<LogEntry[]>([]);
vi.mock("../../composables/useLogs", () => ({
	useLogs: () => ({ entries: logEntries }),
}));

import type { Job, TaskCompile } from "../../stores/jobs";
import { useJobsStore } from "../../stores/jobs";
import type { AppConfig } from "../../stores/settings";
import { useSettingsStore } from "../../stores/settings";
import TaskAnalytics from "./TaskAnalytics.vue";

type Handler = (args: Record<string, unknown>) => unknown;
let tools: Record<string, Handler>;

function job(over: Partial<Job>): Job {
	return {
		id: "job-000000000001",
		task_id: "Aviary/nightly",
		agent_id: "Aviary",
		prompt: "Summarize the day",
		status: "completed",
		attempts: 1,
		max_retries: 3,
		created_at: "2026-11-20T10:00:00Z",
		updated_at: "2026-11-20T10:00:04.200Z",
		...over,
	};
}

function compile(over: Partial<TaskCompile>): TaskCompile {
	return {
		id: "compile-00000000001",
		agent_id: "Aviary",
		status: "succeeded",
		created_at: "2026-11-20T09:00:00Z",
		updated_at: "2026-11-20T09:00:01Z",
		...over,
	};
}

let wrapper: VueWrapper | null = null;

function setup(
	opts: {
		jobs?: Job[];
		compiles?: TaskCompile[];
		task?: string;
		config?: AppConfig | null;
	} = {},
) {
	const store = useJobsStore();
	store.agent = "Aviary";
	store.startDate = "2026-11-18";
	store.endDate = "2026-11-21";
	store.jobs = opts.jobs ?? [];
	store.taskCompiles = opts.compiles ?? [];
	useSettingsStore().config = opts.config ?? null;
	wrapper = mount(TaskAnalytics, {
		props: { task: opts.task ?? "" },
		attachTo: document.body,
	});
	return { store, wrapper };
}

function buttonByText(w: Pick<VueWrapper, "findAll">, text: string) {
	const b = w.findAll("button").find((x) => x.text() === text);
	if (!b) throw new Error(`no button "${text}"`);
	return b;
}

function jobRows(w: VueWrapper) {
	return w.find('[data-testid="job-queue"]').findAll("tbody tr");
}

function panel(w: VueWrapper) {
	return w.find('[data-testid="details-panel"]');
}

beforeEach(() => {
	setActivePinia(createPinia());
	callTool.mockReset();
	logEntries.value = [];
	tools = {
		job_query: () => "[]",
		task_compile_query: () => "[]",
		job_logs: () => "",
	};
	callTool.mockImplementation(
		async (name: string, args: Record<string, unknown>) => {
			const h = tools[name];
			if (!h) throw new Error(`unexpected tool ${name}`);
			return h(args);
		},
	);
	Element.prototype.scrollIntoView = vi.fn();
});

afterEach(() => {
	wrapper?.unmount();
	wrapper = null;
});

describe("TaskAnalytics jobs by day", () => {
	it("says so when the range has no jobs", () => {
		const { wrapper } = setup();
		expect(wrapper.find('[data-testid="jobs-by-day"]').text()).toContain(
			"No jobs in selected range",
		);
	});

	it("stacks each day's bars in proportion and shows a hover tooltip", async () => {
		const { wrapper } = setup({
			jobs: [
				job({ id: "a", created_at: "2026-11-19T01:00:00Z" }),
				job({ id: "b", status: "failed", created_at: "2026-11-19T02:00:00Z" }),
				job({ id: "c", status: "pending", created_at: "2026-11-19T03:00:00Z" }),
				job({ id: "d", created_at: "2026-11-20T03:00:00Z" }),
			],
		});
		const chart = wrapper.find('[data-testid="jobs-by-day"]');
		const days = chart.findAll(".group");
		expect(days).toHaveLength(4);
		// 2026-11-19 is the busiest day: 60px split three ways.
		const heights = days[1]
			.findAll(".w-full")
			.map((d) => (d.element as HTMLElement).style.height);
		expect(heights).toEqual(["20px", "20px", "20px"]);
		// 2026-11-20 has a third of the peak, all completed.
		expect(
			days[2]
				.findAll(".w-full")
				.map((d) => (d.element as HTMLElement).style.height),
		).toEqual(["20px"]);
		expect(days[0].findAll(".w-full")).toHaveLength(0);
		// Few days, so every date is labelled.
		expect(chart.text()).toContain("11-18");
		expect(chart.text()).toContain("11-21");

		await days[1].trigger("mouseenter");
		const tip = days[1].find(".pointer-events-none");
		expect(tip.text()).toContain("2026-11-19");
		expect(tip.text()).toContain("✓ 1");
		expect(tip.text()).toContain("✗ 1");
		expect(tip.text()).toContain("● 1");
		await days[1].trigger("mouseleave");
		expect(days[1].find(".pointer-events-none").exists()).toBe(false);
	});

	it("labels only some dates in a long range", async () => {
		const { store, wrapper } = setup({
			jobs: [job({ created_at: "2026-12-05T00:00:00Z" })],
		});
		store.startDate = "2026-12-01";
		store.endDate = "2026-12-20";
		await flushPromises();
		const labels = wrapper
			.find('[data-testid="jobs-by-day"]')
			.findAll("span.flex-1")
			.map((s) => s.text());
		expect(labels).toHaveLength(20);
		// Step of 3: first, last and every third day (December avoids DST).
		expect(labels[0]).toBe("12-01");
		expect(labels[1]).toBe("");
		expect(labels[3]).toBe("12-04");
		expect(labels[19]).toBe("12-20");
	});
});

describe("TaskAnalytics job queue", () => {
	it("shows the empty state with a zero range", () => {
		const { wrapper } = setup();
		const queue = wrapper.find('[data-testid="job-queue"]');
		expect(queue.text()).toContain("No jobs in this range.");
		expect(queue.text()).toContain("Showing 0-0 of 0");
		expect(wrapper.find('[data-testid="job-count"]').text()).toBe("0 jobs");
	});

	it("renders status labels, classes, durations and schedule times", () => {
		const { wrapper } = setup({
			jobs: [
				job({
					id: "p",
					status: "pending",
					scheduled_for: "2026-11-21T08:00:00Z",
				}),
				job({ id: "r", status: "in_progress" }),
				job({ id: "c", status: "completed" }),
				job({ id: "f", status: "failed", updated_at: "2026-11-20T10:03:12Z" }),
				job({
					id: "x",
					status: "canceled",
					updated_at: "2026-11-20T10:00:00.850Z",
				}),
			],
		});
		const rows = jobRows(wrapper);
		const badge = (i: number) => rows[i].find("td span");
		expect(rows.map((r) => badge(rows.indexOf(r)).text())).toEqual([
			"Pending",
			"Running",
			"Done",
			"Failed",
			"canceled",
		]);
		expect(badge(0).classes()).toContain("bg-yellow-100");
		expect(badge(1).classes()).toContain("bg-blue-100");
		expect(badge(1).find(".animate-pulse").exists()).toBe(true);
		expect(badge(2).classes()).toContain("bg-green-100");
		expect(badge(3).classes()).toContain("bg-red-100");
		expect(badge(4).classes()).toContain("bg-gray-100");
		const cells = (i: number) => rows[i].findAll("td").map((c) => c.text());
		expect(cells(2)[3]).toBe("4.2s");
		expect(cells(3)[3]).toBe("3m 12s");
		expect(cells(4)[3]).toBe("850ms");
		expect(cells(2)[4]).toBe("1/3");
		expect(cells(2)[5]).toBe("—");
		expect(cells(0)[5]).not.toBe("—");
		expect(cells(0)[6]).toBe("Run Now");
		expect(cells(1)[6]).toBe("Stop");
		expect(cells(2)[6]).toBe("");
	});

	it("filters by status and resets to the first page", async () => {
		const jobs = Array.from({ length: 30 }, (_, i) =>
			job({
				id: `j${i}`,
				status: i % 3 === 0 ? "failed" : "completed",
			}),
		);
		const { wrapper } = setup({ jobs });
		expect(wrapper.find('[data-testid="job-count"]').text()).toBe("30 jobs");
		await buttonByText(wrapper, "Next").trigger("click");
		expect(wrapper.text()).toContain("Page 2 of 2");

		const failed = buttonByText(wrapper, "Failed");
		await failed.trigger("click");
		expect(failed.attributes("aria-pressed")).toBe("true");
		expect(failed.classes()).toContain("bg-blue-600");
		expect(buttonByText(wrapper, "All").attributes("aria-pressed")).toBe(
			"false",
		);
		expect(wrapper.find('[data-testid="job-count"]').text()).toBe("10 jobs");
		expect(wrapper.text()).toContain("Page 1 of 1");
		expect(wrapper.text()).toContain("Showing 1-10 of 10");

		await buttonByText(wrapper, "Pending").trigger("click");
		expect(wrapper.text()).toContain("No jobs in this range.");
	});

	it("pages through jobs with First, Prev, Next and Last", async () => {
		const jobs = Array.from({ length: 60 }, (_, i) => job({ id: `j${i}` }));
		const { wrapper } = setup({ jobs });
		const first = buttonByText(wrapper, "First");
		const prev = buttonByText(wrapper, "Prev");
		const next = buttonByText(wrapper, "Next");
		const last = buttonByText(wrapper, "Last");
		expect(jobRows(wrapper)).toHaveLength(25);
		expect(wrapper.text()).toContain("Showing 1-25 of 60");
		expect(first.attributes("disabled")).toBeDefined();
		expect(prev.attributes("disabled")).toBeDefined();

		await next.trigger("click");
		expect(wrapper.text()).toContain("Showing 26-50 of 60");
		await last.trigger("click");
		expect(wrapper.text()).toContain("Page 3 of 3");
		expect(wrapper.text()).toContain("Showing 51-60 of 60");
		expect(jobRows(wrapper)).toHaveLength(10);
		expect(next.attributes("disabled")).toBeDefined();
		expect(last.attributes("disabled")).toBeDefined();
		await prev.trigger("click");
		expect(wrapper.text()).toContain("Page 2 of 3");
		await first.trigger("click");
		expect(wrapper.text()).toContain("Page 1 of 3");
	});

	it("clamps the page when the job list shrinks", async () => {
		const jobs = Array.from({ length: 60 }, (_, i) => job({ id: `j${i}` }));
		const { store, wrapper } = setup({ jobs });
		await buttonByText(wrapper, "Last").trigger("click");
		store.jobs = jobs.slice(0, 30);
		await flushPromises();
		expect(wrapper.text()).toContain("Page 2 of 2");
		expect(wrapper.text()).toContain("Showing 26-30 of 30");
	});

	it("shows only the chosen task's runs, and clears the filter", async () => {
		const { wrapper } = setup({
			task: "Aviary/nightly",
			jobs: [
				job({ id: "a", task_id: "Aviary/nightly" }),
				job({ id: "b", task_id: "Aviary/hourly" }),
			],
		});
		await flushPromises();
		expect(Element.prototype.scrollIntoView).toHaveBeenCalledWith({
			block: "start",
			behavior: "smooth",
		});
		const chip = wrapper.find('[data-testid="job-task-filter"]');
		expect(chip.text()).toContain("Aviary/nightly");
		expect(wrapper.find('[data-testid="job-count"]').text()).toBe("1 jobs");
		await chip.find("button").trigger("click");
		expect(wrapper.emitted("update:task")).toEqual([[""]]);

		await wrapper.setProps({ task: "" });
		expect(wrapper.find('[data-testid="job-task-filter"]').exists()).toBe(
			false,
		);
		expect(wrapper.find('[data-testid="job-count"]').text()).toBe("2 jobs");
	});

	it("runs a queued job now and refreshes the queue", async () => {
		let release!: () => void;
		tools.job_run_now = () =>
			new Promise((r) => {
				release = () =>
					r(JSON.stringify(job({ id: "p", status: "in_progress" })));
			});
		tools.job_query = () =>
			JSON.stringify([job({ id: "p", status: "in_progress" })]);
		const { wrapper } = setup({ jobs: [job({ id: "p", status: "pending" })] });
		const run = buttonByText(wrapper, "Run Now");
		await run.trigger("click");
		expect(callTool).toHaveBeenCalledWith("job_run_now", { id: "p" });
		expect(run.text()).toBe("Running…");
		expect(run.attributes("disabled")).toBeDefined();
		// The click does not open the run's details.
		expect(panel(wrapper).exists()).toBe(false);
		release();
		await flushPromises();
		expect(callTool).toHaveBeenCalledWith("job_query", expect.anything());
		expect(jobRows(wrapper)[0].find("td span").text()).toBe("Running");
		expect(buttonByText(wrapper, "Stop").exists()).toBe(true);
	});

	it("clears the running state when running a job fails", async () => {
		tools.job_run_now = () => {
			throw new Error("boom");
		};
		const errors: unknown[] = [];
		const { wrapper } = setup({ jobs: [job({ id: "p", status: "pending" })] });
		wrapper.vm.$.appContext.config.errorHandler = (e) => {
			errors.push(e);
		};
		await buttonByText(wrapper, "Run Now").trigger("click");
		await flushPromises();
		expect(errors).toHaveLength(1);
		const run = buttonByText(wrapper, "Run Now");
		expect(run.attributes("disabled")).toBeUndefined();
	});

	it("stops a running job and refreshes the queue", async () => {
		let release!: () => void;
		tools.job_stop = () =>
			new Promise((r) => {
				release = () => r("ok");
			});
		tools.job_query = () =>
			JSON.stringify([job({ id: "r", status: "failed", error: "stopped" })]);
		const { wrapper } = setup({
			jobs: [job({ id: "r", status: "in_progress" })],
		});
		const stop = buttonByText(wrapper, "Stop");
		await stop.trigger("click");
		expect(callTool).toHaveBeenCalledWith("job_stop", { id: "r" });
		expect(stop.text()).toBe("Stopping…");
		expect(stop.attributes("disabled")).toBeDefined();
		release();
		await flushPromises();
		expect(jobRows(wrapper)[0].find("td span").text()).toBe("Failed");
	});

	it("clears the stopping state when stopping fails", async () => {
		tools.job_stop = () => {
			throw new Error("nope");
		};
		const errors: unknown[] = [];
		const { wrapper } = setup({
			jobs: [job({ id: "r", status: "in_progress" })],
		});
		wrapper.vm.$.appContext.config.errorHandler = (e) => {
			errors.push(e);
		};
		await buttonByText(wrapper, "Stop").trigger("click");
		await flushPromises();
		expect(errors).toHaveLength(1);
		const stop = buttonByText(wrapper, "Stop");
		expect(stop.text()).toBe("Stop");
		expect(stop.attributes("disabled")).toBeUndefined();
	});
});

describe("TaskAnalytics job details", () => {
	it("loads a finished job's output and reloads it", async () => {
		let output = "line one\r\nline two";
		tools.job_logs = ({ id }) => {
			expect(id).toBe("job-abcdef123456789");
			return output;
		};
		const { wrapper } = setup({
			jobs: [
				job({
					id: "job-abcdef123456789",
					scheduled_for: "2026-11-20T09:59:00Z",
					next_retry_at: "2026-11-20T11:00:00Z",
				}),
			],
		});
		await jobRows(wrapper)[0].trigger("click");
		expect(jobRows(wrapper)[0].classes()).toContain("bg-blue-50");
		expect(panel(wrapper).text()).toContain("Loading…");
		await flushPromises();
		const p = panel(wrapper);
		expect(p.text()).toContain("Done");
		expect(p.text()).toContain("…def123456789"); // last 12 chars
		expect(p.text()).toContain("Scheduled for");
		expect(p.text()).toContain("Next retry");
		expect(p.text()).toContain("1 / 3");
		expect(p.text()).toContain("Prompt");
		expect(p.text()).toContain("Summarize the day");
		expect(p.text()).toContain("Output");
		expect(p.text()).toContain("Line 1");
		expect(p.text()).toContain("line two");

		output = "fresh";
		await buttonByText(wrapper, "↺ Reload").trigger("click");
		await flushPromises();
		expect(panel(wrapper).text()).toContain("fresh");
		expect(panel(wrapper).text()).not.toContain("line two");
	});

	it("truncates long output lines until expanded", async () => {
		const long = "x".repeat(250);
		tools.job_logs = () => long;
		const { wrapper } = setup({ jobs: [job({})] });
		await jobRows(wrapper)[0].trigger("click");
		await flushPromises();
		expect(panel(wrapper).text()).toContain(`${"x".repeat(200)}…`);
		await buttonByText(wrapper, "Show").trigger("click");
		expect(panel(wrapper).find("pre").text()).toBe(long);
		await buttonByText(wrapper, "Hide").trigger("click");
		expect(panel(wrapper).find("pre").exists()).toBe(false);
	});

	it("says no output was captured for an empty log line list", async () => {
		tools.job_logs = () => "";
		const { wrapper } = setup({ jobs: [job({})] });
		await jobRows(wrapper)[0].trigger("click");
		await flushPromises();
		expect(panel(wrapper).text()).toContain("(no output captured)");
	});

	it("falls back to the session's messages when the job has no output", async () => {
		let release!: () => void;
		tools.session_messages = (args) => {
			expect(args).toEqual({ agent: "Aviary", session_id: "s1", order: "asc" });
			return new Promise((r) => {
				release = () =>
					r(
						JSON.stringify([
							{
								id: "m1",
								role: "user",
								content: "hi",
								timestamp: "2026-11-20T10:00:00Z",
							},
							{ id: "m2", role: "assistant", content: "y".repeat(210) },
							{ role: "tool", content: '{"ok":true}' },
							{
								id: "m4",
								role: "system",
								content: "",
								media_urls: ["/media/a.png"],
							},
							{
								id: "m5",
								role: "assistant",
								content: "pic",
								media_urls: ["/media/b.png"],
							},
							{ id: "m6", role: "tool", content: "not json" },
							{ id: "m7", role: "tool" },
						]),
					);
			});
		};
		const { wrapper } = setup({ jobs: [job({ session_id: "s1" })] });
		await jobRows(wrapper)[0].trigger("click");
		await flushPromises();
		expect(panel(wrapper).text()).toContain("Loading session messages…");
		release();
		await flushPromises();
		const cards = panel(wrapper).findAll(".rounded-lg.border");
		expect(cards).toHaveLength(7);
		const role = (i: number) => cards[i].find("span.inline-block");
		expect(role(0).classes()).toContain("bg-gray-800");
		expect(role(1).classes()).toContain("bg-green-900");
		expect(role(2).classes()).toContain("bg-purple-900");
		expect(role(3).classes()).toContain("bg-gray-700");
		expect(cards[1].text()).toContain(`${"y".repeat(200)}…`);
		expect(cards[3].find("a").attributes("href")).toBe("/media/a.png");
		expect(cards[4].text()).toContain("pic");

		// Expanded tool messages are pretty-printed JSON when they parse.
		await cards[2].find("button").trigger("click");
		expect(cards[2].find("pre").text()).toBe('{\n  "ok": true\n}');
		await cards[5].find("button").trigger("click");
		expect(cards[5].find("pre").text()).toBe("not json");
		await cards[6].find("button").trigger("click");
		expect(cards[6].find("pre").text()).toBe("");
		await cards[1].find("button").trigger("click");
		expect(cards[1].find("pre").text()).toBe("y".repeat(210));
		expect(cards[1].find("button").text()).toBe("Hide");
	});

	it("shows a failed job's error and a retried one's last failure", async () => {
		const { store, wrapper } = setup({
			jobs: [job({ id: "f", status: "failed", error: "exit status 1" })],
		});
		await jobRows(wrapper)[0].trigger("click");
		await flushPromises();
		const err = wrapper.find('[data-testid="job-error"]');
		expect(err.text()).toContain("Error");
		expect(err.text()).toContain("exit status 1");

		store.jobs = [job({ id: "f", status: "pending", error: "timeout" })];
		await flushPromises();
		expect(wrapper.find('[data-testid="job-error"]').text()).toContain(
			"Last attempt failed",
		);
	});

	it("shows a script task's run as a script", async () => {
		const { wrapper } = setup({
			jobs: [job({ prompt: "echo hi" })],
			config: {
				agents: [
					{ name: "Aviary", tasks: [{ name: "nightly", type: "script" }] },
				],
			} as unknown as AppConfig,
		});
		await jobRows(wrapper)[0].trigger("click");
		await flushPromises();
		expect(panel(wrapper).text()).toContain("Script");
		expect(panel(wrapper).find("pre").text()).toBe("echo hi");
	});

	it("streams a running job's live output and the idle timeout", async () => {
		const { wrapper } = setup({
			jobs: [job({ id: "r", status: "in_progress" })],
			config: {
				agents: [
					{ name: "Aviary", tasks: [{ name: "nightly", idle_timeout: "5m" }] },
				],
			} as unknown as AppConfig,
		});
		await jobRows(wrapper)[0].trigger("click");
		await flushPromises();
		expect(callTool).not.toHaveBeenCalledWith("job_logs", expect.anything());
		const p = panel(wrapper);
		expect(p.text()).toContain("Live Output");
		expect(p.text()).toContain("Waiting for output…");
		expect(p.text()).toContain("A run with no output for 5m is stopped");
		expect(p.findAll("button").some((b) => b.text() === "↺ Reload")).toBe(
			false,
		);

		const scroll = vi.mocked(Element.prototype.scrollIntoView);
		scroll.mockClear();
		logEntries.value = [
			{
				seq: 1,
				ts: "",
				level: "info",
				component: "job",
				msg: "m",
				attrs: { job_id: "r", chunk: "hello" },
			},
			{
				seq: 2,
				ts: "",
				level: "info",
				component: "job",
				msg: "plain",
				attrs: { job_id: "r" },
			},
			{
				seq: 3,
				ts: "",
				level: "info",
				component: "job",
				msg: "other",
				attrs: { job_id: "z" },
			},
		];
		await flushPromises();
		expect(panel(wrapper).text()).toContain("hello");
		expect(panel(wrapper).text()).toContain("plain");
		expect(panel(wrapper).text()).not.toContain("other");
		expect(scroll).toHaveBeenCalledWith({ behavior: "smooth" });
	});

	it("uses the default idle timeout and hides it when disabled", async () => {
		const { wrapper } = setup({
			jobs: [job({ id: "r", status: "in_progress" })],
		});
		await jobRows(wrapper)[0].trigger("click");
		expect(panel(wrapper).text()).toContain("no output for 15m");

		useSettingsStore().config = {
			agents: [
				{ name: "Aviary", tasks: [{ name: "nightly", idle_timeout: "0s" }] },
			],
		} as unknown as AppConfig;
		await flushPromises();
		expect(panel(wrapper).text()).not.toContain("is stopped and marked failed");
	});

	it("loads the output once the open run finishes", async () => {
		tools.job_logs = () => "all done";
		const { store, wrapper } = setup({
			jobs: [job({ id: "r", status: "in_progress" })],
		});
		await jobRows(wrapper)[0].trigger("click");
		// Unrelated refreshes leave the open run alone.
		store.jobs = [job({ id: "other" }), ...store.jobs];
		await flushPromises();
		expect(callTool).not.toHaveBeenCalledWith("job_logs", expect.anything());

		store.jobs = [job({ id: "r", status: "completed" })];
		await flushPromises();
		expect(callTool).toHaveBeenCalledWith("job_logs", { id: "r" });
		expect(panel(wrapper).text()).toContain("Output");
		expect(panel(wrapper).text()).toContain("all done");
	});

	it("closes the details panel", async () => {
		const { store, wrapper } = setup({ jobs: [job({})] });
		await jobRows(wrapper)[0].trigger("click");
		await flushPromises();
		await buttonByText(panel(wrapper), "✕").trigger("click");
		expect(panel(wrapper).exists()).toBe(false);
		// With nothing open, a refresh changes nothing.
		store.jobs = [job({ status: "failed" })];
		await flushPromises();
		expect(panel(wrapper).exists()).toBe(false);
	});
});

describe("TaskAnalytics compile attempts", () => {
	it("shows loading and empty states", async () => {
		const { store, wrapper } = setup();
		const section = () => wrapper.find('[data-testid="compile-attempts"]');
		expect(section().text()).toContain("No compile attempts in this range.");
		store.compilesLoading = true;
		await flushPromises();
		expect(section().text()).toContain("Loading compile attempts…");
	});

	it("lists the five newest attempts with status labels", () => {
		const { wrapper } = setup({
			compiles: [
				compile({
					id: "c1",
					task_name: "nightly",
					reason: "ok",
					requested_task_type: "prompt",
					result_task_type: "script",
				}),
				compile({ id: "c2", status: "skipped", prompt: "do it" }),
				compile({ id: "c3", status: "failed" }),
				compile({ id: "c4", status: "weird" as TaskCompile["status"] }),
				compile({ id: "c5" }),
				compile({ id: "c6" }),
			],
		});
		const section = wrapper.find('[data-testid="compile-attempts"]');
		expect(section.text()).toContain("5 shown of 6 recorded");
		const rows = section.findAll("tbody tr");
		expect(rows).toHaveLength(5);
		const badge = (i: number) => rows[i].find("td span");
		expect(badge(0).text()).toBe("Compiled");
		expect(badge(0).classes()).toContain("bg-emerald-100");
		expect(badge(1).text()).toBe("Skipped");
		expect(badge(1).classes()).toContain("bg-amber-100");
		expect(badge(2).text()).toBe("Failed");
		expect(badge(2).classes()).toContain("bg-red-100");
		expect(badge(3).text()).toBe("weird");
		expect(badge(3).classes()).toContain("bg-gray-100");
		expect(rows[0].text()).toContain("nightly");
		expect(rows[0].text()).toContain("prompt → script");
		expect(rows[0].text()).toContain("ok");
		expect(rows[1].text()).toContain("ad hoc task");
		expect(rows[1].text()).toContain("prompt → prompt");
		expect(rows[1].find("td[title]").attributes("title")).toBe("do it");
		expect(rows[2].text()).toContain("—");
	});

	it("opens an attempt's stages, reloads it and switches to a job", async () => {
		const full = compile({
			id: "compile-abcdef123456",
			task_name: "nightly",
			trigger: "save",
			target: "https://example.test",
			prompt: "Fetch the page",
			reason: "deterministic",
			validated: true,
			needs_discovery: true,
			deterministic_steps: 3,
			script: "print('hi')",
			stages: [
				{
					name: "plan",
					status: "succeeded",
					started_at: "2026-11-20T09:00:00Z",
					system_prompt: "SYS",
					user_prompt: "USR",
					response: "RESP",
				},
				{
					name: "verify",
					status: "failed",
					started_at: "2026-11-20T09:00:01Z",
					error: "ERR",
				},
				{
					name: "write",
					status: "started",
					started_at: "2026-11-20T09:00:02Z",
				},
				{ name: "other", status: "queued", started_at: "" },
			],
		} as Partial<TaskCompile>);
		let release!: () => void;
		tools.task_compile_get = ({ id }) => {
			expect(id).toBe("compile-abcdef123456");
			return new Promise((r) => {
				release = () => r(JSON.stringify(full));
			});
		};
		const { wrapper } = setup({
			jobs: [job({})],
			compiles: [compile({ id: "compile-abcdef123456" })],
		});
		const row = wrapper.find('[data-testid="compile-attempts"] tbody tr');
		await row.trigger("click");
		expect(row.classes()).toContain("bg-sky-50");
		expect(panel(wrapper).text()).toContain("Compiler Stages");
		expect(panel(wrapper).text()).toContain("Loading…");
		release();
		await flushPromises();
		const p = panel(wrapper);
		expect(p.text()).toContain("…abcdef123456");
		expect(p.text()).toContain("Compiled");
		expect(p.text()).toContain("save");
		expect(p.text()).toContain("https://example.test");
		expect(p.text()).toContain("Fetch the page");
		expect(p.text()).toContain("Reason");
		expect(p.text()).toContain("deterministic");
		const dl = p.find("dl").text();
		expect(dl).toContain("Validatedyes");
		expect(dl).toContain("Needs discoveryyes");
		expect(dl).toContain("Deterministic steps3");
		for (const s of [
			"SYS",
			"USR",
			"RESP",
			"ERR",
			"Generated Script",
			"print('hi')",
		]) {
			expect(p.text()).toContain(s);
		}
		const stageBadges = p.findAll("span.inline-flex.rounded-full.px-2");
		expect(
			stageBadges
				.slice(1)
				.map((b) => b.classes().find((c) => c.startsWith("bg-"))),
		).toEqual(["bg-emerald-100", "bg-red-100", "bg-blue-100", "bg-gray-100"]);

		tools.task_compile_get = () =>
			JSON.stringify({ ...full, stages: [], script: "" });
		await buttonByText(p, "↺ Reload").trigger("click");
		await flushPromises();
		expect(panel(wrapper).text()).toContain("No stage logs captured.");

		// Opening a job replaces the compile in the panel.
		await jobRows(wrapper)[0].trigger("click");
		await flushPromises();
		expect(panel(wrapper).text()).not.toContain("Compiler Stages");
		expect(panel(wrapper).text()).toContain("Summarize the day");
	});

	it("keeps the listed attempt when fetching its details fails", async () => {
		tools.task_compile_get = () => {
			throw new Error("gone");
		};
		const { store, wrapper } = setup({
			compiles: [compile({ id: "c1", task_name: "nightly" })],
		});
		await wrapper
			.find('[data-testid="compile-attempts"] tbody tr')
			.trigger("click");
		await flushPromises();
		expect(store.error).toBe("gone");
		const p = panel(wrapper);
		expect(p.text()).toContain("nightly");
		expect(p.find("dl").text()).toContain("Trigger—");
		expect(p.find("dl").text()).toContain("Validatedno");
		expect(p.find("dl").text()).toContain("Deterministic steps0");
		expect(p.text()).toContain("No stage logs captured.");
		expect(p.text()).not.toContain("Reason");
	});
});
