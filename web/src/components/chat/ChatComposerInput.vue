<template>
	<textarea ref="textarea" :value="modelValue" rows="1" @input="onInput" @keydown="onKeydown" />
</template>

<script setup lang="ts">
import { nextTick, onMounted, ref, watch } from "vue";
import { caretLine } from "./caretLine";

// The one chat composer input: every chat message box uses it so Enter,
// Shift+Enter, IME handling, and Up/Down history cannot drift apart between
// views.

const props = withDefaults(
	defineProps<{
		modelValue: string;
		/** Sent messages, oldest first, that Up and Down step through. */
		history?: string[];
	}>(),
	{ history: () => [] },
);
const emit = defineEmits<{
	"update:modelValue": [value: string];
	send: [];
}>();

const textarea = ref<HTMLTextAreaElement | null>(null);
// How far back in history the composer is showing; -1 is the draft.
const historyIndex = ref(-1);
const historyDraft = ref("");

/** True while an IME composition owns the key. */
function isComposing(e: KeyboardEvent): boolean {
	return e.isComposing || e.keyCode === 229;
}

function onKeydown(e: KeyboardEvent) {
	if (e.key === "ArrowUp" || e.key === "ArrowDown") {
		onArrow(e);
		return;
	}
	if (e.key !== "Enter" || isComposing(e)) return;
	// Shift+Enter falls through to the textarea's own newline at the caret.
	if (e.shiftKey) return;
	e.preventDefault();
	emit("send");
}

// Up and Down move the caret between lines, as in any textarea, and only
// step through history from the first or last visual line.
function onArrow(e: KeyboardEvent) {
	if (e.shiftKey || e.altKey || e.ctrlKey || e.metaKey) return;
	if (isComposing(e)) return;
	const el = e.target as HTMLTextAreaElement;
	if (el.selectionStart !== el.selectionEnd) return;
	const history = props.history;
	if (history.length === 0) return;

	const line = caretLine(el);
	if (e.key === "ArrowUp") {
		if (!line.first) return;
		e.preventDefault();
		if (historyIndex.value === -1) historyDraft.value = props.modelValue;
		historyIndex.value = Math.min(historyIndex.value + 1, history.length - 1);
		showValue(history[history.length - 1 - historyIndex.value]);
		return;
	}

	if (historyIndex.value === -1 || !line.last) return;
	e.preventDefault();
	historyIndex.value -= 1;
	if (historyIndex.value === -1) {
		showValue(historyDraft.value);
		historyDraft.value = "";
		return;
	}
	showValue(history[history.length - 1 - historyIndex.value]);
}

/** Shows a history entry or the draft with the caret at its end. */
function showValue(value: string) {
	emit("update:modelValue", value);
	void nextTick(() => {
		const el = textarea.value;
		if (!el) return;
		el.setSelectionRange(value.length, value.length);
	});
}

function onInput(e: Event) {
	const value = (e.target as HTMLTextAreaElement).value;
	// Editing a recalled message makes it the new draft.
	historyIndex.value = -1;
	historyDraft.value = "";
	emit("update:modelValue", value);
}

/** Grows the textarea to fit its content; CSS max-height caps it. */
function resize() {
	const el = textarea.value;
	if (!el) return;
	el.style.height = "auto";
	el.style.height = `${el.scrollHeight + el.offsetHeight - el.clientHeight}px`;
}

watch(
	() => props.modelValue,
	() => nextTick(resize),
);
// New or different history, such as after a send or a session switch,
// starts browsing again from the draft.
watch(
	() => props.history,
	(next, prev) => {
		if (next.length === prev.length && next.every((m, i) => m === prev[i]))
			return;
		historyIndex.value = -1;
		historyDraft.value = "";
	},
);
onMounted(resize);

defineExpose({ textarea });
</script>
