import { createRouter, createWebHistory } from "vue-router";
import { useAuthStore } from "../stores/auth";

const SettingsView = () => import("../views/SettingsView.vue");
const ProjectsView = () => import("../views/ProjectsView.vue");
const ProjectRunView = () => import("../views/ProjectRunView.vue");

const router = createRouter({
	history: createWebHistory(),
	routes: [
		{ path: "/", redirect: "/system/overview" },
		{ path: "/login", component: () => import("../views/LoginView.vue") },
		{
			path: "/onboarding",
			component: () => import("../views/OverviewView.vue"),
			meta: { requiresAuth: true, forceOnboarding: true },
		},
		// Everything an agent owns lives under its entry in the sidebar.
		{
			path: "/agents",
			component: SettingsView,
			meta: { requiresAuth: true },
		},
		{
			path: "/agents/:agent",
			redirect: (to) => `/agents/${encodeURIComponent(String(to.params.agent))}/chat`,
		},
		{
			path: "/agents/:agent/chat/:sessionId?",
			component: () => import("../views/ChatView.vue"),
			meta: { requiresAuth: true },
		},
		{
			path: "/agents/:agent/:subtab(settings|permissions|channels|files|tasks|projects)",
			component: SettingsView,
			meta: { requiresAuth: true },
		},
		{
			path: "/agents/:agent/projects/:project",
			component: ProjectsView,
			meta: { requiresAuth: true },
		},
		{
			path: "/agents/:agent/projects/:project/jobs/:number(\\d+)",
			component: ProjectRunView,
			meta: { requiresAuth: true },
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
			meta: { requiresAuth: true },
		},
		{
			path: "/system/usage",
			component: () => import("../views/UsageView.vue"),
			meta: { requiresAuth: true },
		},
		{
			path: "/system/jobs",
			component: () => import("../views/JobsView.vue"),
			meta: { requiresAuth: true },
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
		},
		{
			path: "/system/watches/:project/jobs/:number(\\d+)",
			component: ProjectRunView,
			meta: { requiresAuth: true },
		},
	],
});

router.beforeEach((to) => {
	const auth = useAuthStore();
	if (to.meta.requiresAuth && !auth.isLoggedIn) {
		return "/login";
	}
});

export default router;
