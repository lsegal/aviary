// @vitest-environment happy-dom
import { flushPromises, mount, type VueWrapper } from "@vue/test-utils";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { defineComponent, h, type Ref, ref } from "vue";
import { createMemoryHistory, createRouter, type Router } from "vue-router";
import AddProjectDialog from "../components/projects/AddProjectDialog.vue";
import BoardMoreMenu from "../components/projects/BoardMoreMenu.vue";
import BoardStateFilter from "../components/projects/BoardStateFilter.vue";
import GitHubSignInBanner from "../components/projects/GitHubSignInBanner.vue";
import JobCard from "../components/projects/JobCard.vue";
import KanbanBoard from "../components/projects/KanbanBoard.vue";
import ProjectLogsPanel from "../components/projects/ProjectLogsPanel.vue";
import QueueDialog from "../components/projects/QueueDialog.vue";
import QueueMenu from "../components/projects/QueueMenu.vue";
import ScheduledCard from "../components/projects/ScheduledCard.vue";
import StatChipRow from "../components/projects/StatChipRow.vue";
import UpNextStrip from "../components/projects/UpNextStrip.vue";
import { useAgentItemDialogs } from "../composables/useAgentItemDialogs";
import type {
	BoardState,
	ProjectJob,
	ProjectSchedule,
	ProjectStatus,
	UpcomingRun,
} from "../lib/projects";
import ProjectsView from "./ProjectsView.vue";

// #1610: ProjectsView is the Code Loop board. Its data comes from the
// projects composables, stubbed here with fixed jobs and schedules, and its
// children are stubbed so the tests drive the view through their events.

const m = vi.hoisted(() => ({
	board: null as unknown as Record<string, unknown>,
	schedules: null as unknown as Record<string, unknown>,
	listRefresh: null as unknown as () => Promise<void>,
	setTaskEnabled: null as unknown as (
		agent: string,
		name: string,
		enabled: boolean,
	) => Promise<boolean>,
	config: null as unknown,
	fetchConfig: null as unknown as () => Promise<void>,
	boardArgs: [] as unknown[],
}));

vi.mock("../composables/useProjects", () => ({
	JOBS_PAGE_SIZE: 12,
	useProjectBoard: (...args: unknown[]) => {
		m.boardArgs = args;
		return m.board;
	},
	useProjectList: () => ({ refresh: m.listRefresh }),
	useProjectSchedules: () => m.schedules,
}));
vi.mock("../composables/useTaskPause", () => ({
	useTaskPause: () => ({ setTaskEnabled: m.setTaskEnabled }),
}));
vi.mock("../composables/useHeaderCollapse", () => ({
	useHeaderCollapse: () => ref(0),
}));
vi.mock("../composables/useFilterRowFit", () => ({
	useFilterRowFit: () => ({ stacked: ref(false), collapsed: ref(false) }),
}));
vi.mock("../stores/settings", async (orig) => ({
	...(await orig<typeof import("../stores/settings")>()),
	useSettingsStore: () => ({
		get config() {
			return m.config;
		},
		fetchConfig: m.fetchConfig,
	}),
}));

const STATES: Record<BoardState, number> = {
	starting: 1,
	paused: 0,
	running: 2,
	done: 3,
	failed: 1,
};

function makeStatus(over: Partial<ProjectStatus> = {}): ProjectStatus {
	return {
		instanceID: "",
		running: true,
		error: "",
		delivery: "gh",
		deliveryOnline: true,
		lastChecked: "",
		stale: false,
		cachedAt: "",
		interval: "1m",
		issueFilter: "",
		issueFilters: {},
		customFilters: [],
		allowedCommenters: [],
		noMerge: false,
		defaultMode: "watch",
		slotsUsed: 1,
		slotsTotal: 4,
		states: { ...STATES },
		merged24h: 5,
		targets: [{ name: "o/r", issues: 3 }],
		clis: [],
		log: ["line one"],
		github: null,
		...over,
	};
}

function makeJob(n: number, over: Partial<ProjectJob> = {}): ProjectJob {
	return {
		id: `o/r#${n}`,
		target: "o/r",
		repo: "o/r",
		number: n,
		title: `Job ${n}`,
		state: "running",
		...over,
	} as ProjectJob;
}

function makeSchedule(
	name: string,
	over: Partial<ProjectSchedule> = {},
): ProjectSchedule {
	return {
		id: `bot/${name}`,
		agent: "bot",
		name,
		project: "o/r",
		model: "",
		prompt: `Prompt for ${name}`,
		enabled: true,
		precompile: false,
		compiled: false,
		trigger: "interval",
		schedule: "",
		every: "1h",
		at: "",
		days: "",
		startAt: "",
		timezone: "",
		overlap: "skip",
		missed: "skip",
		description: "",
		nextRun: "",
		...over,
	} as ProjectSchedule;
}

