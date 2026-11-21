<template>
	<div data-testid="chat-activity" role="status" aria-live="polite"
		class="inline-flex items-center gap-2 rounded-xl bg-gray-100 px-4 py-2 text-sm text-gray-600 dark:bg-gray-800 dark:text-gray-300">
		<span class="inline-flex items-center gap-1" aria-hidden="true">
			<span v-for="i in 3" :key="i" data-testid="chat-activity-dot"
				class="chat-activity-dot h-1.5 w-1.5 rounded-full bg-blue-500 dark:bg-blue-400"
				:style="{ animationDelay: `${(i - 1) * 160}ms` }" />
		</span>
		<span data-testid="chat-activity-label">{{ label }}</span>
	</div>
</template>

<script setup lang="ts">
import { computed } from "vue";

const props = defineProps<{
	/** The running tool's name; absent while the agent is thinking. */
	tool?: string;
	/** Seconds the running tool has taken, shown once it is slow. */
	elapsed?: number;
}>();

const label = computed(() => {
	if (!props.tool) return "Thinking…";
	if (props.elapsed === undefined) return `Running ${props.tool}…`;
	return `Running ${props.tool} · ${props.elapsed}s`;
});
</script>

<style scoped>
.chat-activity-dot {
	animation: chat-activity-bounce 1.2s ease-in-out infinite;
}

@keyframes chat-activity-bounce {
	0%,
	60%,
	100% {
		transform: translateY(0);
		opacity: 0.5;
	}

	30% {
		transform: translateY(-4px);
		opacity: 1;
	}
}

@keyframes chat-activity-fade {
	0%,
	100% {
		opacity: 0.35;
	}

	50% {
		opacity: 1;
	}
}

@media (prefers-reduced-motion: reduce) {
	.chat-activity-dot {
		animation-name: chat-activity-fade;
		animation-duration: 2s;
	}
}
</style>
