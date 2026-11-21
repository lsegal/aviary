<template>
  <div class="flex h-dvh flex-col bg-white dark:bg-gray-950 lg:flex-row">
    <header class="border-b border-gray-200 bg-gray-50 px-4 py-3 dark:border-gray-800 dark:bg-gray-900 lg:hidden">
      <div class="flex items-center justify-between gap-3">
        <div class="flex items-center gap-2">
          <img :src="logoUrl" alt="Aviary logo" class="h-8 w-8 object-contain" />
          <span class="text-lg font-bold text-gray-900 dark:text-white">Aviary</span>
          <div class="flex items-center gap-1">
            <a
              v-for="link in brandLinks"
              :key="link.label"
              :href="link.href"
              :aria-label="link.label"
              target="_blank"
              rel="noreferrer"
              class="inline-flex h-8 w-8 items-center justify-center rounded-lg text-gray-500 transition hover:bg-gray-200 hover:text-gray-900 dark:text-gray-400 dark:hover:bg-gray-800 dark:hover:text-white">
              <component
                v-if="link.iconComponent"
                :is="link.iconComponent"
                aria-hidden="true"
                class="h-4 w-4" />
              <svg
                v-else
                viewBox="0 0 24 24"
                fill="currentColor"
                aria-hidden="true"
                class="h-4 w-4">
                <path :d="link.iconPath" />
              </svg>
            </a>
          </div>
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
        <UpdateStatusBanner :restart="restart" class="mb-3" @cancel="cancelRestart()" />
        <div v-if="upgradeAvailable"
          class="mb-3 rounded-xl border border-yellow-300 bg-yellow-50 px-3 py-2 text-sm text-yellow-900 dark:border-yellow-700 dark:bg-yellow-950/40 dark:text-yellow-100">
          <div class="flex items-center justify-between gap-3">
            <span>New version available:
              <a v-if="releaseUrl" :href="releaseUrl" target="_blank" rel="noreferrer" class="underline">{{ shortVersion(latestVersion) }}</a>
              <template v-else>{{ shortVersion(latestVersion) }}</template>
            </span>
            <button type="button"
              class="rounded-lg bg-yellow-500 px-3 py-1.5 text-xs font-semibold text-yellow-950 transition hover:bg-yellow-400 disabled:opacity-60"
              :disabled="upgrading || restartPending || !upgradeSupported" @click="openUpgradeDialog()">
              {{ upgrading ? "Upgrading..." : "Upgrade" }}
            </button>
          </div>
          <div v-if="versionMessage" class="mt-1 text-xs opacity-80" data-testid="version-message">{{ versionMessage }}</div>
          <div v-if="autoUpdateWaiting" class="mt-1 text-xs" data-testid="auto-update-status">{{ autoUpdateWaiting }}</div>
          <div v-if="autoUpdateError" class="mt-1 text-xs font-medium text-red-700 [overflow-wrap:anywhere] dark:text-red-300" data-testid="auto-update-error">{{ autoUpdateError }}</div>
        </div>
        <div v-else-if="updateNotice" data-testid="update-notice"
          class="mb-3 rounded-xl border border-green-300 bg-green-50 px-3 py-2 text-xs text-green-900 dark:border-green-700 dark:bg-green-950/40 dark:text-green-100">
          {{ updateNotice }}
        </div>
        <div v-else-if="noCheckout" data-testid="update-no-checkout"
          class="mb-3 rounded-xl border border-gray-300 bg-gray-50 px-3 py-2 text-xs text-gray-700 dark:border-gray-700 dark:bg-gray-800/60 dark:text-gray-300">
          <div class="font-medium">Dev build — no checkout found</div>
          <div v-if="versionMessage" class="mt-1 opacity-80">{{ versionMessage }}</div>
        </div>
        <nav v-if="soleAgent" class="flex flex-wrap gap-2" aria-label="Main">
          <router-link v-for="child in soleAgent.children" :key="child.key" :to="child.to"
            :class="topLevelLinkClass(isChildActive(child), true)">
            {{ child.label }}
          </router-link>
          <router-link to="/system/overview" :class="topLevelLinkClass(systemActive, true)">System</router-link>
          <router-link :to="addAgentLink" data-testid="mobile-add-agent" :class="topLevelLinkClass(false, true)">
            + Add Agent
          </router-link>
        </nav>
        <nav v-else class="flex flex-wrap gap-2" aria-label="Main">
          <router-link v-for="a in agentNav" :key="a.name" :to="a.to" :class="topLevelLinkClass(a.active, true)">
            {{ a.label }}
          </router-link>
          <router-link :to="addAgentLink" data-testid="mobile-add-agent" :class="topLevelLinkClass(false, true)">
            + Add Agent
          </router-link>
          <router-link to="/system/overview" :class="topLevelLinkClass(systemActive, true)">System</router-link>
        </nav>
      </div>
    </header>

    <div v-if="mobileChildren.length"
      class="border-b border-gray-200 bg-white px-4 py-2 dark:border-gray-800 dark:bg-gray-950 lg:hidden">
      <nav class="flex gap-2 overflow-x-auto pb-1">
        <router-link v-for="child in mobileChildren" :key="child.key" :to="child.to"
          :class="mobileSubnavClass(child)">
          {{ child.label }}<span v-if="child.count" class="ml-1.5 text-xs font-normal text-gray-400">{{ child.count }}</span><ProjectRunningBadge
            :running="child.running ?? 0" class="ml-1.5" />
        </router-link>
        <div v-if="systemActive" class="ml-auto flex shrink-0 items-center gap-1">
          <button type="button" aria-label="Restart" title="Restart" data-testid="restart-button"
            class="inline-flex h-8 w-8 items-center justify-center rounded-lg text-gray-500 transition hover:bg-gray-200 hover:text-gray-900 disabled:opacity-50 dark:text-gray-400 dark:hover:bg-gray-800 dark:hover:text-white"
            :disabled="upgrading || restartPending" @click="triggerRestart()">
            <ArrowPathIcon aria-hidden="true" class="h-4 w-4" />
          </button>
          <button type="button" aria-label="Log out" title="Log out"
            class="inline-flex h-8 w-8 items-center justify-center rounded-lg text-gray-500 transition hover:bg-red-50 hover:text-red-500 dark:text-gray-400 dark:hover:bg-red-950/40 dark:hover:text-red-400"
            @click="auth.logout(); $router.push('/login')">
            <ArrowRightOnRectangleIcon aria-hidden="true" class="h-4 w-4" />
          </button>
        </div>
      </nav>
    </div>

    <nav
      class="hidden w-60 flex-col border-r border-gray-200 bg-gray-50 p-4 dark:border-gray-800 dark:bg-gray-900 lg:flex">
      <div class="mb-6 flex items-center gap-2">
        <img :src="logoUrl" alt="Aviary logo" class="h-8 w-8 object-contain" />
        <span class="text-lg font-bold text-gray-900 dark:text-white">Aviary</span>
        <div class="ml-auto flex items-center gap-1">
          <a
            v-for="link in brandLinks"
            :key="link.label"
            :href="link.href"
            :aria-label="link.label"
            target="_blank"
            rel="noreferrer"
            class="inline-flex h-8 w-8 items-center justify-center rounded-lg text-gray-500 transition hover:bg-gray-200 hover:text-gray-900 dark:text-gray-400 dark:hover:bg-gray-800 dark:hover:text-white">
            <component
              v-if="link.iconComponent"
              :is="link.iconComponent"
              aria-hidden="true"
              class="h-4 w-4" />
            <svg
              v-else
              viewBox="0 0 24 24"
              fill="currentColor"
              aria-hidden="true"
              class="h-4 w-4">
              <path :d="link.iconPath" />
            </svg>
          </a>
        </div>
      </div>
      <div class="min-h-0 flex-1 space-y-1 overflow-y-auto" data-testid="sidebar-nav">
        <div v-if="soleAgent" class="mb-3 space-y-1" data-testid="sidebar-sole-agent" :data-agent="soleAgent.name">
          <template v-for="child in soleAgent.children" :key="child.key">
            <router-link :to="child.to" :class="[topLevelLinkClass(isChildActive(child), false), 'flex items-center gap-2']">
              <span class="min-w-0 flex-1 truncate">{{ child.label }}</span>
              <span v-if="child.key === 'projects' && soleAgent.running > 0" data-testid="agent-running-badge"
                class="shrink-0 rounded-full bg-blue-600 px-1.5 text-xs font-semibold text-white">{{ soleAgent.running }}</span>
              <span v-else-if="child.count" class="shrink-0 text-xs text-gray-400">{{ child.count }}</span>
            </router-link>
            <ChatSessionList v-if="child.key === 'chat'" :agent="soleAgent.name" />
            <div v-if="child.key === 'projects' && projectsOpen" class="space-y-1 pl-2" data-testid="sidebar-agent-projects">
              <router-link v-for="p in soleAgent.projects" :key="p.key" :to="p.to"
                :class="[childLinkClass(isChildActive(p), false), 'flex items-center gap-2']">
                <span class="min-w-0 flex-1 truncate">{{ p.label }}</span>
                <ProjectRunningBadge :running="p.running ?? 0" />
              </router-link>
              <router-link :to="addProjectLink(soleAgent.name)" data-testid="sidebar-add-project"
                class="mt-1 block rounded-lg px-3 py-2 text-sm text-blue-600 transition-colors hover:bg-gray-100 dark:text-blue-400 dark:hover:bg-gray-800">
                + Add project
              </router-link>
            </div>
          </template>
        </div>
        <template v-else>
        <router-link to="/agents" :class="[topLevelLinkClass(false, false), 'flex items-center']" data-testid="sidebar-agents">
          Agents
        </router-link>
        <div class="mb-3 mt-1 space-y-1 pl-2" data-testid="sidebar-agent-list">
          <template v-for="a in agentNav" :key="a.name">
            <router-link :to="a.to" :data-agent="a.name"
              :class="[childLinkClass(a.active && !a.children.length, false), 'flex items-center gap-2', a.active ? 'font-semibold text-gray-950 dark:text-white' : '']">
              <span class="min-w-0 flex-1 truncate">{{ a.label }}</span>
              <span v-if="a.running > 0" data-testid="agent-running-badge"
                class="shrink-0 rounded-full bg-blue-600 px-1.5 text-xs font-semibold text-white">{{ a.running }}</span>
            </router-link>
            <div v-if="a.children.length" class="space-y-1 border-l border-gray-200 pl-2 dark:border-gray-800" :data-testid="`sidebar-agent-${a.name}`">
              <template v-for="child in a.children" :key="child.key">
                <router-link :to="child.to" :class="[childLinkClass(isChildActive(child), false), 'flex items-center gap-2']">
                  <span class="min-w-0 flex-1 truncate">{{ child.label }}</span>
                  <span v-if="child.count" class="shrink-0 text-xs text-gray-400">{{ child.count }}</span>
                </router-link>
                <ChatSessionList v-if="child.key === 'chat'" :agent="a.name" />
                <div v-if="child.key === 'projects' && projectsOpen" class="space-y-1 pl-2" data-testid="sidebar-agent-projects">
                  <router-link v-for="p in a.projects" :key="p.key" :to="p.to"
                    :class="[childLinkClass(isChildActive(p), false), 'flex items-center gap-2']">
                    <span class="min-w-0 flex-1 truncate">{{ p.label }}</span>
                    <ProjectRunningBadge :running="p.running ?? 0" />
                  </router-link>
                  <router-link :to="addProjectLink(a.name)" data-testid="sidebar-add-project"
                    class="mt-1 block rounded-lg px-3 py-2 text-sm text-blue-600 transition-colors hover:bg-gray-100 dark:text-blue-400 dark:hover:bg-gray-800">
                    + Add project
                  </router-link>
                </div>
              </template>
            </div>
          </template>
          <router-link :to="addAgentLink" data-testid="sidebar-add-agent"
            class="mt-1 block rounded-lg px-3 py-2 text-sm text-blue-600 transition-colors hover:bg-gray-100 dark:text-blue-400 dark:hover:bg-gray-800">
            + Add Agent
          </router-link>
        </div>
        </template>

        <router-link to="/system/overview" :class="[topLevelLinkClass(false, false), 'flex items-center']" data-testid="sidebar-system">
          System
        </router-link>
        <div class="mb-3 mt-1 space-y-1 pl-2" data-testid="sidebar-system-list">
          <router-link v-for="child in systemPrimary" :key="child.key" :to="child.to" :class="childLinkClass(isChildActive(child), false)">
            {{ child.label }}
          </router-link>
          <button type="button" data-testid="sidebar-system-more" :aria-expanded="systemMoreOpen"
            class="mt-1 flex w-full items-center gap-1 rounded-lg px-3 py-2 text-left text-sm text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-900 dark:text-gray-400 dark:hover:bg-gray-800 dark:hover:text-white"
            @click="systemMoreToggled = !systemMoreOpen">
            <span aria-hidden="true" class="inline-block w-3 transition-transform" :class="systemMoreOpen ? 'rotate-90' : ''">›</span>
            {{ systemMoreOpen ? "Less" : "More" }}
          </button>
          <template v-if="systemMoreOpen">
            <router-link v-for="child in systemMore" :key="child.key" :to="child.to"
              :class="[childLinkClass(isChildActive(child), false), 'flex items-center gap-2']">
              <span class="min-w-0 flex-1 truncate">{{ child.label }}</span>
              <span v-if="child.count" class="shrink-0 text-xs text-gray-400">{{ child.count }}</span>
              <ProjectRunningBadge :running="child.running ?? 0" />
            </router-link>
          </template>
        </div>
        <router-link v-if="soleAgent" :to="addAgentLink" data-testid="sidebar-add-agent"
          class="block rounded-lg px-3 py-2 text-sm text-blue-600 transition-colors hover:bg-gray-100 dark:text-blue-400 dark:hover:bg-gray-800">
          + Add Agent
        </router-link>
      </div>
      <div class="mt-auto flex flex-col gap-2">
        <UpdateStatusBanner :restart="restart" @cancel="cancelRestart()" />
        <div v-if="upgradeAvailable"
          class="rounded-xl border border-yellow-300 bg-yellow-50 px-3 py-2 text-sm text-yellow-900 dark:border-yellow-700 dark:bg-yellow-950/40 dark:text-yellow-100">
          <div class="flex items-center justify-between gap-2">
            <span>New version available</span>
            <button type="button"
              class="rounded-lg bg-yellow-500 px-2.5 py-1 text-xs font-semibold text-yellow-950 transition hover:bg-yellow-400 disabled:opacity-60"
              :disabled="upgrading || restartPending || !upgradeSupported" @click="openUpgradeDialog()">
              {{ upgrading ? "Upgrading..." : "Upgrade" }}
            </button>
          </div>
          <div class="mt-1 text-xs">
            <a v-if="releaseUrl" :href="releaseUrl" target="_blank" rel="noreferrer" class="underline">{{ shortVersion(latestVersion) }}</a>
            <template v-else>{{ shortVersion(latestVersion) }}</template>
          </div>
          <div v-if="versionMessage" class="mt-1 text-xs opacity-80" data-testid="version-message">{{ versionMessage }}</div>
          <div v-if="autoUpdateWaiting" class="mt-1 text-xs" data-testid="auto-update-status">{{ autoUpdateWaiting }}</div>
          <div v-if="autoUpdateError" class="mt-1 text-xs font-medium text-red-700 [overflow-wrap:anywhere] dark:text-red-300" data-testid="auto-update-error">{{ autoUpdateError }}</div>
        </div>
        <div v-else-if="updateNotice" data-testid="update-notice"
          class="rounded-xl border border-green-300 bg-green-50 px-3 py-2 text-xs text-green-900 dark:border-green-700 dark:bg-green-950/40 dark:text-green-100">
          {{ updateNotice }}
        </div>
        <div v-else-if="noCheckout" data-testid="update-no-checkout"
          class="rounded-xl border border-gray-300 bg-gray-50 px-3 py-2 text-xs text-gray-700 dark:border-gray-700 dark:bg-gray-800/60 dark:text-gray-300">
          <div class="font-medium">Dev build — no checkout found</div>
          <div v-if="versionMessage" class="mt-1 opacity-80">{{ versionMessage }}</div>
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
        <div class="flex items-center gap-1 px-1">
          <button type="button" aria-label="Restart" title="Restart" data-testid="restart-button"
            class="inline-flex h-8 w-8 items-center justify-center rounded-lg text-gray-500 transition hover:bg-gray-200 hover:text-gray-900 disabled:opacity-50 dark:text-gray-400 dark:hover:bg-gray-800 dark:hover:text-white"
            :disabled="upgrading || restartPending" @click="triggerRestart()">
            <ArrowPathIcon aria-hidden="true" class="h-4 w-4" />
          </button>
          <button type="button" aria-label="Log out" title="Log out"
            class="inline-flex h-8 w-8 items-center justify-center rounded-lg text-gray-500 transition hover:bg-red-50 hover:text-red-500 dark:text-gray-400 dark:hover:bg-red-950/40 dark:hover:text-red-400"
            @click="auth.logout(); $router.push('/login')">
            <ArrowRightOnRectangleIcon aria-hidden="true" class="h-4 w-4" />
          </button>
        </div>
      </div>
    </nav>

    <main class="flex min-h-0 flex-1 flex-col overflow-hidden">
      <slot />
    </main>

    <UpgradeDialog :open="upgradeDialogOpen" :latest-version="latestVersion" :release-url="releaseUrl"
      :jobs="restart?.jobs ?? []" :busy="upgrading" :git="git" @close="upgradeDialogOpen = false"
      @confirm="triggerUpgrade($event)" />
  </div>
