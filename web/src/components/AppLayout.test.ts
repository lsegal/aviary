// @vitest-environment happy-dom
import {
	flushPromises,
	RouterLinkStub,
	shallowMount,
	type VueWrapper,
} from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { defineComponent, inject, nextTick, type Ref, ref } from "vue";
import { createMemoryHistory, createRouter, type Router } from "vue-router";
import { useAgentItemDialogs } from "../composables/useAgentItemDialogs";
import { useChannelDialogs } from "../composables/useChannelDialogs";
import { useNavPrefs } from "../composables/useNavPrefs";
import {
	SIDEBAR_DEFAULT_WIDTH,
	SIDEBAR_KEY_STEP,
	SIDEBAR_MAX_WIDTH,
	SIDEBAR_MIN_WIDTH,
	useSidebarLayout,
} from "../composables/useSidebarLayout";
import { navEditGroupsKey } from "../lib/navEditGroups";
import type { ProjectSummary } from "../lib/projects";
import { useAgentFilesStore } from "../stores/agentFiles";
import { useAuthStore } from "../stores/auth";
import { useChatSessionsStore } from "../stores/chatSessions";
import { useSettingsStore } from "../stores/settings";
import AppLayout from "./AppLayout.vue";
import AutoUpdateError from "./AutoUpdateError.vue";
import ConnectionIndicator from "./ConnectionIndicator.vue";
import MobileMainNav from "./MobileMainNav.vue";
import ProjectLoadingBadge from "./projects/ProjectLoadingBadge.vue";
import ProjectManualBadge from "./projects/ProjectManualBadge.vue";
import SidebarRowAction from "./SidebarRowAction.vue";
import SettingsPageHeader from "./settings/SettingsPageHeader.vue";
import AgentFileList from "./sidebar/AgentFileList.vue";
import SidebarGearMenu from "./sidebar/SidebarGearMenu.vue";
import SidebarRailItem from "./sidebar/SidebarRailItem.vue";
import UpdateStatusBanner from "./UpdateStatusBanner.vue";
import UpgradeDialog from "./UpgradeDialog.vue";
import VersionIndicator from "./VersionIndicator.vue";

// Every network-facing dependency is replaced: the MCP client, the server
// status WebSocket, the project list poll, the display preferences fetch and
// the header's scroll listener.
const mocks = vi.hoisted(() => ({
	callTool: vi.fn(),
	server: {} as Record<string, unknown>,
	projects: null as unknown as { value: unknown[] },
	showHeader: vi.fn(),
}));

vi.mock("../composables/useMCP", () => ({
	useMCP: () => ({ callTool: mocks.callTool }),
}));
vi.mock("../composables/useServerStatus", () => ({
	useServerStatus: () => mocks.server,
}));
vi.mock("../composables/useProjects", () => ({
	useProjectList: () => ({ projects: mocks.projects }),
}));
vi.mock("../composables/useDisplayPrefs", () => ({
	useDisplayPrefs: () => ({}),
}));
vi.mock("../composables/useScrollAwayHeader", async () => {
	const { ref } = await import("vue");
	return {
		useScrollAwayHeader: () => ({ offset: ref(0), reset: mocks.showHeader }),
	};
});

interface ServerState {
	status: Ref<string>;
	version: Ref<string>;
	latestVersion: Ref<string>;
	upgradeAvailable: Ref<boolean>;
	upgradeSupported: Ref<boolean>;
	git: Ref<unknown>;
	noCheckout: Ref<boolean>;
	updateNotice: Ref<string>;
	versionMessage: Ref<string>;
	upgrading: Ref<boolean>;
	releaseUrl: Ref<string>;
	restart: Ref<unknown>;
	upgradeDialogOpen: Ref<boolean>;
	reconnect: ReturnType<typeof vi.fn>;
	openUpgradeDialog: ReturnType<typeof vi.fn>;
	triggerUpgrade: ReturnType<typeof vi.fn>;
	triggerRestart: ReturnType<typeof vi.fn>;
	cancelRestart: ReturnType<typeof vi.fn>;
}

let server: ServerState;
let router: Router;
let wrapper: VueWrapper | undefined;
let wide = true;

const Blank = defineComponent({ render: () => null });

// Renders both of the narrow-screen row's slots, its links and its "+".
const MobileSubNavStub = defineComponent({
	template:
		'<div data-testid="mobile-subnav"><slot /><slot name="action" /></div>',
});

function project(
	name: string,
	extra: Partial<ProjectSummary> = {},
): ProjectSummary {
	return { name, running: 0, waiting: 0, issues: 0, ...extra };
}

function agent(
	name: string,
	{
		projects = [],
		...extra
	}: { projects?: string[] } & Record<string, unknown> = {},
) {
	return {
		name,
		channels: [],
		tasks: [],
		...extra,
		projects: projects.map((target) => ({ target })),
	};
}

function setAgents(...agents: ReturnType<typeof agent>[]) {
	useSettingsStore().config = { agents } as never;
}

async function mountAt(
	path: string,
	props: Record<string, unknown> = {},
	slots: Record<string, unknown> = {},
) {
	await router.push(path);
	await router.isReady();
	wrapper = shallowMount(AppLayout, {
		props,
		slots: slots as never,
		attachTo: document.body,
		global: {
			plugins: [router],
			stubs: {
				RouterLink: RouterLinkStub,
				"router-link": RouterLinkStub,
				MobileSubNav: MobileSubNavStub,
				// The page in the default slot renders for real.
				NavEditGroupsProbe: false,
			},
			renderStubDefaultSlot: true,
		},
	});
	await flushPromises();
	return wrapper;
}

function byTestId(w: VueWrapper, id: string) {
	return w.find(`[data-testid="${id}"]`);
}

function railItems(w: VueWrapper) {
	return w.findAllComponents(SidebarRailItem);
}

