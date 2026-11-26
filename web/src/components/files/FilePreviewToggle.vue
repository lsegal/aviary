<template>
	<!-- Shows or hides the Preview panel on wide screens; narrow screens use
	     FilePreviewSplit's Edit / Preview tabs instead. Files without a
	     previewer get no toggle. -->
	<button v-if="previewer" type="button" data-testid="file-preview-toggle" :aria-pressed="open"
		:title="open ? 'Hide preview' : 'Show preview'"
		class="hidden h-9 shrink-0 items-center gap-1 rounded-lg border px-2.5 py-1 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 md:inline-flex"
		:class="open
			? 'border-blue-600 bg-blue-50 text-blue-700 dark:border-blue-500 dark:bg-blue-950/50 dark:text-blue-300'
			: 'border-gray-200 text-gray-700 hover:bg-gray-50 dark:border-gray-700 dark:text-gray-200 dark:hover:bg-gray-800'"
		@click="toggle">
		<EyeIcon aria-hidden="true" class="h-4 w-4" />
		Preview
	</button>
</template>

<script setup lang="ts">
import EyeIcon from "@heroicons/vue/24/outline/EyeIcon";
import { computed } from "vue";
import { useFilePreviewLayout } from "../../composables/useFilePreviewLayout";
import { findFilePreviewer } from "./previewers";

const props = defineProps<{ fileName: string }>();
const { open, toggle } = useFilePreviewLayout();
const previewer = computed(() => findFilePreviewer(props.fileName));
</script>
