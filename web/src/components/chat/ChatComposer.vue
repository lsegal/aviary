<template>
	<form data-testid="chat-composer"
		class="flex flex-col gap-2 rounded-2xl border border-gray-300 bg-white p-2 transition-colors focus-within:border-blue-500 focus-within:ring-2 focus-within:ring-blue-500/30 dark:border-gray-700 dark:bg-gray-900 dark:focus-within:border-blue-400 dark:focus-within:ring-blue-400/30"
		:class="[disabled ? 'opacity-50' : '', dragging ? 'border-blue-500 dark:border-blue-400' : '']"
		@submit.prevent="submit" @dragover.prevent="onDragOver" @dragleave="dragging = false" @drop.prevent="onDrop">
		<ul v-if="attachments.length" aria-label="Attachments" class="flex flex-wrap gap-2 px-1 pt-1">
			<li v-for="att in attachments" :key="att.id" class="relative" :data-testid="`attachment-${att.kind}`">
				<img v-if="att.kind === 'image'" :src="att.dataURL" :alt="att.name"
					class="h-16 w-16 rounded-lg border border-gray-200 object-cover dark:border-gray-700" />
				<span v-else
					class="inline-flex h-16 max-w-48 items-center gap-2 rounded-lg border border-gray-200 bg-gray-50 px-3 text-sm text-gray-700 dark:border-gray-700 dark:bg-gray-800 dark:text-gray-200">
					<svg viewBox="0 0 20 20" fill="currentColor" class="h-5 w-5 shrink-0 text-gray-400" aria-hidden="true">
						<path fill-rule="evenodd"
							d="M4.5 2A1.5 1.5 0 0 0 3 3.5v13A1.5 1.5 0 0 0 4.5 18h11a1.5 1.5 0 0 0 1.5-1.5V7.621a1.5 1.5 0 0 0-.44-1.06l-4.12-4.122A1.5 1.5 0 0 0 11.378 2H4.5Zm2.25 8.5a.75.75 0 0 0 0 1.5h6.5a.75.75 0 0 0 0-1.5h-6.5Zm0 3a.75.75 0 0 0 0 1.5h6.5a.75.75 0 0 0 0-1.5h-6.5Z"
							clip-rule="evenodd" />
					</svg>
					<span class="truncate">{{ att.name }}</span>
				</span>
				<button type="button" :aria-label="`Remove ${att.name}`" :title="`Remove ${att.name}`"
					class="absolute -right-1.5 -top-1.5 flex h-5 w-5 items-center justify-center rounded-full bg-gray-700 text-xs text-white hover:bg-gray-600 dark:bg-gray-600 dark:hover:bg-gray-500"
					@click="remove(att.id)">✕</button>
			</li>
		</ul>
		<p v-if="notice" role="alert" class="px-2 text-xs text-red-600 dark:text-red-400">{{ notice }}</p>
		<div class="flex items-end gap-2">
			<button type="button" :disabled="disabled" aria-label="Attach files" title="Attach files"
				class="inline-flex h-9 w-9 shrink-0 items-center justify-center rounded-full text-gray-500 hover:bg-gray-100 hover:text-gray-900 disabled:pointer-events-none dark:text-gray-400 dark:hover:bg-gray-800 dark:hover:text-white"
				@click="fileInput?.click()">
				<svg viewBox="0 0 20 20" fill="currentColor" class="h-5 w-5" aria-hidden="true">
					<path
						d="M10.75 4.75a.75.75 0 0 0-1.5 0v4.5h-4.5a.75.75 0 0 0 0 1.5h4.5v4.5a.75.75 0 0 0 1.5 0v-4.5h4.5a.75.75 0 0 0 0-1.5h-4.5v-4.5Z" />
				</svg>
			</button>
			<input ref="fileInput" type="file" multiple class="hidden" data-testid="chat-file-input"
				@change="onFilesPicked" />
			<ChatComposerInput ref="inputRef" :model-value="modelValue" :history="history" :disabled="disabled" :placeholder="placeholder"
				aria-label="Message"
				class="max-h-48 min-h-9 flex-1 resize-none overflow-y-auto bg-transparent px-1 py-2 text-sm leading-5 text-gray-900 placeholder-gray-400 focus:outline-none dark:text-white dark:placeholder-gray-500"
				@update:model-value="emit('update:modelValue', $event)" @send="submit"
				@paste="onPaste" />
			<button v-if="showStop" type="button" aria-label="Stop response" title="Stop"
				class="inline-flex h-9 w-9 shrink-0 items-center justify-center rounded-full bg-red-600 text-white hover:bg-red-500 disabled:opacity-40"
				:disabled="disabled" @click="emit('stop')">
				<svg viewBox="0 0 20 20" fill="currentColor" class="h-4 w-4" aria-hidden="true">
					<rect x="5" y="5" width="10" height="10" rx="1.5" />
				</svg>
			</button>
			<button v-else type="submit" :disabled="!canSend" aria-label="Send message" title="Send"
				class="inline-flex h-9 w-9 shrink-0 items-center justify-center rounded-full bg-blue-600 text-white hover:bg-blue-500 disabled:opacity-40">
				<svg viewBox="0 0 20 20" fill="currentColor" class="h-5 w-5" aria-hidden="true">
					<path fill-rule="evenodd"
						d="M10 17a.75.75 0 0 1-.75-.75V5.612L5.29 9.77a.75.75 0 0 1-1.08-1.04l5.25-5.5a.75.75 0 0 1 1.08 0l5.25 5.5a.75.75 0 1 1-1.08 1.04l-3.96-4.158V16.25A.75.75 0 0 1 10 17Z"
						clip-rule="evenodd" />
				</svg>
			</button>
		</div>
	</form>