function railItem(w: VueWrapper, id: string) {
	const item = railItems(w).find(
		(c) =>
			c.attributes("data-testid") === id || c.vm.$attrs["data-testid"] === id,
	);
	if (!item) throw new Error(`no rail item ${id}`);
	return item;
}

function rowActions(w: VueWrapper, label: string) {
	return w
		.findAllComponents(SidebarRowAction)
		.filter((c) => c.props("label") === label);
}

beforeEach(() => {
	vi.stubGlobal(
		"fetch",
		vi.fn(() => Promise.reject(new Error("no network in tests"))),
	);
	wide = true;
	vi.spyOn(window, "matchMedia").mockImplementation(
		(query: string) => ({ matches: wide, media: query }) as MediaQueryList,
	);
	setActivePinia(createPinia());
	mocks.callTool.mockReset();
	mocks.showHeader.mockReset();
	mocks.projects = ref<ProjectSummary[]>([]);
	server = {
		status: ref("connected"),
		version: ref(""),
		latestVersion: ref(""),
		upgradeAvailable: ref(false),
		upgradeSupported: ref(true),
		git: ref(null),
		noCheckout: ref(false),
		updateNotice: ref(""),
		versionMessage: ref(""),
		upgrading: ref(false),
		releaseUrl: ref(""),
		restart: ref(null),
		upgradeDialogOpen: ref(false),
		reconnect: vi.fn(),
		openUpgradeDialog: vi.fn(),
		triggerUpgrade: vi.fn(),
		triggerRestart: vi.fn(),
		cancelRestart: vi.fn(),
	};
	mocks.server = server as unknown as Record<string, unknown>;
	const sidebar = useSidebarLayout();
	sidebar.resetWidth();
	sidebar.collapsed.value = false;
	const nav = useNavPrefs();
	nav.prefs.value = { order: {}, hidden: {} };
	nav.showHidden.value = false;
	useChannelDialogs().close();
	useAgentItemDialogs().close();
	router = createRouter({
		history: createMemoryHistory(),
		routes: [
			{ path: "/agents/:agent/chat/:sessionId?", component: Blank },
			{ path: "/agents/:agent/:rest(.*)*", component: Blank },
			{ path: "/:pathMatch(.*)*", component: Blank },
		],
	});
});

afterEach(() => {
	wrapper?.unmount();
	wrapper = undefined;
	vi.useRealTimers();
	vi.restoreAllMocks();
	vi.unstubAllGlobals();
	document.body.innerHTML = "";
});

