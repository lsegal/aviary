<template>
  <div class="flex h-dvh flex-col bg-white dark:bg-gray-950 lg:flex-row">
    <header class="border-b border-gray-200 bg-gray-50 px-4 py-3 dark:border-gray-800 dark:bg-gray-900 lg:hidden">
      <div class="flex items-center justify-between gap-3">
        <div class="flex items-center gap-2">
          <img :src="logoUrl" alt="Aviary logo" class="h-8 w-8 object-contain" />
          <span class="text-lg font-bold text-gray-900 dark:text-white">Aviary</span>
        </div>
        <div class="flex flex-wrap justify-end gap-1.5">
          <span
            class="flex items-center gap-1.5 rounded-full border border-gray-200 bg-white px-2.5 py-1 text-xs dark:border-gray-700 dark:bg-gray-800"
            :class="versionTextClass">
            <span class="h-1.5 w-1.5 shrink-0 rounded-full" :class="versionDotClass" />
            {{ versionLabel }}
          </span>
          <span
            class="flex items-center gap-1.5 rounded-full border border-gray-200 bg-white px-2.5 py-1 text-xs dark:border-gray-700 dark:bg-gray-800"
            :class="healthTextClass">
            <span class="h-1.5 w-1.5 shrink-0 rounded-full" :class="dotClass" />
            {{ healthLabel }}
          </span>
        </div>
      </div>
      <div class="relative mt-3">
        <div
          v-if="upgradeAvailable"
          class="mb-3 rounded-xl border border-yellow-300 bg-yellow-50 px-3 py-2 text-sm text-yellow-900 dark:border-yellow-700 dark:bg-yellow-950/40 dark:text-yellow-100"
        >
          <div class="flex items-center justify-between gap-3">
            <span>New version available: {{ latestVersion }}</span>
            <button
              type="button"
              class="rounded-lg bg-yellow-500 px-3 py-1.5 text-xs font-semibold text-yellow-950 transition hover:bg-yellow-400 disabled:opacity-60"
              :disabled="upgrading"
              @click="triggerUpgrade()">
              {{ upgrading ? "Upgrading..." : "Upgrade" }}
            </button>
          </div>
          <div v-if="versionMessage" class="mt-1 text-xs opacity-80">{{ versionMessage }}</div>
        </div>
        <nav class="flex flex-wrap gap-2">
          <div v-for="link in links" :key="link.to" class="relative">
            <router-link
              :to="linkTarget(link)"
              :class="topLevelLinkClass(link, true)">
              {{ link.label }}
            </router-link>
          </div>
        </nav>
      </div>
    </header>

    <div
      v-if="activeMobileGroup()"
      class="border-b border-gray-200 bg-white px-4 py-2 dark:border-gray-800 dark:bg-gray-950 lg:hidden"
    >
      <nav class="flex gap-2 overflow-x-auto pb-1">
        <router-link
          v-for="child in childrenForGroup(activeMobileGroup())"
          :key="child.key"
          :to="child.to"
          :class="mobileSubnavClass(child)">
          {{ child.label }}
        </router-link>
        <button
          v-if="activeMobileGroup() === 'settings'"
          type="button"
          class="shrink-0 rounded-lg px-3 py-2 text-sm font-medium text-gray-500 hover:bg-red-50 hover:text-red-500 dark:text-gray-400 dark:hover:bg-red-950/40 dark:hover:text-red-400"
          @click="auth.logout(); $router.push('/login')">
          Log out
        </button>
      </nav>
    </div>

    <nav class="hidden w-60 flex-col border-r border-gray-200 bg-gray-50 p-4 dark:border-gray-800 dark:bg-gray-900 lg:flex">
      <div class="mb-6 flex items-center gap-2">
        <img :src="logoUrl" alt="Aviary logo" class="h-8 w-8 object-contain" />
        <span class="text-lg font-bold text-gray-900 dark:text-white">Aviary</span>
      </div>
      <div class="space-y-1">
        <template v-for="link in links" :key="link.to">
          <router-link
            :to="linkTarget(link)"
            :class="topLevelLinkClass(link, false)">
            {{ link.label }}
          </router-link>
          <div
            v-if="link.children && isLinkActive(link)"
            class="mb-3 mt-1 space-y-1 pl-2"
          >
            <router-link
              v-for="child in link.children"
              :key="child.key"
              :to="child.to"
              :class="childLinkClass(child, false)">
              {{ child.label }}
            </router-link>
          </div>
        </template>
      </div>
      <div class="mt-auto flex flex-col gap-2">
        <div
          v-if="upgradeAvailable"
          class="rounded-xl border border-yellow-300 bg-yellow-50 px-3 py-2 text-sm text-yellow-900 dark:border-yellow-700 dark:bg-yellow-950/40 dark:text-yellow-100"
        >
          <div class="flex items-center justify-between gap-2">
            <span>New version available</span>
            <button
              type="button"
              class="rounded-lg bg-yellow-500 px-2.5 py-1 text-xs font-semibold text-yellow-950 transition hover:bg-yellow-400 disabled:opacity-60"
              :disabled="upgrading"
              @click="triggerUpgrade()">
              {{ upgrading ? "Upgrading..." : "Upgrade" }}
            </button>
          </div>
          <div class="mt-1 text-xs">{{ latestVersion }}</div>
          <div v-if="versionMessage" class="mt-1 text-xs opacity-80">{{ versionMessage }}</div>
        </div>
        <div class="flex flex-wrap gap-1.5 px-1">
          <span
            class="flex items-center gap-1.5 rounded-full border border-gray-200 bg-white px-2.5 py-0.5 text-xs dark:border-gray-700 dark:bg-gray-800"
            :class="versionTextClass">
            <span class="h-1.5 w-1.5 shrink-0 rounded-full" :class="versionDotClass" />
            {{ versionLabel }}
          </span>
          <span
            class="flex items-center gap-1.5 rounded-full border border-gray-200 bg-white px-2.5 py-0.5 text-xs dark:border-gray-700 dark:bg-gray-800"
            :class="healthTextClass">
            <span class="h-1.5 w-1.5 shrink-0 rounded-full" :class="dotClass" />
            {{ healthLabel }}
          </span>
        </div>
        <button
          class="w-full rounded-lg px-3 py-2 text-left text-sm font-medium text-gray-500 hover:text-red-500 dark:text-gray-500 dark:hover:text-red-400"
          @click="auth.logout(); $router.push('/login')">
          Log out
        </button>
      </div>
    </nav>

    <main class="flex min-h-0 flex-1 flex-col overflow-hidden">
      <slot />
    </main>
  </div>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { useRoute } from "vue-router";
