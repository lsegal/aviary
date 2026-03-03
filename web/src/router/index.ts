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
			redirect: "/settings?tab=agents",
			meta: { requiresAuth: true },
		},
		{
			path: "/tasks",
			redirect: "/settings?tab=agents",
			meta: { requiresAuth: true },
		},
		{
			path: "/sessions",
			redirect: "/settings?tab=sessions",
			meta: { requiresAuth: true },
		},
		{
			path: "/settings",
			component: () => import("../views/SettingsView.vue"),
			meta: { requiresAuth: true },
		},
		{
			path: "/logs",
			component: () => import("../views/LogsView.vue"),
			meta: { requiresAuth: true },
		},
		{
			path: "/usage",
			component: () => import("../views/UsageView.vue"),
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