describe("AppLayout navigation", () => {
	it("lists every agent, with the active agent's pages and owned projects", async () => {
		setAgents(
			agent("alice", { projects: ["acme/web", "acme/api", "acme/new"] }),
			agent("bob"),
		);
		mocks.projects.value = [
			project("acme/web", { running: 2 }),
			project("acme/api", { manual: true }),
			project("acme/new", { loading: true }),
		];
		const w = await mountAt("/agents/alice/chat");

		const list = byTestId(w, "sidebar-agent-list");
		expect(list.exists()).toBe(true);
		const agentLinks = list.findAll("[data-agent]");
		expect(agentLinks.map((a) => a.attributes("data-agent"))).toEqual([
			"alice",
			"bob",
		]);
		// Alice owns two running jobs; her badge counts them.
		expect(byTestId(w, "agent-running-badge").text()).toBe("2");
		// Only the active agent lists its pages.
		expect(byTestId(w, "sidebar-agent-alice").exists()).toBe(true);
		expect(byTestId(w, "sidebar-agent-bob").exists()).toBe(false);
		// Bob's link opens the same page for him.
		expect(
			w
				.findAllComponents(RouterLinkStub)
				.find((l) => l.text() === "bob")
				?.props("to"),
		).toBe("/agents/bob/chat");
		const projects = byTestId(w, "sidebar-agent-projects");
		expect(projects.text()).toContain("acme/web");
		expect(projects.text()).toContain("acme/api");
		expect(w.findAllComponents(ProjectManualBadge)).toHaveLength(1);
		expect(w.findAllComponents(ProjectLoadingBadge)).toHaveLength(1);
		// The add-project link opens the add dialog on the agent's board.
		expect(rowActions(w, "Add project")[0].props("to")).toEqual({
			path: "/agents/alice/projects",
			query: { add: "1" },
		});
		// The narrow-screen row lists one pill per agent, plus System.
		const items = w.findComponent(MobileMainNav).props("items") as {
			key: string;
			active: boolean;
		}[];
		expect(items.map((i) => i.key)).toEqual([
			"agent:alice",
			"agent:bob",
			"system",
		]);
		expect(items[0].active).toBe(true);
		// The mobile second row shows Alice's pages.
		expect(w.text()).toContain("Agent Settings");
	});

	it("keeps another agent's page when switching from an agent page", async () => {
		setAgents(agent("alice"), agent("bob"));
		const w = await mountAt("/agents/alice/channels/slack");
		expect(
			w
				.findAllComponents(RouterLinkStub)
				.find((l) => l.text() === "bob")
				?.props("to"),
		).toBe("/agents/bob/channels");
		await router.push("/agents/alice/projects/acme");
		await flushPromises();
		expect(
			w
				.findAllComponents(RouterLinkStub)
				.find((l) => l.text() === "bob")
				?.props("to"),
		).toBe("/agents/bob/projects");
		await router.push("/agents/alice/settings");
		await flushPromises();
		expect(
			w
				.findAllComponents(RouterLinkStub)
				.find((l) => l.text() === "bob")
				?.props("to"),
		).toBe("/agents/bob/settings");
	});

	it("names an unsaved agent New agent and shows the full tree", async () => {
		setAgents(agent("alice"));
		const w = await mountAt("/agents/_1/settings");
		expect(byTestId(w, "sidebar-sole-agent").exists()).toBe(false);
		const pending = byTestId(w, "sidebar-agent-list").find('[data-agent="_1"]');
		expect(pending.text()).toContain("New agent");
	});

	it("lifts a sole agent's pages to the top level", async () => {
		setAgents(agent("solo", { projects: ["acme/web"] }));
		mocks.projects.value = [project("acme/web", { running: 1 })];
		const w = await mountAt("/agents/solo/projects");
		const sole = byTestId(w, "sidebar-sole-agent");
		expect(sole.attributes("data-agent")).toBe("solo");
		expect(byTestId(w, "sidebar-agents").exists()).toBe(false);
		expect(byTestId(w, "sidebar-row-projects").exists()).toBe(true);
		expect(
			sole.find('[data-testid="sidebar-agent-projects"]').text(),
		).toContain("acme/web");
		const items = w.findComponent(MobileMainNav).props("items") as {
			key: string;
			active: boolean;
			running?: number;
		}[];
		expect(items.map((i) => i.key)).toEqual([
			"chat",
			"projects",
			"channels",
			"tasks",
			"files",
			"settings",
			"system",
		]);
		expect(items.find((i) => i.key === "projects")).toMatchObject({
			active: true,
			running: 1,
		});
		// On its Code Loop page the second row lists its projects only.
		expect(w.text()).toContain("acme/web");
	});

	it("hides System and the upgrade prompt in a minimal session", async () => {
		useAuthStore().minimal = true;
		setAgents(agent("solo"));
		server.upgradeAvailable.value = true;
		const w = await mountAt("/agents/solo/projects");
		expect(byTestId(w, "sidebar-system").exists()).toBe(false);
		expect(byTestId(w, "sidebar-system-list").exists()).toBe(false);
		const items = w.findComponent(MobileMainNav).props("items") as {
			key: string;
		}[];
		expect(items.map((i) => i.key)).toEqual(["projects", "tasks"]);
		expect(w.text()).not.toContain("New version available");
		expect(w.findComponent(VersionIndicator).props("upgradeAvailable")).toBe(
			false,
		);
		expect(w.findComponent(SidebarGearMenu).props("minimal")).toBe(true);
		useSidebarLayout().collapsed.value = true;
		await nextTick();
		expect(() => railItem(w, "sidebar-rail-system")).toThrow();
		expect(railItem(w, "sidebar-rail-version").exists()).toBe(true);
	});

	it("toggles the System group and opens it on a System page", async () => {
		setAgents(agent("alice"), agent("bob"));
		const w = await mountAt("/system/usage");
		const toggle = byTestId(w, "sidebar-system-toggle");
		expect(toggle.attributes("aria-expanded")).toBe("true");
		expect(byTestId(w, "sidebar-system-usage").exists()).toBe(true);
		// System's pages form the narrow-screen second row.
		expect(
			w.findAll('[data-active="true"]').some((l) => l.text().includes("Usage")),
		).toBe(true);
		await toggle.trigger("click");
		expect(toggle.attributes("aria-expanded")).toBe("false");
		expect(toggle.attributes("aria-label")).toBe("Expand System");
		expect(byTestId(w, "sidebar-system-list").exists()).toBe(false);
		await toggle.trigger("click");
		expect(byTestId(w, "sidebar-system-list").exists()).toBe(true);
	});

	it("lists unowned watches under System", async () => {
		setAgents(agent("alice"), agent("bob"));
		mocks.projects.value = [project("acme/loose", { running: 3 })];
		const w = await mountAt("/system/overview");
		// The System group's collapse is shared by every layout; open it.
		const toggle = byTestId(w, "sidebar-system-toggle");
		if (toggle.attributes("aria-expanded") === "false")
			await toggle.trigger("click");
		expect(byTestId(w, "sidebar-system-watch:acme/loose").text()).toContain(
			"Watch: acme/loose",
		);
	});

	it("shows and hides hidden pages", async () => {
		setAgents(agent("alice"));
		const nav = useNavPrefs();
		nav.prefs.value = {
			order: {},
			hidden: { agentPages: ["files"], systemPages: ["logs"] },
		};
		const w = await mountAt("/agents/alice/chat");
		const button = byTestId(w, "sidebar-show-hidden");
		expect(button.text()).toBe("Show 2 hidden pages");
		expect(byTestId(w, "sidebar-row-files").exists()).toBe(false);
		await button.trigger("click");
		expect(nav.showHidden.value).toBe(true);
		expect(button.text()).toBe("Hide hidden pages");
		expect(byTestId(w, "sidebar-row-files").exists()).toBe(true);
		nav.prefs.value = { order: {}, hidden: { agentPages: ["files"] } };
		nav.showHidden.value = false;
		await nextTick();
		expect(button.text()).toBe("Show 1 hidden page");
	});

	it("passes the route's crumbs, or the page's own, to the header", async () => {
		setAgents(agent("alice"));
		const w = await mountAt("/agents/alice/settings", {
			trail: { from: "/agents/alice/settings", crumbs: [{ label: "Extra" }] },
		});
		const header = w.findComponent(SettingsPageHeader);
		const crumbs = header.props("crumbs") as { label: string; to?: unknown }[];
		expect(crumbs.at(-1)?.label).toBe("Extra");
		expect(crumbs.at(-2)?.to).toBe("/agents/alice/settings");
		await w.setProps({ crumbs: [{ label: "Custom" }], trail: null });
		expect(w.findComponent(SettingsPageHeader).props("crumbs")).toEqual([
			{ label: "Custom" },
		]);
		// A new page brings the header's title row back.
		await router.push("/agents/alice/chat");
		await flushPromises();
		expect(mocks.showHeader).toHaveBeenCalled();
	});

	it("names a chat crumb from its loaded session", async () => {
		setAgents(agent("alice"), agent("bob"));
		useChatSessionsStore().$patch((s) => {
			(s as unknown as { byAgent: Record<string, unknown[]> }).byAgent = {
				alice: [{ id: "s1", title: "Planning" }],
			};
		});
		const w = await mountAt("/agents/alice/chat/s1");
		const crumbs = w.findComponent(SettingsPageHeader).props("crumbs") as {
			label: string;
		}[];
		expect(crumbs.map((c) => c.label)).toContain("Planning");
	});

	it("provides the sidebar editor's lists to the page", async () => {
		let groups:
			| { value: { list: string; title: string; items: { key: string }[] }[] }
			| undefined;
		const Page = defineComponent({
			name: "NavEditGroupsProbe",
			setup() {
				groups = inject(navEditGroupsKey) as typeof groups;
				return () => null;
			},
		});
		setAgents(agent("alice"), agent("bob"));
		await mountAt("/agents/alice/chat", {}, { default: Page });
		expect(groups?.value.map((g) => g.title)).toEqual([
			"Agent pages",
			"System pages",
		]);
		setAgents(agent("solo"));
		await nextTick();
		expect(groups?.value[0].title).toBe("Pages");
		// A minimal session leaves out the pages it never shows.
		useAuthStore().minimal = true;
		await nextTick();
		expect(groups?.value.map((g) => g.list)).toEqual(["agentPages"]);
		expect(groups?.value[0].items.map((i) => i.key)).toEqual([
			"projects",
			"tasks",
		]);
	});

	it("shows no header without crumbs", async () => {
		const w = await mountAt("/somewhere/else");
		expect(w.findComponent(SettingsPageHeader).exists()).toBe(false);
	});
});

