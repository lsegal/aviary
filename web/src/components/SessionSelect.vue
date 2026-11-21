<template>
	<AppCombobox :model-value="selected" :options="options" :aria-label="ariaLabel" data-testid="session-select"
		:placeholder="loading ? 'Loading sessions…' : 'Choose a session…'" empty-text="No matching sessions"
		@update:model-value="emit('update:modelValue', String($event))" />
</template>

<script setup lang="ts">
// A session target picked by name: lists an agent's sessions by display name
// and stores the chosen session's ID. A saved target may also be a session
// name (which the server resolves, creating it on first delivery when needed).
import { computed, watch } from "vue";
import {
	chatDisplayName,
	isMainChat,
	useChatSessionsStore,
} from "../stores/chatSessions";
import { AppCombobox, type SelectOption } from "./ui/select";

const props = defineProps<{
	agent: string;
	modelValue: string;
	ariaLabel?: string;
}>();
const emit = defineEmits<{ "update:modelValue": [value: string] }>();

const store = useChatSessionsStore();

watch(
	() => props.agent,
	(agent) => {
		if (agent) void store.load(agent).catch(() => {});
	},
	{ immediate: true },
);

const loading = computed(() => !!store.loading[props.agent]);
const loaded = computed(() => props.agent in store.byAgent);

const ordered = computed(() =>
	[...store.sessions(props.agent)].sort(
		(a, b) => Number(isMainChat(b)) - Number(isMainChat(a)),
	),
);

/** The listed session the saved target names, by ID or by session name. */
const match = computed(() => {
	const v = props.modelValue.trim();
	if (!v) return undefined;
	return (
		ordered.value.find((s) => s.id === v) ??
		ordered.value.find((s) => s.name === v)
	);
});

const selected = computed(() => match.value?.id ?? props.modelValue);

/** How a saved target that matches no listed session is shown. */
const missingLabel = computed(() => {
	const v = props.modelValue.trim();
	if (!v || match.value) return "";
	if (!loaded.value) return "Loading sessions…";
	if (v === "main") return "Main Chat";
	if (v.startsWith("sess_")) return "Deleted session (no longer exists)";
	return `${v} (created on first delivery)`;
});

const options = computed<SelectOption[]>(() => [
	...ordered.value.map((s) => ({ value: s.id, label: chatDisplayName(s) })),
	...(missingLabel.value
		? [{ value: props.modelValue, label: missingLabel.value }]
		: []),
]);
</script>
