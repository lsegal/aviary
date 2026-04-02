<script setup lang="ts">
import { siDiscord, siSignal } from "simple-icons";

defineProps<{
	name: "signal" | "slack" | "discord";
	disabled?: boolean;
}>();
</script>

<template>
	<span :class="['messaging-logo', name, { disabled }]" :aria-label="name" :title="name">
		<svg v-if="name === 'signal'" viewBox="0 0 24 24" aria-hidden="true">
			<path :d="siSignal.path" fill="currentColor" />
		</svg>

		<svg v-else-if="name === 'slack'" viewBox="0 0 24 24" aria-hidden="true">
			<path
				class="slack-piece slack-blue"
				d="M9.2 2a2.2 2.2 0 0 0-2.2 2.2v5.5a2.2 2.2 0 1 0 4.4 0V4.2A2.2 2.2 0 0 0 9.2 2Z"
			/>
			<path
				class="slack-piece slack-green"
				d="M20 9.2A2.2 2.2 0 0 0 17.8 7h-5.5a2.2 2.2 0 1 0 0 4.4h5.5A2.2 2.2 0 0 0 20 9.2Z"
			/>
			<path
				class="slack-piece slack-yellow"
				d="M14.8 22a2.2 2.2 0 0 0 2.2-2.2v-5.5a2.2 2.2 0 1 0-4.4 0v5.5a2.2 2.2 0 0 0 2.2 2.2Z"
			/>
			<path
				class="slack-piece slack-red"
				d="M4 14.8A2.2 2.2 0 0 0 6.2 17h5.5a2.2 2.2 0 1 0 0-4.4H6.2A2.2 2.2 0 0 0 4 14.8Z"
			/>
			<path
				class="slack-piece slack-blue"
				d="M12.6 4.2A2.2 2.2 0 1 1 17 4.2a2.2 2.2 0 0 1-4.4 0Z"
			/>
			<path
				class="slack-piece slack-green"
				d="M17.8 12.6A2.2 2.2 0 1 1 17.8 17a2.2 2.2 0 0 1 0-4.4Z"
			/>
			<path
				class="slack-piece slack-yellow"
				d="M7 19.8A2.2 2.2 0 1 1 11.4 19.8a2.2 2.2 0 0 1-4.4 0Z"
			/>
			<path
				class="slack-piece slack-red"
				d="M4 7A2.2 2.2 0 1 1 4 11.4 2.2 2.2 0 0 1 4 7Z"
			/>
		</svg>

		<svg v-else viewBox="0 0 24 24" aria-hidden="true">
			<path :d="siDiscord.path" fill="currentColor" />
		</svg>
	</span>
</template>

<style scoped>
.messaging-logo {
	display: inline-flex;
	align-items: center;
	justify-content: center;
}

.messaging-logo svg {
	width: 100%;
	height: 100%;
	display: block;
}

.messaging-logo.disabled {
	filter: grayscale(1);
}

.messaging-logo.disabled.signal,
.messaging-logo.disabled.discord,
.messaging-logo.disabled.slack {
	color: currentColor;
}

.messaging-logo.disabled .slack-piece {
	fill: currentColor;
}

.messaging-logo .slack-blue {
	fill: #36c5f0;
}

.messaging-logo .slack-green {
	fill: #2eb67d;
}

.messaging-logo .slack-yellow {
	fill: #ecb22e;
}

.messaging-logo .slack-red {
	fill: #e01e5a;
}

.messaging-logo.signal {
	color: #3a76f0;
}

.messaging-logo.discord {
	color: #5865f2;
}

.messaging-logo.slack {
	color: #4a154b;
}
</style>
