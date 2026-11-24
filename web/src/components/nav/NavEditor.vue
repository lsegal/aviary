<template>
	<div data-testid="nav-editor" class="space-y-5">
		<NavOrderList v-for="g in groups" :key="g.list" :list="g.list" :title="g.title" :items="g.items" />
		<button type="button" data-testid="nav-edit-reset"
			class="rounded-lg border border-gray-200 px-3 py-1.5 text-sm text-gray-600 transition hover:bg-gray-100 hover:text-gray-900 disabled:opacity-40 dark:border-gray-700 dark:text-gray-300 dark:hover:bg-gray-800 dark:hover:text-white"
			:disabled="!customized" @click="nav.reset()">
			Reset sidebar
		</button>
	</div>
</template>

<script setup lang="ts">
// The sidebar editor (#1003), in Settings → Display (#1093): one shared
// editor for the sidebar's agent and System pages; the top row follows the
// agent pages' order (#1015). The layout provides the lists, as it knows
// which pages this session shows.
import { computed, inject } from "vue";
import { useNavPrefs } from "../../composables/useNavPrefs";
import { navEditGroupsKey } from "../../lib/navEditGroups";
import { isCustomized } from "../../lib/navPrefs";
import NavOrderList from "./NavOrderList.vue";

const nav = useNavPrefs();
const provided = inject(navEditGroupsKey, undefined);
const groups = computed(() => provided?.value ?? []);
const customized = computed(() => isCustomized(nav.prefs.value));
</script>