describe("AppLayout agent item groups", () => {
	it("lists a sole agent's files only on the Files page", async () => {
		setAgents(agent("solo"));
		vi.spyOn(useAgentFilesStore(), "files").mockImplementation((a) =>
			a === "solo" ? ["AGENTS.md", "NOTES.md"] : [],
		);
		const w = await mountAt("/agents/solo/chat");
		expect(w.findComponent(AgentFileList).exists()).toBe(false);
		await router.push("/agents/solo/files?file=NOTES.md");
		await flushPromises();
		expect(w.findComponent(AgentFileList).props("agent")).toBe("solo");
		const active = w.findAll('[data-active="true"]');
		expect(active.map((a) => a.text())).toEqual(["NOTES.md"]);
		// Without a file query the first file is current.
		await router.push("/agents/solo/files");
		await flushPromises();
		expect(w.findAll('[data-active="true"]').map((a) => a.text())).toEqual([
			"AGENTS.md",
		]);
		await byTestId(w, "mobile-row-action").trigger("click");
		expect(useAgentItemDialogs().dialog.value).toMatchObject({
			kind: "file",
			agent: "solo",
			mode: "create",
		});
	});

	it("lists channels on Channels pages, disabled ones last", async () => {
		setAgents(
			agent("alice", {
				channels: [
					{ type: "slack", name: "off", enabled: false },
					{ type: "discord", name: "on" },
				],
			}),
			agent("bob"),
		);
		const w = await mountAt("/agents/alice/channels");
		const disabled = byTestId(w, "mobile-channel-disabled");
		expect(disabled.exists()).toBe(true);
		const links = w.findAll('[data-disabled="true"]');
		expect(links.map((l) => l.text())).toEqual(["off"]);
		expect(links[0].attributes("title")).toBe("Disabled");
		const labels = w
			.findAllComponents(RouterLinkStub)
			.map((l) => l.props("to"))
			.filter(
				(to) =>
					typeof to === "string" && to.startsWith("/agents/alice/channels/"),
			);
		expect(labels).toEqual([
			"/agents/alice/channels/on",
			"/agents/alice/channels/off",
		]);
		await byTestId(w, "mobile-row-action").trigger("click");
		expect(useChannelDialogs().dialog.value).toEqual({
			mode: "create",
			agent: "alice",
		});
	});

	it("lists tasks on Tasks pages, paused ones last, without workflows", async () => {
		setAgents(
			agent("alice", {
				tasks: [
					{ name: "nightly", enabled: false },
					{ name: "hourly" },
					{ name: "on-message", trigger: "message" },
					{ name: "" },
				],
			}),
			agent("bob"),
		);
		const w = await mountAt("/agents/alice/tasks?task=hourly");
		expect(byTestId(w, "mobile-task-paused").exists()).toBe(true);
		const paused = w.findAll('[data-paused="true"]');
		expect(paused.map((l) => l.text())).toEqual(["nightly"]);
		expect(paused[0].attributes("title")).toBe("Paused");
		expect(w.text()).not.toContain("on-message");
		expect(w.findAll('[data-active="true"]').map((a) => a.text())).toEqual([
			"Scheduled Tasks",
			"hourly",
		]);
		await byTestId(w, "mobile-row-action").trigger("click");
		expect(useAgentItemDialogs().dialog.value).toMatchObject({
			kind: "task",
			agent: "alice",
		});
	});

	it("opens the add dialogs from the sidebar rows", async () => {
		setAgents(agent("alice"), agent("bob"));
		const w = await mountAt("/agents/alice/chat");
		await rowActions(w, "Add channel")[0].vm.$emit("click");
		expect(useChannelDialogs().dialog.value).toEqual({
			mode: "create",
			agent: "alice",
		});
		await rowActions(w, "Add task")[0].vm.$emit("click");
		expect(useAgentItemDialogs().dialog.value).toMatchObject({
			kind: "task",
			agent: "alice",
		});
		await rowActions(w, "Add file")[0].vm.$emit("click");
		expect(useAgentItemDialogs().dialog.value).toMatchObject({
			kind: "file",
			agent: "alice",
		});
	});

	it("opens a sole agent's add dialogs from its rows", async () => {
		setAgents(agent("solo"));
		const w = await mountAt("/agents/solo/chat");
		await rowActions(w, "Add channel")[0].vm.$emit("click");
		expect(useChannelDialogs().dialog.value).toEqual({
			mode: "create",
			agent: "solo",
		});
		expect(rowActions(w, "Add project")[0].props("to")).toEqual({
			path: "/agents/solo/projects",
			query: { add: "1" },
		});
	});

	it("creates a chat and opens it", async () => {
		setAgents(agent("alice"), agent("bob"));
		mocks.callTool.mockResolvedValue(JSON.stringify({ id: "new1" }));
		const w = await mountAt("/agents/alice/settings");
		const action = rowActions(w, "New chat")[0];
		action.vm.$emit("click");
		await nextTick();
		expect(rowActions(w, "New chat")[0].vm.$attrs.disabled).toBe(true);
		await flushPromises();
		expect(mocks.callTool).toHaveBeenCalledWith("session_create", {
			agent: "alice",
		});
		expect(router.currentRoute.value.path).toBe("/agents/alice/chat/new1");
		expect(rowActions(w, "New chat")[0].vm.$attrs.disabled).toBe(false);
	});

	it("logs a failed chat creation and re-enables New chat", async () => {
		setAgents(agent("solo"));
		const error = vi.spyOn(console, "error").mockImplementation(() => {});
		mocks.callTool.mockRejectedValue(new Error("boom"));
		const w = await mountAt("/agents/solo/settings");
		rowActions(w, "New chat")[0].vm.$emit("click");
		await flushPromises();
		expect(error).toHaveBeenCalledWith(
			"Failed to create chat",
			expect.any(Error),
		);
		expect(router.currentRoute.value.path).toBe("/agents/solo/settings");
		expect(rowActions(w, "New chat")[0].vm.$attrs.disabled).toBe(false);
	});
});