interface Board {
	status: Ref<ProjectStatus | null>;
	jobs: Ref<ProjectJob[]>;
	total: Ref<number>;
	unfilteredTotal: Ref<number>;
	page: Ref<number>;
	error: Ref<string>;
	loading: Ref<boolean>;
	scheduleEntries: Ref<ProjectJob[]>;
	refresh: ReturnType<typeof vi.fn>;
	checkNow: ReturnType<typeof vi.fn>;
	signInToGitHub: ReturnType<typeof vi.fn>;
	switchToPolling: ReturnType<typeof vi.fn>;
	importProfile: ReturnType<typeof vi.fn>;
	retry: ReturnType<typeof vi.fn>;
	stop: ReturnType<typeof vi.fn>;
	dismiss: ReturnType<typeof vi.fn>;
	clear: ReturnType<typeof vi.fn>;
}
let board: Board;
let sched: {
	schedules: Ref<ProjectSchedule[]>;
	upcoming: Ref<UpcomingRun[]>;
	agents: Ref<string[]>;
	error: Ref<string>;
	runNow: ReturnType<typeof vi.fn>;
	refresh: ReturnType<typeof vi.fn>;
};

// matchMedia listeners, by query, so a test can cross a breakpoint.
let media: Record<string, { matches: boolean; listeners: Set<unknown> }>;

beforeEach(() => {
	localStorage.clear();
	board = {
		status: ref(makeStatus()),
		jobs: ref([makeJob(1), makeJob(2)]),
		total: ref(2),
		unfilteredTotal: ref(2),
		page: ref(0),
		error: ref(""),
		loading: ref(false),
		scheduleEntries: ref([]),
		refresh: vi.fn(async () => {}),
		checkNow: vi.fn(async () => {}),
		signInToGitHub: vi.fn(async () => {}),
		switchToPolling: vi.fn(async () => {}),
		importProfile: vi.fn(async () => {}),
		retry: vi.fn(async () => {}),
		stop: vi.fn(async () => {}),
		dismiss: vi.fn(async () => {}),
		clear: vi.fn(async () => {}),
	};
	m.board = board as unknown as Record<string, unknown>;
	sched = {
		schedules: ref([]),
		upcoming: ref([]),
		agents: ref(["bot"]),
		error: ref(""),
		runNow: vi.fn(async () => {}),
		refresh: vi.fn(async () => {}),
	};
	m.schedules = sched as unknown as Record<string, unknown>;
	m.listRefresh = vi.fn(async () => {});
	m.setTaskEnabled = vi.fn(async () => true);
	m.config = {
		agents: [{ name: "bot", projects: [{ target: "o/r" }, { target: "o/s" }] }],
	};
	m.fetchConfig = vi.fn(async () => {});
	useAgentItemDialogs().close();
	media = {};
	vi.stubGlobal("matchMedia", (query: string) => {
		media[query] ??= {
			matches: query.includes("640"),
			listeners: new Set(),
		};
		const entry = media[query];
		return {
			get matches() {
				return entry.matches;
			},
			addEventListener: (_: string, fn: unknown) => entry.listeners.add(fn),
			removeEventListener: (_: string, fn: unknown) =>
				entry.listeners.delete(fn),
		};
	});
});

let wrapper: VueWrapper | null = null;
afterEach(() => {
	wrapper?.unmount();
	wrapper = null;
	vi.unstubAllGlobals();
	vi.useRealTimers();
});

const Slot = defineComponent({
	setup: (_, { slots }) => () => h("div", slots.default?.()),
});
const AppLayoutStub = defineComponent({
	props: { compactHeader: Boolean, titleLink: Object },
	setup:
		(_, { slots }) =>
		() =>
			h("div", [
				h("header", { "data-testid": "status-slot" }, slots["header-status"]?.()),
				h("nav", { "data-testid": "actions-slot" }, slots["header-actions"]?.()),
				h("main", slots.default?.()),
			]),
});
const chatSend = vi.fn(async (_: string) => {});
const ProjectChatPanelStub = defineComponent({
	name: "ProjectChatPanel",
	props: { project: String },
	emits: ["close", "turn-end"],
	setup(_, { expose }) {
		expose({ send: chatSend });
		return () => h("aside", { "data-testid": "chat-panel" });
	},
});
const RouterViewStub = defineComponent({
	name: "RouterView",
	setup: () => () => h("div", { "data-testid": "job-detail" }),
});