</template>

<script lang="ts">
import { ref } from "vue";

// Shared by every page's layout, so the System expander stays as toggled.
const systemMoreState = ref<boolean | null>(null);
</script>

<script setup lang="ts">
import ArrowPathIcon from "@heroicons/vue/24/outline/ArrowPathIcon";
import ArrowRightOnRectangleIcon from "@heroicons/vue/24/outline/ArrowRightOnRectangleIcon";
import GlobeAltIcon from "@heroicons/vue/24/outline/GlobeAltIcon";
import { type Component, computed, markRaw } from "vue";
import { useRoute } from "vue-router";
import { useProjectList } from "../composables/useProjects";
import { useServerStatus } from "../composables/useServerStatus";
import { projectOwner } from "../lib/projectSettings";
import { shortVersion } from "../lib/version";
import { type ProjectSummary, projectPath } from "../lib/projects";
import { useAuthStore } from "../stores/auth";
import { useSettingsStore } from "../stores/settings";
import UpdateStatusBanner from "./UpdateStatusBanner.vue";
import ProjectRunningBadge from "./projects/ProjectRunningBadge.vue";
import ChatSessionList from "./chat/ChatSessionList.vue";
import UpgradeDialog from "./UpgradeDialog.vue";

interface NavChild {
	key: string;
	label: string;
	to: string;
	count?: string;
	/** A project's running jobs, shown as a gray badge when any. */
	running?: number;
}