describe("AppLayout sidebar resize", () => {
	function pointer(type: string, init: PointerEventInit) {
		return new PointerEvent(type, { bubbles: true, cancelable: true, ...init });
	}

	it("resizes by dragging the edge", async () => {
		const w = await mountAt("/agents");
		const handle = byTestId(w, "sidebar-resize");
		handle.element.dispatchEvent(
			pointer("pointerdown", { button: 0, clientX: 100 }),
		);
		await nextTick();
		expect(handle.classes()).toContain("bg-blue-500/40");
		expect(document.body.style.cursor).toBe("col-resize");
		window.dispatchEvent(pointer("pointermove", { clientX: 150 }));
		await nextTick();
		expect(useSidebarLayout().width.value).toBe(SIDEBAR_DEFAULT_WIDTH + 50);
		expect(handle.attributes("aria-valuenow")).toBe(
			String(SIDEBAR_DEFAULT_WIDTH + 50),
		);
		window.dispatchEvent(pointer("pointerup", {}));
		await nextTick();
		expect(handle.classes()).not.toContain("bg-blue-500/40");
		expect(document.body.style.cursor).toBe("");
		expect(document.body.style.userSelect).toBe("");
		// Moves after release no longer resize.
		window.dispatchEvent(pointer("pointermove", { clientX: 400 }));
		expect(useSidebarLayout().width.value).toBe(SIDEBAR_DEFAULT_WIDTH + 50);
	});

	it("snaps to the rail when dragged far below the minimum", async () => {
		const w = await mountAt("/agents");
		byTestId(w, "sidebar-resize").element.dispatchEvent(
			pointer("pointerdown", { button: 0, clientX: 300 }),
		);
		window.dispatchEvent(pointer("pointermove", { clientX: 250 }));
		window.dispatchEvent(pointer("pointermove", { clientX: 0 }));
		await nextTick();
		const sidebar = useSidebarLayout();
		expect(sidebar.collapsed.value).toBe(true);
		// Expanding again restores the width the drag started from.
		expect(sidebar.width.value).toBe(SIDEBAR_DEFAULT_WIDTH);
		expect(byTestId(w, "sidebar").exists()).toBe(false);
		expect(byTestId(w, "sidebar-rail").exists()).toBe(true);
		expect(document.body.style.cursor).toBe("");
	});

	it("ignores drags with other buttons", async () => {
		const w = await mountAt("/agents");
		byTestId(w, "sidebar-resize").element.dispatchEvent(
			pointer("pointerdown", { button: 2, clientX: 100 }),
		);
		window.dispatchEvent(pointer("pointermove", { clientX: 200 }));
		expect(useSidebarLayout().width.value).toBe(SIDEBAR_DEFAULT_WIDTH);
		expect(document.body.style.cursor).toBe("");
	});

	it("resizes with the arrow, Home and End keys", async () => {
		const w = await mountAt("/agents");
		const handle = byTestId(w, "sidebar-resize");
		const width = useSidebarLayout().width;
		await handle.trigger("keydown", { key: "ArrowRight" });
		expect(width.value).toBe(SIDEBAR_DEFAULT_WIDTH + SIDEBAR_KEY_STEP);
		await handle.trigger("keydown", { key: "ArrowLeft" });
		await handle.trigger("keydown", { key: "ArrowLeft" });
		expect(width.value).toBe(SIDEBAR_DEFAULT_WIDTH - SIDEBAR_KEY_STEP);
		await handle.trigger("keydown", { key: "End" });
		expect(width.value).toBe(SIDEBAR_MAX_WIDTH);
		await handle.trigger("keydown", { key: "Home" });
		expect(width.value).toBe(SIDEBAR_MIN_WIDTH);
		const other = new KeyboardEvent("keydown", { key: "a", cancelable: true });
		handle.element.dispatchEvent(other);
		expect(other.defaultPrevented).toBe(false);
		expect(width.value).toBe(SIDEBAR_MIN_WIDTH);
		expect(handle.attributes("aria-valuemin")).toBe(String(SIDEBAR_MIN_WIDTH));
		expect(handle.attributes("aria-valuemax")).toBe(String(SIDEBAR_MAX_WIDTH));
	});

	it("resets the width on double-click", async () => {
		const w = await mountAt("/agents");
		useSidebarLayout().setWidth(400);
		await nextTick();
		expect(byTestId(w, "sidebar").attributes("style")).toContain(
			"width: 400px",
		);
		await byTestId(w, "sidebar-resize").trigger("dblclick");
		expect(useSidebarLayout().width.value).toBe(SIDEBAR_DEFAULT_WIDTH);
	});
});

