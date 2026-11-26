<template>
  <div class="flex min-w-0 items-center gap-1">
    <!-- A release build links to its release notes with no visual change:
         same font and color, no underline, only a pointer and a focus ring
         (#1405). Dev builds have no release, so they stay plain text. -->
    <a v-if="releaseURL" data-testid="server-version" :href="releaseURL" target="_blank" rel="noopener noreferrer"
      :title="`Release notes for ${display}`" :aria-label="`Release notes for ${display}`"
      class="truncate rounded-sm font-mono text-xs text-gray-600 no-underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-blue-500 dark:text-gray-400">{{ display }}</a>
    <span v-else data-testid="server-version" :title="`Version ${display}`"
      class="truncate font-mono text-xs text-gray-600 dark:text-gray-400">{{ display }}</span>
    <button v-if="upgradeAvailable" type="button" data-testid="update-icon" :aria-label="updateLabel"
      :title="updateLabel" :disabled="disabled"
      class="inline-flex h-6 w-6 shrink-0 items-center justify-center rounded-md text-yellow-700 transition hover:bg-yellow-100 disabled:opacity-50 dark:text-yellow-300 dark:hover:bg-yellow-900/40"
      @click="emit('upgrade')">
      <ArrowUpCircleIcon aria-hidden="true" class="h-4 w-4" />
    </button>
  </div>
</template>

<script setup lang="ts">
// The running version, short, with an update icon beside it when a newer
// version exists. The update notice text stays in its own box.
import ArrowUpCircleIcon from "@heroicons/vue/24/outline/ArrowUpCircleIcon";
import { computed } from "vue";
import type { ConnectionStatus } from "../composables/useServerStatus";
import { releaseNotesURL, shortVersion } from "../lib/version";

const props = defineProps<{
	version: string;
	latestVersion: string;
	status: ConnectionStatus;
	upgradeAvailable: boolean;
	disabled?: boolean;
}>();
const emit = defineEmits<{ upgrade: [] }>();

const display = computed(() => {
	if (props.version) return shortVersion(props.version);
	return props.status === "disconnected" ? "—" : "…";
});
const releaseURL = computed(() => releaseNotesURL(props.version));
const updateLabel = computed(() =>
	props.latestVersion
		? `Update available: ${shortVersion(props.latestVersion)}`
		: "Update available",
);
</script>
