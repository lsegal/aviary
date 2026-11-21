<template>
	<div data-testid="chat-activity" role="status" aria-live="polite"
		class="inline-flex items-center gap-1 rounded-xl bg-gray-100 px-4 py-3 dark:bg-gray-800">
		<span v-for="i in 3" :key="i" data-testid="chat-activity-dot" aria-hidden="true"
			class="chat-activity-dot h-2 w-2 rounded-full bg-gray-500 dark:bg-gray-400"
			:style="{ animationDelay: `${(i - 1) * 160}ms` }" />
		<span class="sr-only">Agent is thinking</span>
	</div>
</template>

<style scoped>
.chat-activity-dot {
	opacity: 0.35;
	transform: scale(0.8);
	animation: chat-activity-wave 1.2s ease-in-out infinite;
}

@keyframes chat-activity-wave {
	0%,
	60%,
	100% {
		opacity: 0.35;
		transform: scale(0.8);
	}

	30% {
		opacity: 1;
		transform: scale(1.1);
	}
}

@media (prefers-reduced-motion: reduce) {
	.chat-activity-dot {
		animation: none;
		opacity: 0.6;
		transform: none;
	}
}
</style>