describe("AppLayout collapsed sidebar", () => {
	it("collapses to the rail and expands again", async () => {
		const w = await mountAt("/agents");
		await byTestId(w, "sidebar-collapse").trigger("click");
		expect(byTestId(w, "sidebar").exists()).toBe(false);
		expect(byTestId(w, "sidebar-rail").exists()).toBe(true);
		railItem(w, "sidebar-expand").vm.$emit("click");
		await nextTick();
		expect(useSidebarLayout().collapsed.value).toBe(false);
		expect(byTestId(w, "sidebar").exists()).toBe(true);
	});

	it("toggles with the keyboard shortcut on wide screens only", async () => {
		await mountAt("/agents");
		const mac = /Mac|iPhone|iPad/.test(navigator.platform);
		const press = (init: KeyboardEventInit) => {
			const e = new KeyboardEvent("keydown", { cancelable: true, ...init });
			window.dispatchEvent(e);
			return e;
		};
		const mod = mac ? { metaKey: true } : { ctrlKey: true };
		const collapsed = useSidebarLayout().collapsed;
		expect(press({ key: "b", ...mod }).defaultPrevented).toBe(true);
		expect(collapsed.value).toBe(true);
		press({ key: "B", ...mod });
		expect(collapsed.value).toBe(false);
		// Other keys and modifiers leave it alone.
		press({ key: "b" });
		press({ key: "b", ...mod, shiftKey: true });
		press({ key: "b", ...mod, altKey: true });
		press({ key: "x", ...mod });
		const handled = new KeyboardEvent("keydown", {
			key: "b",
			cancelable: true,
			...mod,
		});
		handled.preventDefault();
		window.dispatchEvent(handled);
		expect(collapsed.value).toBe(false);
		// Below lg the mobile header replaces the sidebar.
		wide = false;
		expect(press({ key: "b", ...mod }).defaultPrevented).toBe(false);
		expect(collapsed.value).toBe(false);
	});

	it("stops listening for the shortcut once unmounted", async () => {
		const w = await mountAt("/agents");
		w.unmount();
		wrapper = undefined;
		const mac = /Mac|iPhone|iPad/.test(navigator.platform);
		window.dispatchEvent(
			new KeyboardEvent("keydown", {
				key: "b",
				...(mac ? { metaKey: true } : { ctrlKey: true }),
			}),
		);
		expect(useSidebarLayout().collapsed.value).toBe(false);
	});

	it("shows a sole agent's pages with their flyout items on the rail", async () => {
		setAgents(
			agent("solo", {
				projects: ["acme/idle", "acme/busy"],
				channels: [{ type: "slack", name: "team" }],
				tasks: [{ name: "nightly" }, { name: "wf", trigger: "message" }],
			}),
		);
		mocks.projects.value = [
			project("acme/idle"),
			project("acme/busy", { running: 2 }),
		];
		useChatSessionsStore().$patch((s) => {
			(s as unknown as { byAgent: Record<string, unknown[]> }).byAgent = {
				solo: [{ id: "c1", title: "First" }],
			};
		});
		useSidebarLayout().collapsed.value = true;
		const w = await mountAt("/agents/solo/chat/c1");
		expect(railItem(w, "sidebar-rail-chat").props("items")).toEqual([
			{ key: "c1", label: "First", to: "/agents/solo/chat/c1", active: true },
		]);
		expect(railItem(w, "sidebar-rail-projects").props("items")).toEqual([
			expect.objectContaining({ key: "acme/busy", badge: 2, dimmed: false }),
			expect.objectContaining({ key: "acme/idle", badge: 0, dimmed: true }),
		]);
		expect(railItem(w, "sidebar-rail-channels").props("items")).toEqual([
			{
				key: "team",
				label: "team",
				to: "/agents/solo/channels/team",
				active: false,
			},
		]);
		expect(railItem(w, "sidebar-rail-tasks").props("items")).toEqual([
			{
				key: "nightly",
				label: "nightly",
				to: "/agents/solo/tasks/nightly",
				active: false,
			},
		]);
		expect(railItem(w, "sidebar-rail-settings").props("items")).toBeUndefined();
		expect(railItem(w, "sidebar-rail-chat").props("active")).toBe(true);
	});

	it("shows agents, the active agent's pages and the current System page on the rail", async () => {
		setAgents(agent("alice"), agent("bob"));
		useSidebarLayout().collapsed.value = true;
		const w = await mountAt("/agents/alice/settings");
		const agents = railItems(w).filter(
			(c) =>
				c.vm.$attrs["data-testid"] === "sidebar-rail-agent" ||
				c.attributes("data-testid") === "sidebar-rail-agent",
		);
		expect(agents.map((a) => a.props("label"))).toEqual(["alice", "bob"]);
		// The active agent lists its pages instead of a flyout.
		expect(agents[0].props("items")).toBeUndefined();
		expect(
			(agents[1].props("items") as { key: string }[]).map((i) => i.key),
		).toContain("chat");
		expect(railItem(w, "sidebar-rail-settings").props("active")).toBe(true);
		expect(railItem(w, "sidebar-rail-agents").props("active")).toBe(false);
		await router.push("/system/usage");
		await flushPromises();
		expect(railItem(w, "sidebar-rail-system-usage").props("label")).toBe(
			"Usage",
		);
		expect(
			(railItem(w, "sidebar-rail-system").props("items") as { key: string }[])
				.length,
		).toBeGreaterThan(0);
	});
});

