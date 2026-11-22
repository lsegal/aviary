import { createRouter, createWebHistory } from "vue-router";
import { installUnsavedChanges } from "../composables/useUnsavedChanges";
import { needsOnboarding } from "../lib/onboarding";
import { useAuthStore } from "../stores/auth";
import { prefetchSettingsViewWhenIdle, SettingsView } from "./settingsView";

const ProjectsView = () => import("../views/ProjectsView.vue");
const ProjectJobDetail = () =>
	import("../components/projects/ProjectJobDetail.vue");

const router = createRouter({
	history: createWebHistory(),
	routes: [
		{ path: "/", redirect: "/system/overview" },
		{ path: "/login", component: () => import("../views/LoginView.vue") },
		{
			path: "/onboarding",
			component: () => import("../views/OnboardingView.vue"),
			meta: { requiresAuth: true },
		},
		// Channels moved out of Agent Settings into their own sidebar section.
		{
			path: "/agents/:agent/settings/channels",
			redirect: (to) =>
				`/agents/${encodeURIComponent(String(to.params.agent))}/channels`,
		},
		// Everything an agent owns lives under its entry in the sidebar.
		// One record for every SettingsView-backed agent page, so moving between
		// them (or adding an agent from /agents) keeps the unsaved draft.
		{
			path: "/agents/:agent?/:subtab(settings|files|tasks)?/:section(permissions)?",
			component: SettingsView,
			meta: { requiresAuth: true, view: "settings" },
		},
		// A channel's settings form, opened from its view's gear or its
		// sidebar "…" Edit; the only place a channel is edited.
		{
			path: "/agents/:agent/channels/:channel/settings",
			component: SettingsView,
			meta: { requiresAuth: true, view: "settings" },
		},
		// The agent's channels, and one channel's conversation.
		{
			path: "/agents/:agent/channels/:channel?",
			component: () => import("../views/ChannelView.vue"),
			meta: { requiresAuth: true },
		},
		// One scheduled task's session, which its runs share; its settings
		// stay on the Tasks page (?task=), behind the view's gear.
		{
			path: "/agents/:agent/tasks/:task",
			component: () => import("../views/TaskView.vue"),
			meta: { requiresAuth: true },
		},
		{
			path: "/agents/:agent/chat/:sessionId?",
			component: () => import("../views/ChatView.vue"),
			meta: { requiresAuth: true },
		},
		// Code Loop: the board for every project the agent owns, and
		// one project's board. Their settings live in /projects/settings.
		{
			path: "/agents/:agent/projects",
			component: ProjectsView,
			meta: { requiresAuth: true },
		},
		// A job's details open inside its project page, so the project header
		// and its Chat and Logs panels stay mounted.
		{
			path: "/agents/:agent/projects/:project",
			component: ProjectsView,
			meta: { requiresAuth: true },
			children: [{ path: "jobs/:number(\\d+)", component: ProjectJobDetail }],
		},
		// Every agent's projects and the settings they all share, opened from
		// the boards' gear.
		{
			path: "/projects/settings",
			component: SettingsView,
			meta: { requiresAuth: true, view: "settings" },
		},
		// The instance's own pages.
		{ path: "/system", redirect: "/system/overview" },
		{
			path: "/system/overview",
			component: () => import("../views/OverviewView.vue"),
			meta: { requiresAuth: true },
		},
		{
			path: "/system/:tab(settings|providers|sessions|skills)",
			component: SettingsView,
			meta: { requiresAuth: true, view: "settings" },
		},
		{
			path: "/system/usage",
			component: () => import("../views/UsageView.vue"),
			meta: { requiresAuth: true },
		},
		// Task runs and their analytics live on an agent's Scheduled Tasks.
		{
			path: "/system/jobs",
			meta: { requiresAuth: true },
			redirect: () => ({ path: "/agents/_0/tasks", query: {} }),
		},
		{
			path: "/system/tools",
			component: () => import("../views/SystemToolsView.vue"),
			meta: { requiresAuth: true },
		},
		{
			path: "/system/models",
			component: () => import("../views/SystemModelsView.vue"),
			meta: { requiresAuth: true },
		},
		{
			path: "/system/logs",
			component: () => import("../views/LogsView.vue"),
			meta: { requiresAuth: true },
		},
		{
			path: "/system/daemons",
			component: () => import("../views/DaemonsView.vue"),
			meta: { requiresAuth: true },
		},
		// Runtime watches (aviary watch) that no agent owns.
		{
			path: "/system/watches/:project",
			component: ProjectsView,
			meta: { requiresAuth: true },
			children: [{ path: "jobs/:number(\\d+)", component: ProjectJobDetail }],
		},
	],
});

router.beforeEach(async (to) => {
	const auth = useAuthStore();
	// A server running without token authentication needs no login.
	if (
		(to.meta.requiresAuth || to.path === "/login") &&
		!auth.isLoggedIn &&
		(await auth.checkAuthDisabled())
	) {
		if (to.path === "/login") return "/agents";
	}
	if (to.meta.requiresAuth && !auth.isLoggedIn) {
		return "/login";
	}
	// With no agent configured, every page sends the user through the setup
	// wizard first, wherever the login or link landed.
	if (
		to.meta.requiresAuth &&
		to.path !== "/onboarding" &&
		(await needsOnboarding())
	) {
		return "/onboarding";
	}
});
installUnsavedChanges(router);
void router.isReady().then(prefetchSettingsViewWhenIdle);

export default router;