function makeRouter(): Router {
	const Empty = { render: () => null };
	return createRouter({
		history: createMemoryHistory(),
		routes: [
			{ path: "/agents/:agent/projects", component: Empty },
			{
				path: "/agents/:agent/projects/:project",
				component: Empty,
				children: [{ path: "jobs/:number(\\d+)", component: Empty }],
			},
			{ path: "/:rest(.*)*", component: Empty },
		],
	});
}

async function render(path: string) {
	const router = makeRouter();
	await router.push(path);
	await router.isReady();
	wrapper = mount(ProjectsView, {
		global: {
			plugins: [router],
			stubs: {
				AppLayout: AppLayoutStub,
				HeaderOverflow: Slot,
				ProjectChatPanel: ProjectChatPanelStub,
				RouterView: RouterViewStub,
				AddProjectDialog: true,
				BoardHeaderInfo: true,
				BoardMoreMenu: true,
				BoardStateFilter: true,
				CLIProviderList: true,
				GitHubSignInBanner: true,
				JobCard: true,
				KanbanBoard: true,
				ProjectLogsPanel: true,
				ProjectModeMenu: true,
				ProjectNavRow: true,
				QueueDialog: true,
				QueueMenu: true,
				ScheduledCard: true,
				StatChipRow: true,
				UpNextStrip: true,
			},
		},
	});
	await flushPromises();
	return { w: wrapper, router };
}

const testid = (w: VueWrapper, id: string) => w.find(`[data-testid="${id}"]`);
const text = (w: VueWrapper) => w.text().replace(/\s+/g, " ");