describe("AppLayout sidebar peek", () => {
	async function collapsedRail() {
		useSidebarLayout().collapsed.value = true;
		vi.useFakeTimers();
		const w = await mountAt("/agents");
		return w;
	}

	it("peeks the sidebar after the pointer rests on the rail", async () => {
		const w = await collapsedRail();
		const rail = byTestId(w, "sidebar-rail");
		await rail.trigger("mouseenter");
		vi.advanceTimersByTime(200);
		// Moving restarts the wait.
		await rail.trigger("mousemove");
		vi.advanceTimersByTime(200);
		await nextTick();
		expect(byTestId(w, "sidebar").exists()).toBe(false);
		vi.advanceTimersByTime(100);
		await nextTick();
		const sidebar = byTestId(w, "sidebar");
		expect(sidebar.attributes("data-peek")).toBe("true");
		expect(sidebar.classes()).toContain("fixed");
		// The peek has no resize handle and a pin button instead of collapse.
		expect(byTestId(w, "sidebar-resize").exists()).toBe(false);
		expect(byTestId(w, "sidebar-collapse").exists()).toBe(false);
		// Further moves while open change nothing.
		await rail.trigger("mousemove");
		expect(byTestId(w, "sidebar").exists()).toBe(true);
	});

	it("does not peek when the pointer leaves first or rests on an icon", async () => {
		const w = await collapsedRail();
		const rail = byTestId(w, "sidebar-rail");
		await rail.trigger("mouseenter");
		await rail.trigger("mouseleave");
		vi.advanceTimersByTime(1000);
		await nextTick();
		expect(byTestId(w, "sidebar").exists()).toBe(false);
		const button = document.createElement("button");
		rail.element.appendChild(button);
		button.dispatchEvent(new MouseEvent("mousemove", { bubbles: true }));
		vi.advanceTimersByTime(1000);
		await nextTick();
		expect(byTestId(w, "sidebar").exists()).toBe(false);
	});

	async function peeked() {
		const w = await collapsedRail();
		await byTestId(w, "sidebar-rail").trigger("mouseenter");
		vi.advanceTimersByTime(300);
		await nextTick();
		expect(byTestId(w, "sidebar").exists()).toBe(true);
		return w;
	}

	it("closes soon after the pointer leaves, unless it comes back", async () => {
		const w = await peeked();
		const sidebar = byTestId(w, "sidebar");
		await sidebar.trigger("mouseleave");
		vi.advanceTimersByTime(200);
		await sidebar.trigger("mouseenter");
		vi.advanceTimersByTime(500);
		await nextTick();
		expect(byTestId(w, "sidebar").exists()).toBe(true);
		await sidebar.trigger("mouseleave");
		vi.advanceTimersByTime(300);
		await nextTick();
		expect(byTestId(w, "sidebar").exists()).toBe(false);
		// Leaving a closed sidebar schedules nothing.
		expect(vi.getTimerCount()).toBe(0);
	});

	it("closes on following a link, but not on other clicks", async () => {
		const w = await peeked();
		await byTestId(w, "sidebar-title").trigger("click");
		expect(byTestId(w, "sidebar").exists()).toBe(true);
		await byTestId(w, "sidebar-agents").trigger("click");
		expect(byTestId(w, "sidebar").exists()).toBe(false);
	});

	it("closes when focus leaves the overlay", async () => {
		const w = await peeked();
		const sidebar = byTestId(w, "sidebar");
		const inside = byTestId(w, "sidebar-pin").element;
		sidebar.element.dispatchEvent(
			new FocusEvent("focusout", { relatedTarget: inside }),
		);
		await nextTick();
		expect(byTestId(w, "sidebar").exists()).toBe(true);
		sidebar.element.dispatchEvent(
			new FocusEvent("focusout", { relatedTarget: document.body }),
		);
		await nextTick();
		expect(byTestId(w, "sidebar").exists()).toBe(false);
	});

	it("closes on Escape", async () => {
		const w = await peeked();
		window.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape" }));
		await nextTick();
		expect(byTestId(w, "sidebar").exists()).toBe(false);
		// Escape without a peek does nothing.
		window.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape" }));
		expect(useSidebarLayout().collapsed.value).toBe(true);
	});

	it("pins the sidebar open, ending the peek", async () => {
		const w = await peeked();
		await byTestId(w, "sidebar-pin").trigger("click");
		expect(useSidebarLayout().collapsed.value).toBe(false);
		const sidebar = byTestId(w, "sidebar");
		expect(sidebar.attributes("data-peek")).toBeUndefined();
		expect(byTestId(w, "sidebar-resize").exists()).toBe(true);
		// Focus leaving a pinned sidebar changes nothing.
		sidebar.element.dispatchEvent(
			new FocusEvent("focusout", { relatedTarget: null }),
		);
		await sidebar.trigger("mouseleave");
		vi.advanceTimersByTime(1000);
		await nextTick();
		expect(byTestId(w, "sidebar").exists()).toBe(true);
	});
});

