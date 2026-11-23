<template>
	<!-- One line that scrolls sideways instead of wrapping. The bottom padding
		keeps the scrollbar off the chips' borders, and a fade on each edge that
		has more chips past it shows the row continues. -->
	<section ref="row" class="flex min-w-0 flex-1 items-center gap-1.5 overflow-x-auto whitespace-nowrap pb-1.5 text-xs [scrollbar-width:thin]"
		:style="fadeStyle" data-testid="stat-row" @scroll.passive="measure">
		<component :is="s.filter ? 'button' : 'span'" v-for="s in cards" :key="s.label" :type="s.filter ? 'button' : undefined"
			class="inline-flex shrink-0 items-baseline gap-1 rounded-full border px-2.5 py-0.5"
			:class="[s.cls, s.filter ? 'hover:brightness-95' : '', s.active ? 'ring-2 ring-inset ring-blue-500' : '']"
			:aria-pressed="s.filter ? Boolean(s.active) : undefined"
			:title="s.suffix ? `${s.label}: ${s.value} ${s.suffix}` : `${s.label}: ${s.value}`" data-testid="stat-chip"
			:data-filter="s.filter" @click="s.filter && emit('select', s.filter)">
			<b class="tabular-nums">{{ s.chip }}</b><span class="opacity-80">{{ s.chipLabel }}</span>
		</component>
	</section>
</template>

<script setup lang="ts">
import {
	computed,
	nextTick,
	onBeforeUnmount,
	onMounted,
	ref,
	watch,
} from "vue";

export interface StatChip {
	label: string;
	value: number | string;
	suffix?: string;
	chip: string;
	chipLabel: string;
	cls: string;
	/** Makes the chip a button that selects this filter. */
	filter?: string;
	/** Marks the chip's filter as the one applied. */
	active?: boolean;
}

const props = defineProps<{ cards: StatChip[] }>();
const emit = defineEmits<{ select: [filter: string] }>();

const FADE = "1.5rem";
const row = ref<HTMLElement>();
const before = ref(false);
const after = ref(false);

function measure() {
	const el = row.value;
	if (!el) return;
	before.value = el.scrollLeft > 1;
	after.value = el.scrollLeft + el.clientWidth < el.scrollWidth - 1;
}

const fadeStyle = computed(() => {
	if (!before.value && !after.value) return undefined;
	const start = before.value ? `transparent, #000 ${FADE}` : "#000";
	const end = after.value ? `#000 calc(100% - ${FADE}), transparent` : "#000";
	const mask = `linear-gradient(to right, ${start}, ${end})`;
	return { maskImage: mask, WebkitMaskImage: mask };
});

let observer: ResizeObserver | undefined;
onMounted(() => {
	observer = new ResizeObserver(measure);
	if (row.value) {
		observer.observe(row.value);
		for (const chip of row.value.children) observer.observe(chip);
	}
	measure();
});
onBeforeUnmount(() => observer?.disconnect());
watch(
	() => props.cards,
	async () => {
		await nextTick();
		measure();
	},
	{ deep: true },
);
</script>