describe("ProjectsView header", () => {
	it("shows a project's live status, issue count and stat cards", async () => {
		const { w } = await render("/agents/bot/projects/o%2Fr");
		expect(testid(w, "delivery-status").text()).toBe("live");
		expect(testid(w, "board-issues").text()).toBe("3 open issues");
		expect(testid(w, "project-manual-chip").exists()).toBe(false);
		expect(testid(w, "board-projects").exists()).toBe(false);
		// The useProjectBoard arguments are the project's own target.
		expect((m.boardArgs[0] as Ref<string[]>).value).toEqual(["o/r"]);
		// The wide viewport defaults to full cards.
		media["(min-width: 768px)"].matches = true;
		const labels = w
			.findAll('[data-testid="stat-label"]')
			.map((el) => el.text());
		expect(labels[0]).toBe("Slots");
		expect(labels.at(-1)).toBe("Merged (24h)");
		const values = w
			.findAll('[data-testid="stat-value"]')
			.map((el) => el.text());
		expect(values[0]).toBe("1of 4 busy");
		expect(values.at(-1)).toBe("5");
		expect(testid(w, "job-total").text()).toBe("2 jobs");
		expect(w.findAllComponents(JobCard)).toHaveLength(2);
	});

	it("marks a manual project and an offline delivery", async () => {
		board.status.value = makeStatus({
			deliveryOnline: false,
			targets: [{ name: "o/r", issues: 0, manual: true }],
		});
		const { w } = await render("/agents/bot/projects/o%2Fr");
		expect(testid(w, "delivery-status").text()).toBe("offline");
		expect(testid(w, "project-manual-chip").exists()).toBe(true);
		expect(testid(w, "board-issues").exists()).toBe(false);
	});

	it("counts a signed-out browser session as offline and acts on its banner", async () => {
		board.status.value = makeStatus({
			github: { signedOut: true } as ProjectStatus["github"],
		});
		const { w } = await render("/agents/bot/projects/o%2Fr");
		expect(testid(w, "delivery-status").text()).toBe("offline");
		const banner = w.findComponent(GitHubSignInBanner);
		banner.vm.$emit("signin");
		await flushPromises();
		expect(board.signInToGitHub).toHaveBeenCalled();
		banner.vm.$emit("use-polling");
		await flushPromises();
		expect(board.switchToPolling).toHaveBeenCalled();
		banner.vm.$emit("import", "Default");
		await flushPromises();
		expect(board.importProfile).toHaveBeenCalledWith("Default");
		expect(banner.props("busy")).toBe(false);

		board.signInToGitHub.mockRejectedValueOnce(new Error("no browser"));
		banner.vm.$emit("signin");
		await flushPromises();
		expect(text(w)).toContain("no browser");
		board.switchToPolling.mockRejectedValueOnce("plain failure");
		banner.vm.$emit("use-polling");
		await flushPromises();
		expect(text(w)).toContain("plain failure");
		// A later success clears the error.
		banner.vm.$emit("signin");
		await flushPromises();
		expect(text(w)).not.toContain("plain failure");
	});

	it("reports a stopped watch and a watch error", async () => {
		board.status.value = makeStatus({ running: false, error: "bad token" });
		const { w } = await render("/agents/bot/projects/o%2Fr");
		expect(text(w)).toContain("The watch isn't running: bad token");
		board.status.value = makeStatus({ running: false });
		await flushPromises();
		expect(text(w)).toContain("The watch isn't running.");
		board.status.value = makeStatus({ error: "rate limited" });
		await flushPromises();
		expect(text(w)).not.toContain("isn't running");
		expect(text(w)).toContain("rate limited");
		sched.error.value = "schedules failed";
		await flushPromises();
		expect(text(w)).toContain("schedules failed");
	});

	it("loads the settings config when the store has none", async () => {
		m.config = null;
		await render("/agents/bot/projects/o%2Fr");
		expect(m.fetchConfig).toHaveBeenCalled();
	});

	it("switches the counts between cards and the compact row and remembers it", async () => {
		const { w } = await render("/agents/bot/projects/o%2Fr");
		// Below md the counts default to the compact row.
		expect(w.findComponent(StatChipRow).exists()).toBe(true);
		const chips = w.findComponent(StatChipRow).props("cards") as {
			chip: string;
		}[];
		expect(chips.map((c) => c.chip)).toEqual(["1/4", "1", "0", "2", "3", "1", "5"]);
		await testid(w, "stats-toggle").trigger("click");
		expect(w.findComponent(StatChipRow).exists()).toBe(false);
		expect(w.findAll('[data-testid="stat-card"]')).toHaveLength(7);
		expect(localStorage.getItem("aviary.projects.statsCompact")).toBe("0");
		await testid(w, "stats-toggle").trigger("click");
		expect(localStorage.getItem("aviary.projects.statsCompact")).toBe("1");
	});

	it("follows the viewport until a pick and restores a saved pick", async () => {
		const { w } = await render("/agents/bot/projects/o%2Fr");
		const wide = media["(min-width: 768px)"];
		for (const fn of wide.listeners)
			(fn as (e: { matches: boolean }) => void)({ matches: true });
		await flushPromises();
		expect(w.findComponent(StatChipRow).exists()).toBe(false);
		const sm = media["(min-width: 640px)"];
		for (const fn of sm.listeners)
			(fn as (e: { matches: boolean }) => void)({ matches: false });
		await flushPromises();
		wrapper?.unmount();
		wrapper = null;
		expect(wide.listeners.size).toBe(0);
		expect(sm.listeners.size).toBe(0);

		localStorage.setItem("aviary.projects.statsCompact", "1");
		media["(min-width: 768px)"].matches = true;
		const again = await render("/agents/bot/projects/o%2Fr");
		expect(again.w.findComponent(StatChipRow).exists()).toBe(true);
	});

	it("shows CLI providers inline from sm up", async () => {
		board.status.value = makeStatus({
			clis: [{ name: "codex" }] as ProjectStatus["clis"],
		});
		const { w } = await render("/agents/bot/projects/o%2Fr");
		expect(testid(w, "board-nav-row").exists()).toBe(true);
	});

	it("ticks the clock every second", async () => {
		vi.useFakeTimers({ toFake: ["setInterval", "clearInterval", "Date"] });
		const { w } = await render("/agents/bot/projects/o%2Fr");
		const before = w.findComponent(UpNextStrip).props("now") as number;
		vi.advanceTimersByTime(3000);
		await flushPromises();
		expect(w.findComponent(UpNextStrip).props("now")).toBe(before + 3000);
	});
});

