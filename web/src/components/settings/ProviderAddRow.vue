<template>
	<div ref="root" data-testid="provider-add" class="border-t border-gray-200 first:border-t-0 dark:border-gray-700">
		<button v-if="!open" ref="toggle" type="button" data-testid="provider-add-row" aria-expanded="false"
			aria-controls="provider-add-kinds"
			class="group m-2 flex w-[calc(100%-1rem)] items-center gap-2 rounded-lg border-2 border-dashed border-gray-300 px-3 py-2.5 text-left text-sm font-semibold text-blue-600 transition hover:border-blue-300 hover:bg-blue-50/60 focus:outline-none focus-visible:border-blue-400 focus-visible:bg-blue-50/60 dark:border-gray-600 dark:text-blue-400 dark:hover:border-blue-700 dark:hover:bg-blue-950/30 dark:focus-visible:bg-blue-950/30"
			@click="expand">
			<span
				class="flex h-6 w-6 items-center justify-center rounded-full border-2 border-dashed border-blue-400 text-base leading-none transition group-hover:rotate-90 group-hover:border-solid motion-reduce:transition-none motion-reduce:group-hover:rotate-0 dark:border-blue-500"
				aria-hidden="true">+</span>
			Add provider
		</button>
		<div v-else id="provider-add-kinds" class="provider-add-panel bg-gray-50/70 p-3 dark:bg-gray-800/40"
			@keydown="onKeydown">
			<div class="mb-2 flex items-center justify-between">
				<span class="text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">Add a
					provider</span>
				<button type="button" data-testid="provider-add-close"
					class="rounded px-1.5 text-xs text-gray-400 hover:text-gray-600 dark:text-gray-500 dark:hover:text-gray-300"
					@click="collapse(true)">Cancel</button>
			</div>
			<div ref="grid" role="group" aria-label="Provider kinds" class="grid grid-cols-2 gap-2 sm:grid-cols-3">
				<button v-for="(tile, index) in tiles" :key="tile.key" type="button" :data-kind="tile.key"
					class="provider-add-tile flex flex-col items-start rounded-lg border border-gray-200 bg-white px-3 py-2 text-left transition hover:-translate-y-0.5 hover:border-blue-400 hover:shadow-sm focus:outline-none focus-visible:border-blue-500 focus-visible:ring-2 focus-visible:ring-blue-500/30 motion-reduce:hover:translate-y-0 dark:border-gray-700 dark:bg-gray-900 dark:hover:border-blue-500"
					:style="{ '--i': index }" @click="choose(tile.key)">
					<span class="text-sm font-medium text-gray-800 dark:text-gray-200">{{ tile.name }}</span>
					<span v-if="tile.kind" class="text-[11px] text-gray-500 dark:text-gray-400">{{ tile.kind }}</span>
				</button>
			</div>
		</div>
	</div>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from "vue";

const props = defineProps<{ options: Array<{ key: string; label: string }> }>();
const emit = defineEmits<(event: "select", key: string) => void>();

const root = ref<HTMLElement | null>(null);
const toggle = ref<HTMLButtonElement | null>(null);
const grid = ref<HTMLElement | null>(null);
const open = ref(false);

// Option labels read "Anthropic (API Key)"; tiles show the kind as a caption.
const tiles = computed(() =>
	props.options.map((opt) => {
		const match = /^(.*) \(([^)]+)\)$/.exec(opt.label);
		return {
			key: opt.key,
			name: match ? match[1] : opt.label,
			kind: match ? match[2] : "",
		};
	}),
);

function tileButtons(): HTMLButtonElement[] {
	return Array.from(grid.value?.querySelectorAll("button") ?? []);
}

async function expand() {
	open.value = true;
	await nextTick();
	tileButtons()[0]?.focus();
}

async function collapse(restoreFocus: boolean) {
	open.value = false;
	if (!restoreFocus) return;
	await nextTick();
	toggle.value?.focus();
}

function choose(key: string) {
	emit("select", key);
	void collapse(false);
}

function gridColumns(): number {
	if (!grid.value) return 1;
	return getComputedStyle(grid.value).gridTemplateColumns.split(" ").length;
}

function onKeydown(event: KeyboardEvent) {
	if (event.key === "Escape") {
		event.preventDefault();
		void collapse(true);
		return;
	}
	const buttons = tileButtons();
	const current = buttons.indexOf(document.activeElement as HTMLButtonElement);
	if (current < 0) return;
	const step: Record<string, number> = {
		ArrowLeft: -1,
		ArrowRight: 1,
		ArrowUp: -gridColumns(),
		ArrowDown: gridColumns(),
	};
	let next: number;
	if (event.key in step) {
		next = Math.min(buttons.length - 1, Math.max(0, current + step[event.key]));
	} else if (event.key === "Home") {
		next = 0;
	} else if (event.key === "End") {
		next = buttons.length - 1;
	} else {
		return;
	}
	event.preventDefault();
	buttons[next]?.focus();
}

function onPointerDown(event: PointerEvent) {
	if (open.value && !root.value?.contains(event.target as Node)) {
		void collapse(false);
	}
}

onMounted(() => document.addEventListener("pointerdown", onPointerDown));
onBeforeUnmount(() =>
	document.removeEventListener("pointerdown", onPointerDown),
);
</script>

<style scoped>
.provider-add-panel {
  animation: provider-add-open 200ms ease-out;
}

.provider-add-tile {
  animation: provider-add-tile-in 220ms ease-out both;
  animation-delay: calc(var(--i) * 30ms);
}

@keyframes provider-add-open {
  from {
    opacity: 0;
    clip-path: inset(0 0 100% 0);
  }
  to {
    opacity: 1;
    clip-path: inset(0 0 0 0);
  }
}

@keyframes provider-add-tile-in {
  from {
    opacity: 0;
    transform: translateY(6px) scale(0.97);
  }
  to {
    opacity: 1;
    transform: none;
  }
}

@media (prefers-reduced-motion: reduce) {
  .provider-add-panel,
  .provider-add-tile {
    animation: none;
  }
}
</style>