interface AgentNav {
	name: string;
	label: string;
	to: string;
	active: boolean;
	running: number;
	/** The active agent's pages; empty for the others. */
	children: NavChild[];
	projects: NavChild[];
}

interface BrandLink {
	label: string;
	href: string;
	iconComponent?: Component;
	iconPath?: string;
}

const auth = useAuthStore();
const route = useRoute();
const {
	status,
	version,
	latestVersion,
	upgradeAvailable,
	upgradeSupported,
	git,
	noCheckout,
	updateNotice,
	versionMessage,
	upgrading,
	releaseUrl,
	restart,
	upgradeDialogOpen,
	openUpgradeDialog,
	triggerUpgrade,
	triggerRestart,
	cancelRestart,
} = useServerStatus();
const restartPending = computed(
	() => !!restart.value && restart.value.phase !== "idle",
);
// Why an automatic update has not applied yet, and how its last attempt
// failed, shown at the update notice.
const autoUpdateWaiting = computed(() => {
	const auto = restart.value?.autoUpdate;
	switch (auto?.state) {
		case "waiting-jobs": {
			const n = auto.jobs ?? 0;
			return `Auto update is waiting for ${n === 1 ? "1 running job" : `${n} running jobs`} to finish.`;
		}
		case "waiting-restart":
			return "Auto update is waiting for the pending restart.";
		case "applying":
			return git.value ? "Auto update is pulling the checkout…" : "Auto update is applying…";
		default:
			return "";
	}
});
const autoUpdateError = computed(() => {
	const err = restart.value?.autoUpdate?.error;
	return err ? `Auto update failed: ${err}` : "";
});
const logoUrl = "/logo.png";
const brandLinks: BrandLink[] = [
	{
		label: "Open Aviary website",
		href: "https://aviary.bot",
		iconComponent: markRaw(GlobeAltIcon),
	},
	{
		label: "Open Aviary GitHub repository",
		href: "https://github.com/lsegal/aviary",
		iconPath:
			"M12 .297c-6.63 0-12 5.373-12 12 0 5.303 3.438 9.8 8.205 11.385.6.113.82-.258.82-.577 0-.285-.01-1.04-.015-2.04-3.338.724-4.042-1.61-4.042-1.61C4.422 17.97 3.633 17.7 3.633 17.7c-1.087-.744.084-.729.084-.729 1.205.084 1.84 1.236 1.84 1.236 1.07 1.834 2.807 1.304 3.492.997.108-.776.417-1.305.76-1.605-2.665-.3-5.466-1.332-5.466-5.93 0-1.31.465-2.38 1.235-3.22-.135-.303-.54-1.523.105-3.176 0 0 1.005-.322 3.3 1.23a11.49 11.49 0 0 1 3.003-.404c1.02.005 2.045.138 3.003.404 2.295-1.552 3.3-1.23 3.3-1.23.645 1.653.24 2.873.12 3.176.765.84 1.23 1.91 1.23 3.22 0 4.61-2.805 5.625-5.475 5.92.42.36.81 1.096.81 2.22 0 1.606-.015 2.896-.015 3.286 0 .315.21.69.825.57C20.565 22.092 24 17.592 24 12.297c0-6.627-5.373-12-12-12",
	},
];

