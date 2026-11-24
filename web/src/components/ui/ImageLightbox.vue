<template>
	<Teleport to="body">
		<div v-if="current" ref="root" role="dialog" aria-modal="true" aria-label="Image preview"
			data-testid="image-preview"
			class="fixed inset-0 z-50 flex items-center justify-center bg-black/80 px-4 py-6" @click="close">
			<button ref="closeButton" type="button"
				class="absolute right-4 top-4 rounded-full bg-black/50 px-3 py-1 text-sm text-white hover:bg-black/70 focus:outline-none focus-visible:ring-2 focus-visible:ring-white"
				aria-label="Close image" @click.stop="close">
				Close
			</button>
			<button v-if="images.length > 1" type="button" aria-label="Previous image"
				class="absolute left-4 top-1/2 flex h-10 w-10 -translate-y-1/2 items-center justify-center rounded-full bg-black/50 text-white hover:bg-black/70 focus:outline-none focus-visible:ring-2 focus-visible:ring-white"
				@click.stop="step(-1)">
				<svg viewBox="0 0 20 20" fill="currentColor" class="h-5 w-5" aria-hidden="true">
					<path fill-rule="evenodd"
						d="M11.78 5.22a.75.75 0 0 1 0 1.06L8.06 10l3.72 3.72a.75.75 0 1 1-1.06 1.06l-4.25-4.25a.75.75 0 0 1 0-1.06l4.25-4.25a.75.75 0 0 1 1.06 0Z"
						clip-rule="evenodd" />
				</svg>
			</button>
			<img :src="current.src" :alt="current.alt" data-testid="image-preview-img"
				class="max-h-full max-w-full rounded-xl shadow-2xl" @click.stop />
			<button v-if="images.length > 1" type="button" aria-label="Next image"
				class="absolute right-4 top-1/2 flex h-10 w-10 -translate-y-1/2 items-center justify-center rounded-full bg-black/50 text-white hover:bg-black/70 focus:outline-none focus-visible:ring-2 focus-visible:ring-white"
				@click.stop="step(1)">
				<svg viewBox="0 0 20 20" fill="currentColor" class="h-5 w-5" aria-hidden="true">
					<path fill-rule="evenodd"
						d="M8.22 5.22a.75.75 0 0 1 1.06 0l4.25 4.25a.75.75 0 0 1 0 1.06l-4.25 4.25a.75.75 0 0 1-1.06-1.06L11.94 10 8.22 6.28a.75.75 0 0 1 0-1.06Z"
						clip-rule="evenodd" />
				</svg>
			</button>
			<p v-if="images.length > 1" data-testid="image-preview-position"
				class="absolute bottom-4 left-1/2 -translate-x-1/2 rounded-full bg-black/50 px-3 py-1 text-sm tabular-nums text-white">
				{{ (index ?? 0) + 1 }} / {{ images.length }}
			</p>
		</div>
	</Teleport>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from "vue";

export interface LightboxImage {
	src: string;
	alt?: string;
}

const props = defineProps<{
	images: LightboxImage[];
	/** The shown image; null keeps the preview closed. */
	index: number | null;
}>();

const emit = defineEmits<{ "update:index": [value: number | null] }>();

const root = ref<HTMLElement | null>(null);
const closeButton = ref<HTMLButtonElement | null>(null);
let opener: HTMLElement | null = null;

const current = computed(() =>
	props.index === null ? undefined : props.images[props.index],
);

function close() {
	emit("update:index", null);
}

function step(delta: number) {
	if (props.index === null || props.images.length < 2) return;
	const n = props.images.length;
	emit("update:index", (props.index + delta + n) % n);
}

function onKeydown(e: KeyboardEvent) {
	if (e.key === "Escape") {
		e.preventDefault();
		e.stopPropagation();
		close();
	} else if (e.key === "ArrowLeft") {
		e.preventDefault();
		step(-1);
	} else if (e.key === "ArrowRight") {
		e.preventDefault();
		step(1);
	} else if (e.key === "Tab" && root.value) {
		// Keep focus on the preview's own buttons while it is open.
		const buttons = Array.from(root.value.querySelectorAll("button"));
		if (!buttons.length) return;
		const at = buttons.indexOf(document.activeElement as HTMLButtonElement);
		const next = e.shiftKey
			? at <= 0 ? buttons.length - 1 : at - 1
			: at === buttons.length - 1 ? 0 : at + 1;
		e.preventDefault();
		buttons[next].focus();
	}
}

watch(
	() => current.value !== undefined,
	async (open) => {
		if (open) {
			opener = document.activeElement as HTMLElement | null;
			window.addEventListener("keydown", onKeydown, true);
			await nextTick();
			closeButton.value?.focus();
		} else {
			window.removeEventListener("keydown", onKeydown, true);
			// An index past the list (the image went away) closes the preview.
			if (props.index !== null) close();
			opener?.focus();
			opener = null;
		}
	},
	{ immediate: true },
);

onBeforeUnmount(() => window.removeEventListener("keydown", onKeydown, true));
</script>
