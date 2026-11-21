<template>
	<div class="space-y-0.5 pl-2">
		<!-- A sidebar section's items, the first few then More. -->
		<template v-for="item in shown" :key="itemKey(item)">
			<slot :item="item" />
		</template>
		<button v-if="items.length > limit" type="button" :data-testid="moreTestid" :aria-expanded="showAll"
			class="block w-full rounded-lg px-3 py-2 text-left text-sm text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-900 dark:text-gray-400 dark:hover:bg-gray-800 dark:hover:text-white"
			@click="showAll = !showAll">
			{{ showAll ? "Less" : "More" }}
		</button>
		<p v-if="!items.length && empty" class="px-3 py-2 text-sm text-gray-400 dark:text-gray-500">{{ empty }}</p>
	</div>
</template>

<script setup lang="ts" generic="T">
import { computed, ref } from "vue";

const props = withDefaults(
	defineProps<{
		items: T[];
		itemKey: (item: T) => string;
		moreTestid: string;
		/** How many items show before More. */
		limit?: number;
		/** Shown when there are no items. */
		empty?: string;
	}>(),
	{ limit: 5, empty: "" },
);

defineSlots<{ default(props: { item: T }): unknown }>();

const showAll = ref(false);
const shown = computed(() =>
	showAll.value ? props.items : props.items.slice(0, props.limit),
);
</script>