describe("ProjectsView filters and search", () => {
	it("toggles state filters into the URL and localStorage", async () => {
		const { w, router } = await render("/agents/bot/projects/o%2Fr");
		const filter = w.findComponent(BoardStateFilter);
		expect(filter.props("counts")).toMatchObject({
			scheduled: 0,
			running: 2,
			done: 3,
		});
		filter.vm.$emit("toggle", "running");
		await flushPromises();
		expect(router.currentRoute.value.query.state).toBe("running");
		expect(localStorage.getItem("aviary.projects.states.o/r")).toBe("running");
		expect(filter.props("filters")).toEqual(["running"]);
		// The board reads only the selected job states.
		expect((m.boardArgs[1] as Ref<string[]>).value).toEqual(["running"]);
		expect(testid(w, "job-total").text()).toBe("2 of 7 jobs");

		filter.vm.$emit("toggle", "done");
		await flushPromises();
		expect(router.currentRoute.value.query.state).toBe("running,done");
		filter.vm.$emit("toggle", "running");
		await flushPromises();
		expect(router.currentRoute.value.query.state).toBe("done");

		filter.vm.$emit("clear");
		await flushPromises();
		expect(router.currentRoute.value.query.state).toBeUndefined();
		expect(localStorage.getItem("aviary.projects.states.o/r")).toBeNull();
	});

	it("restores a board's remembered filters and lets the URL win", async () => {
		localStorage.setItem("aviary.projects.states.o/r", "failed");
		const { router } = await render("/agents/bot/projects/o%2Fr");
		expect(router.currentRoute.value.query.state).toBe("failed");

		const linked = await render("/agents/bot/projects/o%2Fr?state=done");
		expect(linked.router.currentRoute.value.query.state).toBe("done");
		expect(localStorage.getItem("aviary.projects.states.o/r")).toBe("done");

		await linked.router.replace({ query: { state: "" } });
		await flushPromises();
		expect(localStorage.getItem("aviary.projects.states.o/r")).toBeNull();
	});

	it("sends the old Schedules view's links to the scheduled filter", async () => {
		const { router } = await render("/agents/bot/projects/o%2Fr?tab=schedules");
		expect(router.currentRoute.value.query).toEqual({ state: "scheduled" });
		const view = await render("/agents/bot/projects/o%2Fr?view=schedules");
		expect(view.router.currentRoute.value.query).toEqual({ state: "scheduled" });
	});

	it("hides jobs when only Scheduled is selected", async () => {
		sched.schedules.value = [makeSchedule("nightly")];
		const { w } = await render("/agents/bot/projects/o%2Fr?state=scheduled");
		expect(w.findAllComponents(JobCard)).toHaveLength(0);
		expect(w.findAllComponents(ScheduledCard)).toHaveLength(1);
		expect(testid(w, "job-total").text()).toBe("1 of 7 items");
	});

	it("keeps a running schedule under its job state's filter", async () => {
		sched.schedules.value = [makeSchedule("nightly"), makeSchedule("idle")];
		board.scheduleEntries.value = [
			makeJob(9, {
				scheduled: true,
				scheduleID: "bot/nightly",
				schedule: { state: "running", recentRuns: [] },
			} as Partial<ProjectJob>),
			makeJob(10, {
				scheduled: true,
				scheduleID: "bot/idle",
				schedule: { state: "scheduled", recentRuns: [] },
			} as Partial<ProjectJob>),
		];
		const { w } = await render("/agents/bot/projects/o%2Fr?state=running");
		const cards = w.findAllComponents(ScheduledCard);
		expect(cards.map((c) => (c.props("schedule") as ProjectSchedule).name)).toEqual([
			"nightly",
		]);
		expect(cards[0].props("job")).toMatchObject({ number: 9 });
		// The running schedule is one item, not a job and a schedule.
		expect(testid(w, "job-total").text()).toBe("3 of 8 items");
	});

	it("searches jobs and schedules and clears an empty search", async () => {
		sched.schedules.value = [makeSchedule("nightly"), makeSchedule("weekly")];
		board.unfilteredTotal.value = 5;
		const { w } = await render("/agents/bot/projects/o%2Fr");
		const input = w.find('input[type="search"]');
		await input.setValue("weekly");
		expect((m.boardArgs[2] as Ref<string>).value).toBe("weekly");
		board.total.value = 1;
		await flushPromises();
		expect(testid(w, "search-count").text()).toBe("2 of 7 items match");
		expect(w.findAllComponents(ScheduledCard)).toHaveLength(1);
		// Searching always shows the pager.
		expect(w.find('nav[aria-label="Job pages"]').exists()).toBe(true);

		board.jobs.value = [];
		board.total.value = 0;
		await input.setValue("zzz");
		expect(testid(w, "search-empty").text()).toContain("No jobs match “zzz”");
		await testid(w, "search-empty").find("button").trigger("click");
		expect((input.element as HTMLInputElement).value).toBe("");
		expect(testid(w, "search-empty").exists()).toBe(false);
	});

	it("says one job matches in the singular", async () => {
		board.unfilteredTotal.value = 1;
		board.total.value = 1;
		board.jobs.value = [makeJob(1)];
		const { w } = await render("/agents/bot/projects/o%2Fr");
		await w.find('input[type="search"]').setValue("1");
		expect(testid(w, "search-count").text()).toBe("1 of 1 job matches");
		await w.find('input[type="search"]').setValue("");
		expect(testid(w, "job-total").text()).toBe("1 job");
	});

	it("pages through the jobs", async () => {
		board.total.value = 30;
		const { w } = await render("/agents/bot/projects/o%2Fr");
		const pager = w.find('nav[aria-label="Job pages"]');
		expect(pager.text()).toContain("Page 1 of 3");
		const prev = pager.find('button[aria-label="Previous page"]');
		const next = pager.find('button[aria-label="Next page"]');
		expect(prev.attributes("disabled")).toBeDefined();
		await next.trigger("click");
		expect(board.page.value).toBe(1);
		await next.trigger("click");
		expect(next.attributes("disabled")).toBeDefined();
		await prev.trigger("click");
		expect(board.page.value).toBe(1);
	});
});

