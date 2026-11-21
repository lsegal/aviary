<template>
	<!-- Same renderer and prose styles as chat output; raw HTML is escaped. -->
	<div data-testid="markdown-preview" class="prose dark:prose-invert max-w-none" v-html="html" />
</template>

<script setup lang="ts">
import { computed } from "vue";
import { renderMarkdown } from "../../lib/markdown";

const props = defineProps<{ content: string }>();
const html = computed(() => renderMarkdown(props.content));
</script>
