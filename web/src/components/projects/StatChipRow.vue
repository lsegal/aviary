<template>
	<!-- One line that scrolls sideways instead of wrapping. Its scrollbar is
		hidden (#1209), and a fade on each edge that has more chips past it
		shows the row continues. -->
	<section ref="row" class="flex min-w-0 flex-1 items-center gap-1.5 overflow-x-auto whitespace-nowrap pb-1.5 text-xs scrollbar-none"
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
import { nextTick, ref, watch } from "vue";
import { useScrollFade } from "../../composables/useScrollFade";

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

const row = ref<HTMLElement>();
const { fadeStyle, measure, observe } = useScrollFade(row);

watch(
	() => props.cards,
	async () => {
		await nextTick();
		observe();
	},
	{ deep: true },
);
</script>
