<template>
	<router-link v-if="to" :to="to" :aria-label="label" :title="label" :class="classes">
		<PlusIcon aria-hidden="true" :class="iconClass" />
	</router-link>
	<button v-else type="button" :aria-label="label" :title="label" :class="classes"
		@click="emit('click')">
		<PlusIcon aria-hidden="true" :class="iconClass" />
	</button>
</template>

<script setup lang="ts">
// The sidebar's one add/create control: a blue "+" on the right of a row.
// The row carries `group/row relative`; on hover-capable pointers the "+"
// shows only while that row is hovered or focused, and on touch it always
// shows.
//
// `inline` is the variant for a row that lays the "+" out itself, such as
// the narrow-screen second row, which pins it beside its scrolling tabs
// (#1403): it sits in the flow, always shows, and uses the neutral outlined
// icon-button style (#1305).
import PlusIcon from "@heroicons/vue/16/solid/PlusIcon";
import { computed } from "vue";
import type { RouteLocationRaw } from "vue-router";

const props = defineProps<{
	label: string;
	to?: RouteLocationRaw;
	inline?: boolean;
}>();
const emit = defineEmits<{ click: [] }>();

const rowClasses =
	"absolute right-2 top-1/2 inline-flex h-5 w-5 -translate-y-1/2 items-center justify-center rounded-md bg-blue-600 text-white transition hover:bg-blue-500 focus-visible:opacity-100 focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-blue-500 disabled:opacity-50 [@media(hover:hover)]:opacity-0 group-hover/row:opacity-100 group-focus-within/row:opacity-100";
const inlineClasses =
	"inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-lg border border-gray-200 bg-white text-gray-600 transition hover:bg-gray-50 hover:text-gray-900 focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-blue-500 disabled:opacity-50 dark:border-gray-700 dark:bg-gray-900 dark:text-gray-300 dark:hover:bg-gray-800 dark:hover:text-white";

const classes = computed(() => (props.inline ? inlineClasses : rowClasses));
const iconClass = computed(() => (props.inline ? "h-4 w-4" : "h-3.5 w-3.5"));
</script>
