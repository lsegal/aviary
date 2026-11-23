<template>
	<div v-if="nav.editing.value" class="fixed inset-0 z-50 flex items-center justify-center bg-black/50 px-4 py-6"
		data-testid="nav-edit-dialog" @click.self="close()" @keydown.esc="close()">
		<div ref="panel" role="dialog" aria-modal="true" aria-labelledby="nav-edit-title" tabindex="-1"
			class="flex max-h-[88vh] w-full max-w-md flex-col overflow-hidden rounded-xl border border-gray-200 bg-white shadow-2xl outline-none dark:border-gray-800 dark:bg-gray-900">
			<div class="border-b border-gray-200 px-5 py-4 dark:border-gray-800">
				<h3 id="nav-edit-title" class="text-sm font-semibold text-gray-900 dark:text-white">Edit sidebar</h3>
				<p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
					Drag or move items to reorder them, and hide the ones you don't use. Changes save as you go and follow you to your other devices.
				</p>
			</div>
			<div class="min-h-0 flex-1 space-y-5 overflow-y-auto bg-gray-50 px-5 py-4 dark:bg-gray-950">
				<NavOrderList v-for="g in groups" :key="g.list" :list="g.list" :title="g.title" :items="g.items"
					:hideable="g.hideable" />
			</div>
			<div class="flex items-center gap-2 border-t border-gray-200 px-5 py-3 dark:border-gray-800">
				<button type="button" data-testid="nav-edit-reset"
					class="rounded-lg px-3 py-1.5 text-sm text-gray-600 transition hover:bg-gray-100 hover:text-gray-900 disabled:opacity-40 dark:text-gray-300 dark:hover:bg-gray-800 dark:hover:text-white"
					:disabled="!customized" @click="nav.reset()">
					Reset to default
				</button>
				<button type="button" data-testid="nav-edit-done"
					class="ml-auto rounded-lg bg-blue-600 px-4 py-1.5 text-sm font-semibold text-white transition hover:bg-blue-500"
					@click="close()">
					Done
				</button>
			</div>
		</div>
	</div>
</template>

<script setup lang="ts">
// The Edit sidebar dialog (#1003), opened from the gear menu: one shared
// editor for the sidebar's agent and System pages and the top row.
import { computed, nextTick, ref, watch } from "vue";
import { useNavPrefs } from "../../composables/useNavPrefs";
import { isCustomized, type NavList } from "../../lib/navPrefs";
import NavOrderList, { type NavEditItem } from "./NavOrderList.vue";

export interface NavEditGroup {
	list: NavList;
	title: string;
	items: NavEditItem[];
	hideable?: boolean;
}

defineProps<{ groups: NavEditGroup[] }>();

const nav = useNavPrefs();
const panel = ref<HTMLElement | null>(null);
const customized = computed(() => isCustomized(nav.prefs.value));

watch(nav.editing, async (open) => {
	if (!open) return;
	await nextTick();
	panel.value?.focus();
});

function close() {
	nav.editing.value = false;
}
</script>
