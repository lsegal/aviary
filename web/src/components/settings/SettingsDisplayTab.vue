<template>
	<section data-testid="settings-display" class="space-y-6 pb-8">
		<div :class="card">
			<h3 :class="heading">Text size</h3>
			<p :class="hint">Scales the whole interface.</p>
			<div role="radiogroup" aria-label="Text size" data-testid="display-text-size" :class="segmented">
				<button v-for="s in TEXT_SIZES" :key="s.value" type="button" role="radio"
					:aria-checked="display.prefs.value.textSize === s.value" :data-value="s.value"
					:class="segment(display.prefs.value.textSize === s.value)" @click="display.set({ textSize: s.value })">
					{{ s.label }}
				</button>
			</div>
			<p data-testid="display-text-preview" class="mt-3 text-sm text-gray-700 dark:text-gray-300">
				The quick brown fox jumps over the lazy dog.
			</p>
		</div>

		<div :class="card">
			<h3 :class="heading">Theme</h3>
			<p :class="hint">System follows your device's light or dark setting.</p>
			<div role="radiogroup" aria-label="Theme" data-testid="display-theme" :class="segmented">
				<button v-for="t in THEMES" :key="t.value" type="button" role="radio"
					:aria-checked="display.prefs.value.theme === t.value" :data-value="t.value"
					:class="segment(display.prefs.value.theme === t.value)" @click="display.set({ theme: t.value })">
					{{ t.label }}
				</button>
			</div>
		</div>

		<div id="sidebar" ref="sidebarSection" :class="card" data-testid="display-sidebar">
			<h3 :class="heading">Sidebar</h3>
			<p :class="hint">
				Drag or move pages to reorder them, and hide the ones you don't use. The top row follows the same order.
			</p>
			<NavEditor />
		</div>

		<div class="flex flex-wrap items-center gap-3">
			<button type="button" data-testid="display-reset"
				class="rounded-lg border border-gray-200 px-3 py-2 text-sm text-gray-700 transition hover:bg-gray-100 disabled:opacity-40 dark:border-gray-700 dark:text-gray-300 dark:hover:bg-gray-800"
				:disabled="!customized" @click="resetAll">
				Reset to defaults
			</button>
			<p class="text-xs text-gray-500 dark:text-gray-400">Changes apply at once and follow you to your other devices.</p>
		</div>
	</section>
</template>

<script setup lang="ts">
// Settings → Display (#1093): the user's own look, as text size, theme and
// the sidebar's pages. Every change applies and saves at once, like the
// sidebar editor always has, rather than through Settings' Save Changes.
// New display options join DisplayPrefs and get a card here.
import { computed, nextTick, ref, watch } from "vue";
import { useRoute } from "vue-router";
import { useDisplayPrefs } from "../../composables/useDisplayPrefs";
import { useNavPrefs } from "../../composables/useNavPrefs";
import { isDefaultDisplay, TEXT_SIZES, THEMES } from "../../lib/displayPrefs";
import { isCustomized } from "../../lib/navPrefs";
import NavEditor from "../nav/NavEditor.vue";

const display = useDisplayPrefs();
const nav = useNavPrefs();
const route = useRoute();
const sidebarSection = ref<HTMLElement | null>(null);

const customized = computed(
	() => !isDefaultDisplay(display.prefs.value) || isCustomized(nav.prefs.value),
);

function resetAll() {
	display.reset();
	nav.reset();
}

// The gear menu's Edit sidebar opens this tab at its Sidebar section.
watch(
	() => route.hash,
	async (hash) => {
		if (hash !== "#sidebar") return;
		await nextTick();
		sidebarSection.value?.scrollIntoView({ block: "start" });
	},
	{ immediate: true },
);

const card =
	"min-w-0 rounded-xl border border-gray-200 bg-white p-5 dark:border-gray-800 dark:bg-gray-900";
const heading =
	"text-sm font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400";
const hint = "mb-3 mt-1 text-xs text-gray-500 dark:text-gray-400";
const segmented =
	"inline-flex max-w-full flex-wrap gap-1 rounded-lg border border-gray-200 bg-gray-50 p-1 dark:border-gray-700 dark:bg-gray-950";

function segment(active: boolean): string {
	const base = "rounded-md px-3 py-1.5 text-sm font-medium transition-colors";
	return active
		? `${base} bg-white text-gray-950 shadow-sm dark:bg-gray-800 dark:text-white`
		: `${base} text-gray-600 hover:text-gray-900 dark:text-gray-400 dark:hover:text-white`;
}
</script>