describe("ProjectsView layouts and states", () => {
	it("switches to the kanban board and remembers it", async () => {
		sched.schedules.value = [makeSchedule("nightly")];
		const { w } = await render("/agents/bot/projects/o%2Fr");
		const toggle = testid(w, "kanban-toggle");
		expect(toggle.attributes("aria-pressed")).toBe("false");
		await toggle.trigger("click");
		expect(localStorage.getItem("aviary.projects.kanban")).toBe("1");
		expect((m.boardArgs[3] as Ref<boolean>).value).toBe(true);
		const kanban = w.findComponent(KanbanBoard);
		expect(kanban.props("jobs")).toHaveLength(2);
		expect(kanban.props("schedules")).toHaveLength(1);
		kanban.vm.$emit("settings", sched.schedules.value[0]);
		expect(useAgentItemDialogs().dialog.value).toEqual({
			kind: "task",
			mode: "edit",
			agent: "bot",
			name: "nightly",
		});
		await toggle.trigger("click");
		expect(localStorage.getItem("aviary.projects.kanban")).toBe("0");
		expect(w.findComponent(KanbanBoard).exists()).toBe(false);
	});

	it("opens in kanban when it was left there", async () => {
		localStorage.setItem("aviary.projects.kanban", "1");
		board.total.value = 30;
		const { w } = await render("/agents/bot/projects/o%2Fr");
		expect(w.findComponent(KanbanBoard).exists()).toBe(true);
		expect(w.find('nav[aria-label="Job pages"]').exists()).toBe(false);
	});

	it("shows loading and empty boards", async () => {
		board.jobs.value = [];
		board.total.value = 0;
		board.loading.value = true;
		const { w } = await render("/agents/bot/projects/o%2Fr");
		expect(text(w)).toContain("Loading jobs…");
		board.loading.value = false;
		await flushPromises();
		expect(text(w)).toContain("No jobs here yet.");
		expect(testid(w, "job-total").text()).toBe("0 jobs");
	});

	it("shows a job's details in place of the board", async () => {
		const { w } = await render("/agents/bot/projects/o%2Fr/jobs/4");
		expect(testid(w, "job-detail").exists()).toBe(true);
		expect(testid(w, "job-toolbar").exists()).toBe(false);
		expect(testid(w, "delivery-status").exists()).toBe(false);
		expect(w.findComponent(AppLayoutStub).props("compactHeader")).toBe(false);
	});

	it("opens the logs and chat panels and remembers them", async () => {
		const { w } = await render("/agents/bot/projects/o%2Fr");
		await testid(w, "logs-toggle").trigger("click");
		expect(localStorage.getItem("aviary.projects.logsOpen")).toBe("1");
		const logs = w.findComponent(ProjectLogsPanel);
		expect(logs.props("lines")).toEqual(["line one"]);
		logs.vm.$emit("close");
		await flushPromises();
		expect(w.findComponent(ProjectLogsPanel).exists()).toBe(false);
		expect(localStorage.getItem("aviary.projects.logsOpen")).toBe("0");

		await testid(w, "prompt-chat-toggle").trigger("click");
		expect(localStorage.getItem("aviary.projects.chatOpen")).toBe("1");
		const chat = w.findComponent(ProjectChatPanelStub);
		chat.vm.$emit("turn-end");
		expect(sched.refresh).toHaveBeenCalled();
		chat.vm.$emit("close");
		await flushPromises();
		expect(testid(w, "chat-panel").exists()).toBe(false);
	});
});

