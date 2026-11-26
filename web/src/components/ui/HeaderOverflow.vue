<template>
	<!-- Collapsed, the actions sit behind a "…" button in a popover; otherwise
		they render in place in the header's action row. -->
	<PopoverRoot v-if="collapsed">
		<PopoverTrigger asChild>
			<button type="button" data-testid="header-overflow" :aria-label="label" :title="label" :class="HEADER_ICON_CLS">
				<EllipsisHorizontalIcon aria-hidden="true" class="h-5 w-5" />
			</button>
		</PopoverTrigger>
		<PopoverPortal>
			<PopoverContent align="end" :side-offset="6" :collision-padding="8" data-testid="header-overflow-items"
				class="z-50 flex max-w-[calc(100vw-1rem)] flex-wrap items-center justify-end gap-2 rounded-xl border border-gray-200 bg-white p-2 shadow-lg dark:border-gray-700 dark:bg-gray-900">
				<slot />
			</PopoverContent>
		</PopoverPortal>
	</PopoverRoot>
	<slot v-else />
</template>

<script setup lang="ts">
// Lower-priority page header actions that move behind a "…" button when the
// header row has no room for them, such as the Code Loop board's actions
// while a job is open on a phone (#1347).
import EllipsisHorizontalIcon from "@heroicons/vue/24/outline/EllipsisHorizontalIcon";
import {
	PopoverContent,
	PopoverPortal,
	PopoverRoot,
	PopoverTrigger,
} from "radix-vue";
import { HEADER_ICON_CLS } from "../../lib/headerActions";

withDefaults(defineProps<{ collapsed: boolean; label?: string }>(), {
	label: "More actions",
});
</script>