// An agent's pages, in sidebar order. Settings holds its General,
// Permissions and Channels tabs.
const AGENT_PAGES = [
	{ key: "chat", label: "Chats" },
	{ key: "projects", label: "GitHub Projects" },
	{ key: "tasks", label: "Tasks" },
	{ key: "files", label: "Files" },
	{ key: "settings", label: "Settings" },
] as const;

// System's first pages are always shown; the rest sit behind the expander in
// their previous relative order.
const systemPrimary: NavChild[] = [
	{ key: "overview", label: "Overview", to: "/system/overview" },
	{ key: "settings", label: "Settings", to: "/system/settings" },
	{ key: "projects", label: "Projects", to: "/system/projects" },
	{ key: "providers", label: "Providers & Auth", to: "/system/providers" },
	{ key: "usage", label: "Usage", to: "/system/usage" },
	{ key: "jobs", label: "Jobs", to: "/system/jobs" },
];
const systemRest: NavChild[] = [
	{ key: "tools", label: "Tools", to: "/system/tools" },
	{ key: "skills", label: "Skills", to: "/system/skills" },
	{ key: "sessions", label: "Sessions", to: "/system/sessions" },
	{ key: "models", label: "Models", to: "/system/models" },
	{ key: "logs", label: "Logs", to: "/system/logs" },
	{ key: "daemons", label: "Daemons", to: "/system/daemons" },
];

