<script setup lang="ts">
import type { HTMLAttributes } from "vue";

const props = defineProps<{
	class?: HTMLAttributes["class"];
}>();
</script>

<template>
	<div
		:class="[
			'animate-pulse rounded-md bg-gray-200 dark:bg-gray-800',
			props.class,
		]"
	/>
</template>