import { useServerStatus } from "../composables/useServerStatus";
import { useAuthStore } from "../stores/auth";

type SettingsTab = "general" | "agents" | "skills" | "sessions" | "providers";
type NavGroup = "settings" | "system";

interface NavChild {
	key: string;
	label: string;
	to: string;
}

interface NavLink {
	to: string;
	label: string;
	group?: NavGroup;
	children?: NavChild[];
}

const auth = useAuthStore();
const route = useRoute();
const {
	status,
	version,
	latestVersion,
	upgradeAvailable,
	versionMessage,
	upgrading,
	triggerUpgrade,
} = useServerStatus();
const logoUrl = "/logo.png";

const settingsChildren: NavChild[] = [
	{ key: "general", label: "General", to: "/settings/general" },
	{ key: "agents", label: "Agents & Tasks", to: "/settings/agents" },
	{ key: "skills", label: "Skills", to: "/settings/skills" },
	{ key: "sessions", label: "Sessions", to: "/settings/sessions" },
	{ key: "providers", label: "Providers & Auth", to: "/settings/providers" },
];

const systemChildren: NavChild[] = [
	{ key: "tools", label: "Tools", to: "/system/tools" },
	{ key: "skills", label: "Skills", to: "/system/skills" },
	{ key: "models", label: "Models", to: "/system/models" },
	{ key: "logs", label: "Logs", to: "/logs" },
	{ key: "usage", label: "Usage", to: "/usage" },
	{ key: "jobs", label: "Jobs", to: "/jobs" },
	{ key: "daemons", label: "Daemons", to: "/daemons" },
];

const links: NavLink[] = [
	{ to: "/overview", label: "Overview" },
	{ to: "/chat", label: "Chat" },
	{
		to: "/settings",
		label: "Settings",
		group: "settings",
		children: settingsChildren,
	},
	{ to: "/system", label: "System", group: "system", children: systemChildren },
];

function settingsTabTarget(tab: SettingsTab) {
	return `/settings/${tab}`;
}

function groupTarget(group: NavGroup): string {
	if (group === "settings") {
		const tab = (route.params.tab as SettingsTab | undefined) ?? "general";
		return settingsTabTarget(tab);
	}
	return systemChildren[0]?.to ?? "/logs";
}