const settingsStore = useSettingsStore();
if (!settingsStore.config && auth.isLoggedIn) void settingsStore.fetchConfig();
const { projects } = useProjectList();

function projectChild(owner: string, p: ProjectSummary): NavChild {
	return {
		key: p.name,
		label: p.name,
		to: projectPath(owner, p.name),
		running: p.running,
	};
}

const routeAgent = computed(() =>
	typeof route.params.agent === "string" ? route.params.agent : "",
);

const agentNames = computed(() =>
	(settingsStore.config?.agents ?? []).map((a) => a.name).filter(Boolean),
);

function ownedProjects(agent: string): ProjectSummary[] {
	return projects.value.filter(
		(p) => projectOwner(settingsStore.config, p.name) === agent,
	);
}

function agentPagePath(agent: string, page: string): string {
	return `/agents/${encodeURIComponent(agent)}/${page}`;
}

// On one agent's page, another agent's link opens the same page for it;
// elsewhere it opens the agent's chat.
const agentPageTail = computed(() => {
	const match = /^\/agents\/[^/]+\/(.+)$/.exec(route.path);
	const tail = match?.[1] ?? "chat";
	if (tail.startsWith("chat")) return "chat";
	if (tail.startsWith("projects")) return "projects";
	return tail;
});

const agentNav = computed<AgentNav[]>(() => {
	const names = [...agentNames.value];
	// An agent added but not saved yet routes as _N until it has a name.
	const pending = routeAgent.value.startsWith("_") ? routeAgent.value : "";
	if (pending) names.push(pending);
	return names.map((name) => {
		const active = routeAgent.value === name;
		const owned = pending === name ? [] : ownedProjects(name);
		return {
			name,
			label: pending === name ? "New agent" : name,
			to: agentPagePath(
				name,
				pending === name ? "settings" : agentPageTail.value,
			),
			active,
			running: owned.reduce((n, p) => n + p.running, 0),
			children:
				active || names.length === 1
				? AGENT_PAGES.map((page) => ({
						key: page.key,
						label: page.label,
						to: agentPagePath(name, page.key),
						count:
							page.key === "projects" && owned.length
								? String(owned.length)
								: undefined,
					}))
				: [],
			projects: owned.map((p) => projectChild(name, p)),
		};
	});
});