describe("AppLayout version and update status", () => {
	async function rail() {
		useSidebarLayout().collapsed.value = true;
		return mountAt("/agents");
	}

	it("labels the rail's version", async () => {
		server.status.value = "connecting";
		const w = await rail();
		expect(railItem(w, "sidebar-rail-version").props("label")).toBe(
			"Version …",
		);
		server.status.value = "disconnected";
		await nextTick();
		expect(railItem(w, "sidebar-rail-version").props("label")).toBe(
			"Version unknown",
		);
		server.version.value = "v1.2.3";
		await nextTick();
		expect(railItem(w, "sidebar-rail-version").props("label")).toBe(
			"Version v1.2.3",
		);
	});

	it("labels the rail's update and opens the upgrade dialog", async () => {
		server.upgradeAvailable.value = true;
		const w = await rail();
		const update = railItem(w, "sidebar-rail-update");
		expect(update.props("label")).toBe("Update available");
		expect(update.props("disabled")).toBe(false);
		server.latestVersion.value = "v2.0.0";
		await nextTick();
		expect(railItem(w, "sidebar-rail-update").props("label")).toBe(
			"Update available: v2.0.0",
		);
		railItem(w, "sidebar-rail-update").vm.$emit("click");
		expect(server.openUpgradeDialog).toHaveBeenCalled();
	});

	it("disables upgrading while a restart is pending or unsupported", async () => {
		server.upgradeAvailable.value = true;
		server.restart.value = { phase: "restarting" };
		const w = await rail();
		expect(railItem(w, "sidebar-rail-update").props("disabled")).toBe(true);
		const gear = w.findAllComponents(SidebarGearMenu);
		expect(
			gear.every(
				(g) => g.props("restartDisabled") && g.props("restartPending"),
			),
		).toBe(true);
		server.restart.value = { phase: "idle" };
		await nextTick();
		expect(railItem(w, "sidebar-rail-update").props("disabled")).toBe(false);
		server.upgradeSupported.value = false;
		await nextTick();
		expect(railItem(w, "sidebar-rail-update").props("disabled")).toBe(true);
		server.upgradeSupported.value = true;
		server.upgrading.value = true;
		await nextTick();
		expect(railItem(w, "sidebar-rail-update").props("disabled")).toBe(true);
	});

	it("shows the update notice with its release link and messages", async () => {
		server.upgradeAvailable.value = true;
		server.latestVersion.value = "v2.0.0";
		server.releaseUrl.value = "https://example.test/release";
		server.versionMessage.value = "Local changes";
		const w = await mountAt("/agents");
		const links = w.findAll('a[href="https://example.test/release"]');
		expect(links).toHaveLength(2);
		expect(links[0].text()).toBe("v2.0.0");
		expect(
			w.findAll('[data-testid="version-message"]').map((m) => m.text()),
		).toEqual(["Local changes", "Local changes"]);
		const buttons = w.findAll("button").filter((b) => b.text() === "Upgrade");
		expect(buttons).toHaveLength(2);
		await buttons[0].trigger("click");
		await buttons[1].trigger("click");
		expect(server.openUpgradeDialog).toHaveBeenCalledTimes(2);
		server.releaseUrl.value = "";
		server.upgrading.value = true;
		await nextTick();
		expect(w.findAll('a[href="https://example.test/release"]')).toHaveLength(0);
		expect(w.text()).toContain("2.0.0");
		const busy = w.findAll("button").filter((b) => b.text() === "Upgrading...");
		expect(busy).toHaveLength(2);
		expect(busy.every((b) => b.attributes("disabled") !== undefined)).toBe(
			true,
		);
	});

	it("explains why an automatic update is waiting", async () => {
		server.upgradeAvailable.value = true;
		server.restart.value = {
			phase: "idle",
			autoUpdate: { state: "waiting-restart" },
		};
		const w = await mountAt("/agents");
		const status = () =>
			w.findAll('[data-testid="auto-update-status"]').map((s) => s.text());
		expect(status()).toEqual([
			"Auto update is waiting for the restart in progress.",
			"Auto update is waiting for the restart in progress.",
		]);
		server.restart.value = { phase: "idle", autoUpdate: { state: "applying" } };
		await nextTick();
		expect(status()[0]).toBe("Auto update is applying…");
		server.git.value = { branch: "main" };
		await nextTick();
		expect(status()[0]).toBe("Auto update is pulling the checkout…");
		server.restart.value = { phase: "idle", autoUpdate: { state: "failed" } };
		await nextTick();
		expect(status()).toEqual([]);
	});

	it("retries a failed automatic update", async () => {
		server.upgradeAvailable.value = true;
		const w = await mountAt("/agents");
		const error = w.findComponent(AutoUpdateError);
		error.vm.$emit("retry");
		error.vm.$emit("remove-lock");
		expect(server.triggerUpgrade).toHaveBeenNthCalledWith(1, "now");
		expect(server.triggerUpgrade).toHaveBeenNthCalledWith(2, "now", {
			removeStaleLock: true,
		});
	});

	it("shows the result of a git update, or a missing checkout", async () => {
		server.updateNotice.value = "Updated to abc123";
		const w = await mountAt("/agents");
		expect(
			w.findAll('[data-testid="update-notice"]').map((n) => n.text()),
		).toEqual(["Updated to abc123", "Updated to abc123"]);
		server.updateNotice.value = "";
		server.noCheckout.value = true;
		await nextTick();
		const missing = w.findAll('[data-testid="update-no-checkout"]');
		expect(missing).toHaveLength(2);
		expect(missing[0].text()).toBe("Dev build — no checkout found");
		server.versionMessage.value = "Set AVIARY_CHECKOUT";
		await nextTick();
		expect(w.find('[data-testid="update-no-checkout"]').text()).toContain(
			"Set AVIARY_CHECKOUT",
		);
	});

	it("wires the status controls to the server", async () => {
		const w = await mountAt("/agents");
		w.findComponent(VersionIndicator).vm.$emit("upgrade");
		expect(server.openUpgradeDialog).toHaveBeenCalled();
		w.findComponent(ConnectionIndicator).vm.$emit("retry");
		expect(server.reconnect).toHaveBeenCalled();
		w.findComponent(SidebarGearMenu).vm.$emit("restart");
		expect(server.triggerRestart).toHaveBeenCalled();
		w.findComponent(UpdateStatusBanner).vm.$emit("cancel");
		expect(server.cancelRestart).toHaveBeenCalled();
		const dialog = w.findComponent(UpgradeDialog);
		dialog.vm.$emit("confirm", "later");
		expect(server.triggerUpgrade).toHaveBeenCalledWith("later");
		server.upgradeDialogOpen.value = true;
		dialog.vm.$emit("close");
		expect(server.upgradeDialogOpen.value).toBe(false);
	});
});