</template>

<script setup lang="ts">
import { computed, ref } from "vue";
import { type ChatAttachment, readAttachment } from "./attachments";
import ChatComposerInput from "./ChatComposerInput.vue";

const props = withDefaults(
	defineProps<{
		modelValue: string;
		attachments: ChatAttachment[];
		disabled?: boolean;
		/** The agent is replying; an empty composer offers Stop instead of Send. */
		running?: boolean;
		placeholder?: string;
		/** Sent messages, oldest first, that Up and Down recall. */
		history?: string[];
	}>(),
	{ placeholder: "Describe a task or ask a question" },
);

const emit = defineEmits<{
	"update:modelValue": [value: string];
	"update:attachments": [value: ChatAttachment[]];
	submit: [];
	stop: [];
}>();

const inputRef = ref<{ textarea: HTMLTextAreaElement | null } | null>(null);
const textarea = computed(() => inputRef.value?.textarea ?? null);
const fileInput = ref<HTMLInputElement | null>(null);
const notice = ref("");
const dragging = ref(false);

const hasContent = computed(
	() => props.modelValue.trim() !== "" || props.attachments.length > 0,
);
// Typing during a reply turns Stop back into Send, so a follow-up can go out
// while the agent is still replying.
const showStop = computed(() => props.running && !hasContent.value);
const canSend = computed(() => hasContent.value && !props.disabled);

function submit() {
	if (!canSend.value) return;
	notice.value = "";
	emit("submit");
}

async function addFiles(files: Iterable<File>) {
	const added: ChatAttachment[] = [];
	const errors: string[] = [];
	for (const file of files) {
		const result = await readAttachment(file);
		if ("error" in result) errors.push(result.error);
		else added.push(result.attachment);
	}
	notice.value = errors.join(" ");
	if (added.length)
		emit("update:attachments", [...props.attachments, ...added]);
}

function remove(id: string) {
	emit(
		"update:attachments",
		props.attachments.filter((a) => a.id !== id),
	);
	textarea.value?.focus();
}

async function onFilesPicked(e: Event) {
	const el = e.target as HTMLInputElement;
	const files = Array.from(el.files ?? []);
	el.value = "";
	await addFiles(files);
	textarea.value?.focus();
}

function onPaste(e: ClipboardEvent) {
	const files = Array.from(e.clipboardData?.files ?? []);
	if (!files.length) return;
	e.preventDefault();
	void addFiles(files);
}

function onDragOver(e: DragEvent) {
	if (props.disabled || !e.dataTransfer?.types.includes("Files")) return;
	dragging.value = true;
}

function onDrop(e: DragEvent) {
	dragging.value = false;
	if (props.disabled) return;
	void addFiles(Array.from(e.dataTransfer?.files ?? []));
}

defineExpose({ textarea });
</script>
