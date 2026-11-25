<template>
	<div aria-live="polite" class="pointer-events-none fixed bottom-4 right-4 z-[60] flex w-80 max-w-[calc(100vw-2rem)] flex-col gap-2">
		<div v-for="t in toasts" :key="t.id" :data-testid="t.testId ?? 'toast'" :data-tone="t.tone" :role="t.tone === 'error' ? 'alert' : 'status'"
			class="pointer-events-auto flex items-start gap-2 rounded-xl border px-3 py-2 text-sm shadow-lg"
			:class="t.tone === 'error'
				? 'border-red-200 bg-red-50 text-red-800 dark:border-red-900 dark:bg-red-950 dark:text-red-200'
				: 'border-gray-200 bg-white text-gray-800 dark:border-gray-700 dark:bg-gray-900 dark:text-gray-100'">
			<button v-if="t.onClick" type="button" class="min-w-0 flex-1 text-left [overflow-wrap:anywhere]" @click="t.onClick(); dismiss(t.id)">
				<span class="block font-medium">{{ t.title ?? t.message }}</span>
				<span v-if="t.body" class="mt-0.5 line-clamp-3 block whitespace-pre-line text-xs opacity-80">{{ t.body }}</span>
			</button>
			<span v-else-if="t.title" class="min-w-0 flex-1 [overflow-wrap:anywhere]">
				<span class="block font-medium">{{ t.title }}</span>
				<span v-if="t.body" class="mt-0.5 line-clamp-3 block whitespace-pre-line text-xs opacity-80">{{ t.body }}</span>
			</span>
			<span v-else class="min-w-0 flex-1 [overflow-wrap:anywhere]">{{ t.message }}</span>
			<button type="button" aria-label="Dismiss" class="shrink-0 opacity-60 hover:opacity-100" @click="dismiss(t.id)">
				<XMarkIcon aria-hidden="true" class="h-4 w-4" />
			</button>
		</div>
	</div>
</template>

<script setup lang="ts">
import XMarkIcon from "@heroicons/vue/24/outline/XMarkIcon";
import { useToasts } from "../../composables/useToasts";

const { toasts, dismiss } = useToasts();
</script>