describe("ProjectsView actions", () => {
	it("runs job actions and shows their failures", async () => {
		const { w } = await render("/agents/bot/projects/o%2Fr");
		const card = w.findAllComponents(JobCard)[0];
		const job = board.jobs.value[0];
		card.vm.$emit("retry", job);
		await flushPromises();
		expect(board.retry).toHaveBeenCalledWith(job);
		card.vm.$emit("stop", job);
		card.vm.$emit("clear", job);
		await flushPromises();
		expect(board.stop).toHaveBeenCalledWith(job);
		expect(board.clear).toHaveBeenCalledWith(job);

		let release = () => {};
		board.retry.mockImplementationOnce(
			() =>
				new Promise<void>((_, reject) => {
					release = () => reject(new Error("retry failed"));
				}),
		);
		card.vm.$emit("retry", job);
		await flushPromises();
		expect(card.props("busy")).toBe(true);
		release();
		await flushPromises();
		expect(card.props("busy")).toBe(false);
		expect(board.error.value).toBe("retry failed");
		expect(text(w)).toContain("retry failed");
		board.stop.mockRejectedValueOnce("stop failed");
		card.vm.$emit("stop", job);
		await flushPromises();
		expect(board.error.value).toBe("stop failed");
	});

	it("summarizes a job in the prompt chat", async () => {
		chatSend.mockClear();
		const { w } = await render("/agents/bot/projects/o%2Fr");
		w.findAllComponents(JobCard)[0].vm.$emit("summarize", board.jobs.value[0]);
		await flushPromises();
		expect(testid(w, "chat-panel").exists()).toBe(true);
		expect(chatSend).toHaveBeenCalledTimes(1);
		expect(chatSend.mock.calls[0][0]).toContain("#1");
	});

	it("runs, pauses, duplicates, dismisses and opens a scheduled task", async () => {
		const nightly = makeSchedule("nightly");
		sched.schedules.value = [nightly];
		sched.upcoming.value = [
			{ id: "bot/nightly", name: "nightly", project: "o/r", at: "" },
		];
		const { w, router } = await render("/agents/bot/projects/o%2Fr");
		const card = w.findComponent(ScheduledCard);

		card.vm.$emit("run", nightly);
		await flushPromises();
		expect(sched.runNow).toHaveBeenCalledWith(nightly);
		sched.runNow.mockRejectedValueOnce(new Error("busy"));
		card.vm.$emit("run", nightly);
		await flushPromises();
		expect(board.error.value).toBe("busy");
		sched.runNow.mockRejectedValueOnce("odd");
		card.vm.$emit("run", nightly);
		await flushPromises();
		expect(board.error.value).toBe("odd");
		expect(card.props("busy")).toBe(false);

		card.vm.$emit("toggle", nightly);
		await flushPromises();
		expect(m.setTaskEnabled).toHaveBeenCalledWith("bot", "nightly", false);
		expect(nightly.enabled).toBe(false);
		expect(sched.refresh).toHaveBeenCalledTimes(1);
		// A failed change flips the card back.
		vi.mocked(m.setTaskEnabled).mockResolvedValueOnce(false);
		card.vm.$emit("toggle", nightly);
		await flushPromises();
		expect(nightly.enabled).toBe(false);

		card.vm.$emit("duplicate", nightly);
		const dup = useAgentItemDialogs().dialog.value;
		expect(dup).toMatchObject({
			kind: "task",
			mode: "create",
			agent: "bot",
			duplicate: "nightly",
		});
		dup?.onCreated?.("bot", "nightly-2");
		expect(sched.refresh).toHaveBeenCalledTimes(3);

		card.vm.$emit("settings", nightly);
		expect(useAgentItemDialogs().dialog.value).toMatchObject({ mode: "edit" });

		card.vm.$emit("dismiss", board.jobs.value[0]);
		await flushPromises();
		expect(board.dismiss).toHaveBeenCalledWith(board.jobs.value[0]);

		const strip = w.findComponent(UpNextStrip);
		strip.vm.$emit("open", "missing");
		await flushPromises();
		expect(router.currentRoute.value.path).toBe("/agents/bot/projects/o%2Fr");
		strip.vm.$emit("open", "bot/nightly");
		await flushPromises();
		expect(router.currentRoute.value.path).toBe("/agents/bot/tasks/nightly");
	});

	it("creates a scheduled task for the project under its owner", async () => {
		const { w } = await render("/agents/bot/projects/o%2Fr");
		const button = testid(w, "schedule-new");
		expect(button.attributes("disabled")).toBeUndefined();
		await button.trigger("click");
		const dialog = useAgentItemDialogs().dialog.value;
		expect(dialog).toMatchObject({
			kind: "task",
			mode: "create",
			agent: "bot",
			project: "o/r",
		});
		dialog?.onCreated?.("bot", "new");
		expect(sched.refresh).toHaveBeenCalled();
	});

	it("falls back to the first agent and disables Schedule without one", async () => {
		m.config = { agents: [] };
		sched.agents.value = ["helper"];
		const { w } = await render("/agents/bot/projects/o%2Fr");
		await testid(w, "schedule-new").trigger("click");
		expect(useAgentItemDialogs().dialog.value).toMatchObject({
			agent: "helper",
		});
		useAgentItemDialogs().close();
		sched.agents.value = [];
		await flushPromises();
		expect(testid(w, "schedule-new").attributes("disabled")).toBeDefined();
		w.findComponent(BoardMoreMenu).vm.$emit("schedule");
		expect(useAgentItemDialogs().dialog.value).toBeNull();
	});

	it("checks now, queues work and refreshes from the menus", async () => {
		const { w } = await render("/agents/bot/projects/o%2Fr");
		const menu = w.findComponent(BoardMoreMenu);
		expect(menu.props("settingsPath")).toBe(
			"/projects/settings?agent=bot&project=o%2Fr",
		);
		let finish = () => {};
		board.checkNow.mockImplementationOnce(
			() =>
				new Promise<void>((resolve) => {
					finish = resolve;
				}),
		);
		menu.vm.$emit("refresh");
		await flushPromises();
		expect(menu.props("checking")).toBe(true);
		finish();
		await flushPromises();
		expect(menu.props("checking")).toBe(false);
		board.checkNow.mockRejectedValueOnce(new Error("offline"));
		menu.vm.$emit("refresh");
		await flushPromises();
		expect(board.error.value).toBe("offline");
		board.checkNow.mockRejectedValueOnce("down");
		menu.vm.$emit("refresh");
		await flushPromises();
		expect(board.error.value).toBe("down");

		menu.vm.$emit("cleared");
		expect(board.refresh).toHaveBeenCalled();

		menu.vm.$emit("queue", "gh-discuss");
		await flushPromises();
		const queue = w.findComponent(QueueDialog);
		expect(queue.props()).toMatchObject({ target: "o/r", skill: "gh-discuss" });
		queue.vm.$emit("close");
		await flushPromises();
		expect(queue.props("skill")).toBeNull();
		w.findComponent(QueueMenu).vm.$emit("pick", "gh-fix");
		await flushPromises();
		expect(queue.props("skill")).toBe("gh-fix");
	});
});

