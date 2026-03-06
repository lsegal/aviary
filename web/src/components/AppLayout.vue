<template>
  <div class="flex h-dvh bg-white dark:bg-gray-950">
    <!-- Sidebar -->
    <nav class="flex w-52 flex-col border-r border-gray-200 bg-gray-50 p-4 dark:border-gray-800 dark:bg-gray-900">
      <div class="mb-6 flex items-center gap-2">
        <img :src="logoUrl" alt="Aviary logo" class="h-8 w-8 object-contain" />
        <span class="text-lg font-bold text-gray-900 dark:text-white">Aviary</span>
      </div>
      <router-link v-for="link in links" :key="link.to" :to="link.to"
        class="mb-1 rounded-lg px-3 py-2 text-sm font-medium text-gray-600 hover:bg-gray-200 hover:text-gray-900 dark:text-gray-400 dark:hover:bg-gray-800 dark:hover:text-white"
        active-class="bg-gray-200 text-gray-900 dark:bg-gray-800 dark:text-white">
        {{ link.label }}
      </router-link>
      <div class="mt-auto flex flex-col gap-2">
        <!-- Server status pills -->
        <div class="flex flex-wrap gap-1.5 px-1">
          <span
            class="flex items-center gap-1.5 rounded-full border border-gray-200 bg-white px-2.5 py-0.5 text-xs text-gray-600 dark:border-gray-700 dark:bg-gray-800 dark:text-gray-400">
            <span class="h-1.5 w-1.5 shrink-0 rounded-full" :class="versionDotClass" />
            Version {{ displayVersion }}
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

    <!-- Main -->
    <main class="flex flex-1 flex-col overflow-hidden">
      <slot />
    </main>
  </div>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { useServerStatus } from "../composables/useServerStatus";
import { useAuthStore } from "../stores/auth";

const _auth = useAuthStore();
const { status, version } = useServerStatus();
const _logoUrl = "/logo.png";

const _links = [
	{ to: "/overview", label: "Overview" },
	{ to: "/chat", label: "Chat" },
	{ to: "/settings", label: "Settings" },
	{ to: "/logs", label: "Logs" },
	{ to: "/usage", label: "Usage" },
	{ to: "/jobs", label: "Jobs" },
];

const _dotClass = computed(() => {
	if (status.value === "connected") return "bg-green-500";
	if (status.value === "disconnected") return "bg-red-500";
	return "bg-yellow-400 animate-pulse";
});

// Version dot is always green — out-of-date detection not yet implemented.
const _versionDotClass = "bg-green-500";

const _displayVersion = computed(() =>
	version.value ? version.value : status.value === "disconnected" ? "—" : "…",
);

const _healthLabel = computed(() => {
	if (status.value === "connected") return "Connected";
	if (status.value === "disconnected") return "Disconnected";
	return "…";
});

const _healthTextClass = computed(() => {
	if (status.value === "connected") return "text-green-600 dark:text-green-400";
	if (status.value === "disconnected") return "text-red-500 dark:text-red-400";
	return "text-yellow-600 dark:text-yellow-400";
});
</script>
