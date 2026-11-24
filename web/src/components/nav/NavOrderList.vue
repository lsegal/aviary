<template>
	<section :data-testid="`nav-edit-${list}`">
		<h4 class="mb-1.5 text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">{{ title }}</h4>
		<SortableList ref="sortable" :items="rows" :row-attrs="rowAttrs" @reorder="(keys) => nav.setOrder(list, keys)">
			<template #default="{ item, index, handle }">
				<button type="button" v-bind="handle" :aria-label="`Drag ${item.label}`"
					title="Drag to reorder, or press the up and down arrow keys" data-testid="nav-edit-handle"
					class="inline-flex h-7 w-6 shrink-0 cursor-grab items-center justify-center rounded text-gray-400 hover:text-gray-700 active:cursor-grabbing dark:hover:text-gray-200">
					<Bars3Icon aria-hidden="true" class="h-4 w-4" />
				</button>
				<span class="min-w-0 flex-1 truncate" :class="hidden(item.key) ? 'line-through' : 'text-gray-900 dark:text-white'">{{ item.label }}</span>
				<button type="button" :aria-label="`Move ${item.label} up`" title="Move up" data-testid="nav-edit-up"
					:disabled="index === 0" :class="iconButton" @click="move(item.key, -1)">
					<ChevronUpIcon aria-hidden="true" class="h-4 w-4" />
				</button>
				<button type="button" :aria-label="`Move ${item.label} down`" title="Move down" data-testid="nav-edit-down"
					:disabled="index === rows.length - 1" :class="iconButton" @click="move(item.key, 1)">
					<ChevronDownIcon aria-hidden="true" class="h-4 w-4" />
				</button>
				<button type="button" :aria-pressed="!hidden(item.key)"
					:aria-label="hidden(item.key) ? `Show ${item.label}` : `Hide ${item.label}`"
					:title="hidden(item.key) ? 'Show' : 'Hide'" data-testid="nav-edit-visibility" :class="iconButton"
					@click="nav.setHidden(list, item.key, !hidden(item.key))">
					<EyeSlashIcon v-if="hidden(item.key)" aria-hidden="true" class="h-4 w-4" />
					<EyeIcon v-else aria-hidden="true" class="h-4 w-4" />
				</button>
			</template>
		</SortableList>
	</section>
</template>

<script setup lang="ts">
// One arrangeable navigation list in the Edit sidebar dialog (#1003): drag
// a row's handle, or use its move up/down buttons or arrow keys, to
// reorder, and its eye to hide or show it. Hidden rows stay listed so they
// can be shown again.
import Bars3Icon from "@heroicons/vue/24/outline/Bars3Icon";
import ChevronDownIcon from "@heroicons/vue/24/outline/ChevronDownIcon";
import ChevronUpIcon from "@heroicons/vue/24/outline/ChevronUpIcon";
import EyeIcon from "@heroicons/vue/24/outline/EyeIcon";
import EyeSlashIcon from "@heroicons/vue/24/outline/EyeSlashIcon";
import { computed, nextTick, ref } from "vue";
import { useNavPrefs } from "../../composables/useNavPrefs";
import { arrangeNav, moveIndex, type NavList } from "../../lib/navPrefs";
import SortableList from "../SortableList.vue";

export interface NavEditItem {
	key: string;
	label: string;
}

const props = defineProps<{
	list: NavList;
	title: string;
	items: NavEditItem[];
}>();

const nav = useNavPrefs();
const sortable = ref<{ $el: HTMLElement } | null>(null);

const rows = computed(() =>
	arrangeNav(props.items, props.list, nav.prefs.value, { showHidden: true }),
);

function hidden(key: string): boolean {
	return nav.isHidden(props.list, key);
}

function rowAttrs(item: NavEditItem): Record<string, unknown> {
	return {
		"data-testid": "nav-edit-row",
		"data-hidden": hidden(item.key) || undefined,
		class: [
			"flex items-center gap-1 rounded-lg border border-gray-200 bg-white px-1.5 py-1 text-sm dark:border-gray-700 dark:bg-gray-800",
			hidden(item.key) ? "opacity-60" : "",
		],
	};
}

async function move(key: string, delta: number) {
	const keys = rows.value.map((r) => r.key);
	const from = keys.indexOf(key);
	nav.setOrder(props.list, moveIndex(keys, from, from + delta));
	// Keep focus on the moved row's same button, so arrows can repeat.
	await nextTick();
	const button = delta < 0 ? "up" : "down";
	sortable.value?.$el
		?.querySelector<HTMLButtonElement>(
			`[data-key="${CSS.escape(key)}"] [data-testid="nav-edit-${button}"]`,
		)
		?.focus();
}

const iconButton =
	"inline-flex h-7 w-7 shrink-0 items-center justify-center rounded-md text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-900 disabled:pointer-events-none disabled:opacity-30 dark:text-gray-400 dark:hover:bg-gray-700 dark:hover:text-white";
</script>