// With exactly one agent its pages are the top level, with no Agents → name
// level, and + Add Agent moves below System. A second agent, including one
// being added, brings the full tree back.
const soleAgent = computed<AgentNav | undefined>(() =>
	agentNav.value.length === 1 && !agentNav.value[0].name.startsWith("_")
		? agentNav.value[0]
		: undefined,
);

// The agent's projects list opens on its GitHub Projects pages and boards.
const projectsOpen = computed(() =>
	/^\/agents\/[^/]+\/projects(\/|$)/.test(route.path),
);

const addAgentLink = { path: "/agents", query: { add: "1" } };

// + Add project opens the add dialog on the agent's GitHub Projects board.
function addProjectLink(agent: string) {
	return { path: agentPagePath(agent, "projects"), query: { add: "1" } };
}

// Runtime watches no agent owns sit with System's other pages.
const watchChildren = computed<NavChild[]>(() =>
	projects.value
		.filter((p) => !projectOwner(settingsStore.config, p.name))
		.map((p) => ({ ...projectChild("", p), label: `Watch: ${p.name}` })),
);

const systemMore = computed(() => [...systemRest, ...watchChildren.value]);

const systemActive = computed(() => route.path.startsWith("/system/"));

// The expander opens itself while one of its pages is showing; a click
// overrides that until the next page load.
const systemMoreToggled = systemMoreState;
const systemMoreOpen = computed(
	() =>
		systemMoreToggled.value ??
		systemMore.value.some((child) => isChildActive(child)),
);

