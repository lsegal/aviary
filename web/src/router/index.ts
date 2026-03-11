import { createRouter, createWebHistory } from "vue-router";
import { useAuthStore } from "../stores/auth";

const router = createRouter({
	history: createWebHistory(),
	routes: [
		{ path: "/", redirect: "/overview" },
		{ path: "/login", component: () => import("../views/LoginView.vue") },
		{
			path: "/overview",
			component: () => import("../views/OverviewView.vue"),
			meta: { requiresAuth: true },
		},
		{
			path: "/chat",
			component: () => import("../views/ChatView.vue"),
			meta: { requiresAuth: true },
		},
		{
			path: "/agents",
			redirect: "/settings/agents",
			meta: { requiresAuth: true },
		},
		{
			path: "/tasks",
			component: () => import("../views/TasksView.vue"),
			meta: { requiresAuth: true },
		},
		{
			path: "/sessions",
			redirect: "/settings/sessions",
			meta: { requiresAuth: true },
		},
		{
			path: "/settings",
			redirect: (to) => {
				const allowed = new Set([
					"general",
					"agents",
					"skills",
					"sessions",
					"providers",
				]);
				const tab =
					typeof to.query.tab === "string" && allowed.has(to.query.tab)
						? to.query.tab
						: "general";
				return `/settings/${tab}`;
			},
			meta: { requiresAuth: true },
		},
		{
			path: "/settings/:tab(general|agents|skills|sessions|providers)",
			component: () => import("../views/SettingsView.vue"),
			meta: { requiresAuth: true },
		},
		{
			path: "/logs",
			component: () => import("../views/LogsView.vue"),
			meta: { requiresAuth: true },
		},
		{
			path: "/system/tools",
			component: () => import("../views/SystemToolsView.vue"),
			meta: { requiresAuth: true },
		},
		{
			path: "/system/skills",
			component: () => import("../views/SystemSkillsView.vue"),
			meta: { requiresAuth: true },
		},
		{
			path: "/system/models",
			component: () => import("../views/SystemModelsView.vue"),
			meta: { requiresAuth: true },
		},
		{
			path: "/usage",
			component: () => import("../views/UsageView.vue"),
			meta: { requiresAuth: true },
		},
		{
			path: "/jobs",
			component: () => import("../views/JobsView.vue"),
			meta: { requiresAuth: true },
		},
		{
			path: "/daemons",
			component: () => import("../views/DaemonsView.vue"),
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
