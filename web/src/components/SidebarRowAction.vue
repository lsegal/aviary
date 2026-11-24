<template>
	<router-link v-if="to" :to="to" :aria-label="label" :title="label" :class="classes">
		<PlusIcon aria-hidden="true" class="h-3.5 w-3.5" />
	</router-link>
	<button v-else type="button" :aria-label="label" :title="label" :class="classes"
		@click="emit('click')">
		<PlusIcon aria-hidden="true" class="h-3.5 w-3.5" />
	</button>
</template>

<script setup lang="ts">
// The sidebar's one add/create control: a blue "+" on the right of a row.
// The row carries `group/row relative`; on hover-capable pointers the "+"
// shows only while that row is hovered or focused, and on touch it always
// shows. It hides by default and every state that reveals it sets opacity 1,
// so no two rules for one state compete: a production build orders a
// `[@media(hover:hover)]:opacity-0` after `group-hover/row:opacity-100`, which
// kept the "+" hidden even on hover (#1197).
import PlusIcon from "@heroicons/vue/16/solid/PlusIcon";
import type { RouteLocationRaw } from "vue-router";

defineProps<{ label: string; to?: RouteLocationRaw }>();
const emit = defineEmits<{ click: [] }>();

const classes =
	"absolute right-2 top-1/2 inline-flex h-5 w-5 -translate-y-1/2 items-center justify-center rounded-md bg-blue-600 text-white transition hover:bg-blue-500 focus-visible:opacity-100 focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-blue-500 disabled:opacity-50 opacity-0 [@media(hover:none)]:opacity-100 group-hover/row:opacity-100 group-focus-within/row:opacity-100";
</script>