function isChildActive(child: NavChild): boolean {
	// Project names contain an encoded "/", so compare decoded paths.
	const path = decodeURIComponent(child.to);
	const current = decodeURIComponent(route.path);
	return current === path || current.startsWith(`${path}/`);
}

function topLevelLinkClass(active: boolean, mobile: boolean): string {
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

function childLinkClass(active: boolean, mobile: boolean): string {
	const base = "mt-1 block rounded-lg px-3 py-2 text-sm transition-colors";
	if (active) {
		return `${base} bg-gray-100 font-semibold text-gray-950 dark:bg-gray-800 dark:text-white`;
	}
	return mobile
		? `${base} text-gray-500 hover:bg-gray-50 hover:text-gray-900 dark:text-gray-400 dark:hover:bg-gray-800 dark:hover:text-white`
		: `${base} text-gray-500 hover:bg-gray-100 hover:text-gray-900 dark:text-gray-400 dark:hover:bg-gray-800 dark:hover:text-white`;
}

// On narrow screens the active agent's pages, or all of System's, form a
// second row under the main one.
const mobileChildren = computed<NavChild[]>(() => {
	const agent = agentNav.value.find((a) => a.active);
	// A sole agent's pages are already the main row.
	if (agent && soleAgent.value) return projectsOpen.value ? agent.projects : [];
	if (agent) return [...agent.children, ...(projectsOpen.value ? agent.projects : [])];
	if (systemActive.value) return [...systemPrimary, ...systemMore.value];
	return [];
});

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

const versionDotClass = computed(() => {
	if (upgradeAvailable.value) return "bg-yellow-400";
	return noCheckout.value ? "bg-gray-400" : "bg-green-500";
});

const displayVersion = computed(() =>
	version.value ? shortVersion(version.value) : status.value === "disconnected" ? "—" : "…",
);

const versionLabel = computed(() => {
	if (upgradeAvailable.value && latestVersion.value) {
		return `Version ${displayVersion.value} -> ${shortVersion(latestVersion.value)}`;
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
