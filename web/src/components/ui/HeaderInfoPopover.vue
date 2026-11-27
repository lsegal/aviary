<template>
	<PopoverRoot :open="open" @update:open="onOpenChange">
		<PopoverTrigger asChild>
			<button type="button" :data-testid="testid" :aria-label="label" :class="TRIGGER_CLS"
				@pointerdown="onTriggerPointerDown" @pointerenter="onHoverStart" @pointerleave="onHoverEnd">
				<slot name="icon" />
			</button>
		</PopoverTrigger>
		<PopoverPortal>
			<PopoverContent side="bottom" align="start" :side-offset="6" :collision-padding="8" :aria-label="label"
				:data-testid="`${testid}-popover`"
				class="z-50 w-max max-w-[min(24rem,calc(100vw-1rem))] rounded-xl border border-gray-200 bg-white p-3 text-sm text-gray-700 shadow-lg dark:border-gray-700 dark:bg-gray-900 dark:text-gray-200"
				@open-auto-focus="onOpenAutoFocus" @close-auto-focus="onCloseAutoFocus"
				@escape-key-down="emit('escapeKeyDown', $event)" @pointerdown="pinned = true"
				@pointerenter="onHoverStart" @pointerleave="onHoverEnd">
				<slot />
			</PopoverContent>
		</PopoverPortal>
	</PopoverRoot>
</template>

<script setup lang="ts">
// A small icon in a page header's chip row that opens details in a popover
// (#1570), such as the Code Loop board's instance ID and its watch details.
// Clicking or tapping the icon, or Enter or Space on it, opens the popover
// and keeps it open until Esc, an outside tap or another click, returning
// focus to the icon. Hovering with a mouse shows the same popover as a
// tooltip, closing when the pointer leaves it, unless it was clicked.
import {
	PopoverContent,
	PopoverPortal,
	PopoverRoot,
	PopoverTrigger,
} from "radix-vue";
import { onBeforeUnmount, ref } from "vue";

defineProps<{
	/** The icon's accessible name, which also names the popover. */
	label: string;
	/** The icon's test ID; the popover's is the same with `-popover`. */
	testid: string;
}>();
const emit = defineEmits<{
	/** Esc inside the popover; preventDefault keeps it open. */
	escapeKeyDown: [event: KeyboardEvent];
}>();

const TRIGGER_CLS =
	"inline-flex h-6 w-6 shrink-0 items-center justify-center rounded-full text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-900 focus-visible:outline focus-visible:outline-2 focus-visible:outline-blue-500 data-[state=open]:bg-gray-100 data-[state=open]:text-gray-900 dark:text-gray-400 dark:hover:bg-gray-800 dark:hover:text-white dark:data-[state=open]:bg-gray-800 dark:data-[state=open]:text-white";
// The pointer can cross the gap between the icon and the popover.
const HOVER_CLOSE_MS = 150;

const open = ref(false);
// Opened by a click, tap or key rather than a passing mouse.
const pinned = ref(false);
const hovering = ref(false);
// A click on the icon while its tooltip shows pins the tooltip open.
let pinOnToggle = false;
let closeTimer: ReturnType<typeof setTimeout> | undefined;

function clearCloseTimer() {
	if (closeTimer !== undefined) clearTimeout(closeTimer);
	closeTimer = undefined;
}

function onOpenChange(next: boolean) {
	clearCloseTimer();
	if (!next && pinOnToggle) {
		pinOnToggle = false;
		pinned.value = true;
		return;
	}
	pinOnToggle = false;
	open.value = next;
	pinned.value = next;
	hovering.value = false;
}

function onTriggerPointerDown() {
	pinOnToggle = open.value && !pinned.value;
}

function onHoverStart(e: PointerEvent) {
	if (e.pointerType !== "mouse") return;
	clearCloseTimer();
	if (open.value) return;
	hovering.value = true;
	open.value = true;
}

function onHoverEnd(e: PointerEvent) {
	if (e.pointerType !== "mouse" || pinned.value || !open.value) return;
	clearCloseTimer();
	closeTimer = setTimeout(() => {
		closeTimer = undefined;
		if (pinned.value) return;
		open.value = false;
	}, HOVER_CLOSE_MS);
}

// A tooltip shown on hover leaves focus where it was.
function onOpenAutoFocus(e: Event) {
	if (hovering.value) e.preventDefault();
}

function onCloseAutoFocus(e: Event) {
	if (hovering.value) e.preventDefault();
	hovering.value = false;
}

onBeforeUnmount(clearCloseTimer);
</script>