function linkTarget(link: NavLink) {
	if (link.group) {
		return groupTarget(link.group);
	}
	return link.to;
}

function isLinkActive(link: NavLink): boolean {
	if (link.group === "settings") {
		return route.path.startsWith("/settings");
	}
	if (link.group === "system") {
		return systemChildren.some((child) => route.path === child.to);
	}
	return route.path === link.to;
}

function isChildActive(child: NavChild): boolean {
	return route.path === child.to;
}

function topLevelLinkClass(link: NavLink, mobile: boolean): string {
	const active = isLinkActive(link);
	const base = mobile
		? "rounded-xl border px-3 py-2 text-sm font-medium transition-colors"
		: "block rounded-xl px-3 py-2 text-sm font-medium transition-colors";
	if (active) {
		return mobile
			? `${base} border-gray-300 bg-white text-gray-950 shadow-sm dark:border-gray-600 dark:bg-gray-800 dark:text-white`
			: `${base} bg-white text-gray-950 shadow-sm dark:bg-gray-800 dark:text-white`;
	}
	return mobile
		? `${base} border-gray-200 bg-white text-gray-600 hover:bg-gray-100 hover:text-gray-900 dark:border-gray-700 dark:bg-gray-800 dark:text-gray-300 dark:hover:bg-gray-700 dark:hover:text-white`
		: `${base} text-gray-600 hover:bg-gray-200 hover:text-gray-900 dark:text-gray-400 dark:hover:bg-gray-800 dark:hover:text-white`;
}

function childLinkClass(child: NavChild, mobile: boolean): string {
	const active = isChildActive(child);
	const base = "mt-1 block rounded-lg px-3 py-2 text-sm transition-colors";
	if (active) {
		return mobile
			? `${base} bg-gray-100 font-semibold text-gray-950 dark:bg-gray-800 dark:text-white`
			: `${base} bg-gray-100 font-semibold text-gray-950 dark:bg-gray-800 dark:text-white`;
	}
	return mobile
		? `${base} text-gray-500 hover:bg-gray-50 hover:text-gray-900 dark:text-gray-400 dark:hover:bg-gray-800 dark:hover:text-white`
		: `${base} text-gray-500 hover:bg-gray-100 hover:text-gray-900 dark:text-gray-400 dark:hover:bg-gray-800 dark:hover:text-white`;
}

function activeMobileGroup(): NavGroup | null {
	if (route.path.startsWith("/settings")) return "settings";
	if (systemChildren.some((child) => route.path === child.to)) return "system";
	return null;
}

function childrenForGroup(group: NavGroup | null): NavChild[] {
	if (group === "settings") return settingsChildren;
	if (group === "system") return systemChildren;
	return [];
}

function mobileSubnavClass(child: NavChild): string {
	return isChildActive(child)
		? "shrink-0 rounded-lg bg-gray-100 px-3 py-2 text-sm font-semibold text-gray-950 dark:bg-gray-800 dark:text-white"
		: "shrink-0 rounded-lg px-3 py-2 text-sm text-gray-500 hover:bg-gray-50 hover:text-gray-900 dark:text-gray-400 dark:hover:bg-gray-800 dark:hover:text-white";
}

const dotClass = computed(() => {
	if (status.value === "connected") return "bg-green-500";
	if (status.value === "disconnected") return "bg-red-500";
	return "bg-yellow-400 animate-pulse";
});

const versionDotClass = computed(() =>
	upgradeAvailable.value ? "bg-yellow-400" : "bg-green-500",
);

const displayVersion = computed(() =>
	version.value ? version.value : status.value === "disconnected" ? "—" : "…",
);

const versionLabel = computed(() => {
	if (upgradeAvailable.value && latestVersion.value) {
		return `Version ${displayVersion.value} -> ${latestVersion.value}`;
	}
	return `Version ${displayVersion.value}`;
});

const versionTextClass = computed(() =>
	upgradeAvailable.value
		? "text-yellow-700 dark:text-yellow-300"
		: "text-gray-600 dark:text-gray-400",
);

const healthLabel = computed(() => {
	if (status.value === "connected") return "Connected";
	if (status.value === "disconnected") return "Disconnected";
	return "Connecting";
});

const healthTextClass = computed(() => {
	if (status.value === "connected") return "text-green-600 dark:text-green-400";
	if (status.value === "disconnected") return "text-red-500 dark:text-red-400";
	return "text-yellow-600 dark:text-yellow-400";
});
</script>
