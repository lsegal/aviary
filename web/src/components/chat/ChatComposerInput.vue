<template>
	<textarea ref="textarea" :value="modelValue" rows="1" @input="onInput" @keydown="onKeydown" />
</template>

<script setup lang="ts">
import { nextTick, onMounted, ref, watch } from "vue";

// The one chat composer input: every chat message box uses it so Enter,
// Shift+Enter, and IME handling cannot drift apart between views.

const props = defineProps<{ modelValue: string }>();
const emit = defineEmits<{
	"update:modelValue": [value: string];
	send: [];
}>();

const textarea = ref<HTMLTextAreaElement | null>(null);

/** True while an IME composition owns the Enter key. */
function isComposing(e: KeyboardEvent): boolean {
	return e.isComposing || e.keyCode === 229;
}

function onKeydown(e: KeyboardEvent) {
	if (e.key !== "Enter" || isComposing(e)) return;
	// Shift+Enter falls through to the textarea's own newline at the caret.
	if (e.shiftKey) return;
	e.preventDefault();
	emit("send");
}

function onInput(e: Event) {
	emit("update:modelValue", (e.target as HTMLTextAreaElement).value);
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
onMounted(resize);

defineExpose({ textarea });
</script>
