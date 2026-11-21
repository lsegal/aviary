<script setup lang="ts">
import { ref } from "vue";

const props = withDefaults(
	defineProps<{
		command: string;
		prompt?: string;
	}>(),
	{ prompt: "$" },
);

const copied = ref(false);

async function copy() {
	await navigator.clipboard.writeText(props.command);
	copied.value = true;
	setTimeout(() => {
		copied.value = false;
	}, 1600);
}
</script>

<template>
	<div class="skill-command">
		<code><span v-if="prompt" class="prompt">{{ prompt }}</span>{{ command }}</code>
		<button type="button" class="copy" :aria-label="`Copy ${command}`" @click="copy">
			{{ copied ? "Copied" : "Copy" }}
		</button>
	</div>
</template>
