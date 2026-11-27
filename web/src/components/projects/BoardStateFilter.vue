<template>
	<div ref="root" class="relative flex shrink-0 items-center gap-1.5" @keydown.esc="close(true)">
		<template v-if="!collapsed">
			<div id="job-filter" ref="segments" role="group" aria-label="Job filter" :class="SEGMENTS_CLS" data-testid="state-filter-segments">
				<button v-for="state in BOARD_FILTERS" :key="state" type="button" :aria-pressed="filters.includes(state)"
					:class="[SEGMENT_CLS, tone(state)]" @click="emit('toggle', state)">
					{{ stateLabel(state) }}<span v-if="counts[state] !== null" :class="BADGE_CLS">{{ counts[state] }}</span>
				</button>
			</div>
			<button v-if="filters.length" type="button" :class="CLEAR_CLS" @click="emit('clear')">Clear</button>
		</template>

		<template v-else>
			<button ref="toggleButton" type="button" data-testid="filter-toggle" aria-controls="job-filter"
				:aria-expanded="open" aria-haspopup="true"
				class="flex h-9 shrink-0 items-center gap-1.5 rounded-lg border px-2.5"
				:class="filters.length ? 'border-blue-600 text-blue-700 dark:border-blue-400 dark:text-blue-300' : 'border-gray-300 text-gray-600 hover:bg-gray-50 dark:border-gray-600 dark:text-gray-300 dark:hover:bg-gray-800'"
				@click="open = !open">
				<FunnelIcon aria-hidden="true" class="h-4 w-4 shrink-0" />
				<span data-testid="filter-summary">{{ label }}</span>
				<ChevronDownIcon aria-hidden="true" class="h-4 w-4 shrink-0 transition-transform" :class="open && 'rotate-180'" />
			</button>
			<div v-if="open" id="job-filter" role="group" aria-label="Job filter"
				class="absolute top-full left-0 z-20 mt-1 flex w-56 flex-col rounded-xl border border-gray-200 bg-white p-1 shadow-lg dark:border-gray-700 dark:bg-gray-900">
				<!-- Each row toggles like a segment; its box shows whether it is on. -->
				<button v-for="state in BOARD_FILTERS" :key="state" type="button" :aria-pressed="filters.includes(state)"
					class="flex items-center gap-2 rounded-lg px-2 py-1.5 text-left hover:bg-gray-50 dark:hover:bg-gray-800"
					:class="counts[state] === 0 && !filters.includes(state) ? 'text-gray-400 dark:text-gray-500' : 'text-gray-700 dark:text-gray-200'"
					@click="emit('toggle', state)">
					<span aria-hidden="true" class="flex h-4 w-4 shrink-0 items-center justify-center rounded border"
						:class="filters.includes(state) ? 'border-blue-600 bg-blue-600 text-white' : 'border-gray-300 dark:border-gray-600'">
						<CheckIcon v-if="filters.includes(state)" class="h-3 w-3" />
					</span>
					<span class="flex-1">{{ stateLabel(state) }}</span><span v-if="counts[state] !== null" class="tabular-nums text-xs opacity-75">{{ counts[state] }}</span>
				</button>
				<button v-if="filters.length" type="button" class="mt-1 border-t border-gray-100 px-2 pt-1.5 pb-1 text-left text-gray-500 hover:text-gray-800 hover:underline dark:border-gray-800 dark:text-gray-400 dark:hover:text-gray-100"
					@click="emit('clear')">Clear</button>
			</div>
		</template>
	</div>
</template>

<script setup lang="ts">
import CheckIcon from "@heroicons/vue/24/outline/CheckIcon";
import ChevronDownIcon from "@heroicons/vue/24/outline/ChevronDownIcon";
import FunnelIcon from "@heroicons/vue/24/outline/FunnelIcon";
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { filtersLabel, segmentTone } from "../../lib/boardFilterRow";
import {
	BOARD_FILTERS,
	type BoardFilter,
	stateLabel,
} from "../../lib/projects";

// The board's state filters (#723, #767, #1107): one segmented control with
// a single border, or, when the toolbar can't fit it beside a usable
// search, a "Filters" button whose panel lists the same options (#1490).
const props = defineProps<{
	filters: BoardFilter[];
	counts: Record<BoardFilter, number | null>;
	collapsed: boolean;
}>();
const emit = defineEmits<{ toggle: [state: BoardFilter]; clear: [] }>();

const SEGMENTS_CLS =
	"flex h-9 shrink-0 items-stretch divide-x divide-gray-200 overflow-hidden rounded-lg border border-gray-300 text-xs dark:divide-gray-700 dark:border-gray-600";
const SEGMENT_CLS = "flex items-center gap-1 whitespace-nowrap px-2";
const BADGE_CLS =
	"rounded-full bg-black/5 px-1 text-[10px] leading-4 tabular-nums dark:bg-white/10";
const CLEAR_CLS =
	"shrink-0 px-1 text-xs text-gray-500 hover:text-gray-800 hover:underline dark:text-gray-400 dark:hover:text-gray-100";

function tone(state: BoardFilter) {
	return segmentTone(state, props.filters.includes(state), props.counts[state]);
}
const label = computed(() => filtersLabel(props.filters.length));

const root = ref<HTMLElement | null>(null);
const segments = ref<HTMLElement | null>(null);
const toggleButton = ref<HTMLButtonElement | null>(null);
const open = ref(false);
// Room for Clear, reserved whether or not it shows so selecting a state
// never folds the control.
const CLEAR_RESERVE = 44;
/**
 * The full control's width, measured while it shows and remembered while
 * folded, so the toolbar knows when it fits again. No hidden copy measures
 * it: one would reach past a phone's edge.
 */
const naturalWidth = ref(0);

function close(focus = false) {
	if (!open.value) return;
	open.value = false;
	if (focus) toggleButton.value?.focus();
}
function onOutside(e: PointerEvent) {
	if (root.value && !root.value.contains(e.target as Node)) close();
}
watch(
	() => props.collapsed,
	(folded) => {
		if (!folded) open.value = false;
	},
);

function measureSegments() {
	if (segments.value)
		naturalWidth.value = segments.value.offsetWidth + CLEAR_RESERVE;
}
const observer = new ResizeObserver(measureSegments);
watch(segments, (el, old) => {
	if (old) observer.unobserve(old);
	if (el) {
		observer.observe(el);
		measureSegments();
	}
});
onMounted(() => {
	document.addEventListener("pointerdown", onOutside);
	if (segments.value) {
		observer.observe(segments.value);
		measureSegments();
	}
});
onBeforeUnmount(() => {
	document.removeEventListener("pointerdown", onOutside);
	observer.disconnect();
});

defineExpose({ naturalWidth });
</script>