describe("ProjectsView agent board", () => {
	it("shows every project the agent owns and adds one", async () => {
		const { w, router } = await render("/agents/bot/projects");
		expect(testid(w, "board-projects").text()).toBe("2 projects");
		expect((m.boardArgs[0] as Ref<string[]>).value).toEqual(["o/r", "o/s"]);
		// An agent's board has no single project to schedule for or queue to.
		expect(testid(w, "schedule-new").exists()).toBe(false);
		expect(w.findComponent(QueueMenu).exists()).toBe(false);
		expect(testid(w, "project-manual-chip").exists()).toBe(false);
		expect(w.findComponent(BoardMoreMenu).props("settingsPath")).toBe(
			"/projects/settings?agent=bot",
		);
		expect(w.findComponent(AddProjectDialog).exists()).toBe(false);

		await testid(w, "board-add-project").trigger("click");
		await flushPromises();
		expect(router.currentRoute.value.query.add).toBe("1");
		const dialog = w.findComponent(AddProjectDialog);
		expect(dialog.props("agent")).toBe("bot");

		dialog.vm.$emit("close");
		await flushPromises();
		expect(router.currentRoute.value.query.add).toBeUndefined();
		expect(w.findComponent(AddProjectDialog).exists()).toBe(false);

		w.findComponent(BoardMoreMenu).vm.$emit("add-project");
		await flushPromises();
		w.findComponent(AddProjectDialog).vm.$emit("added", "o/new", "bot");
		await flushPromises();
		expect(router.currentRoute.value.path).toBe("/agents/bot/projects/o%2Fnew");
		expect(m.listRefresh).toHaveBeenCalled();
	});

	it("names a single project in the singular and queues to it", async () => {
		m.config = { agents: [{ name: "bot", projects: [{ target: "o/r" }] }] };
		const { w } = await render("/agents/bot/projects");
		expect(testid(w, "board-projects").text()).toBe("1 project");
		w.findComponent(QueueMenu).vm.$emit("pick", "gh-fix");
		await flushPromises();
		expect(w.findComponent(QueueDialog).props("target")).toBe("o/r");
	});

	it("offers to add a project when the agent owns none", async () => {
		m.config = { agents: [{ name: "bot" }] };
		board.jobs.value = [];
		board.total.value = 0;
		const { w, router } = await render("/agents/bot/projects");
		expect(testid(w, "board-no-projects").text()).toContain(
			"bot owns no projects yet.",
		);
		await testid(w, "board-no-projects").find("button").trigger("click");
		await flushPromises();
		expect(router.currentRoute.value.query.add).toBe("1");
	});
});
