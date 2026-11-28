<template>
	<PopoverRoot :open="open" @update:open="onOpenChange">
		<PopoverTrigger asChild>
			<button type="button" :data-testid="testid" :aria-label="inline ? undefined : label"
				:class="inline ? INLINE_TRIGGER_CLS : TRIGGER_CLS"
				:aria-describedby="alert ? alertID : undefined"
				@pointerdown="onTriggerPointerDown" @pointerenter="onHoverStart" @pointerleave="onHoverEnd">
				<slot name="icon" />
				<template v-if="alert">
					<span aria-hidden="true" :data-testid="`${testid}-alert`" :class="ALERT_DOT_CLS" />
					<span :id="alertID" class="sr-only">{{ alert }}</span>
				</template>
			</button>
		</PopoverTrigger>
		<PopoverPortal>
			<PopoverContent side="bottom" align="start" :side-offset="6" :collision-padding="8" :aria-label="label"
				:data-testid="popoverTestid ?? `${testid}-popover`"
				class="z-50 w-max max-w-[min(24rem,calc(100vw-1rem))] rounded-xl border border-gray-200 bg-white p-3 text-sm text-gray-700 shadow-lg dark:border-gray-700 dark:bg-gray-900 dark:text-gray-200"
				@open-auto-focus="onOpenAutoFocus" @close-auto-focus="onCloseAutoFocus"
				@escape-key-down="emit('escapeKeyDown', $event)" @pointerdown="pin"
				@pointerenter="onHoverStart" @pointerleave="onHoverEnd">
				<slot />
			</PopoverContent>
		</PopoverPortal>
	</PopoverRoot>
</template>

<script setup lang="ts">
// A small icon in a page header's chip row that opens details in a popover
// (#1570), such as the Code Loop board's instance ID and its watch details,
// or the same details' inline badge or summary when the row has room for it
// (#1584).
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
import { useId } from "vue";
import { useHoverPopover } from "../../composables/useHoverPopover";
import { ALERT_DOT_CLS } from "../../lib/headerActions";

defineProps<{
	/** A problem the popover's contents report, marked by a dot on the
	 * closed icon and read out as the icon's description (#1580). */
	alert?: string;
	/** The icon's accessible name, which also names the popover. */
	label: string;
	/** The icon's test ID; the popover's is the same with `-popover`. */
	testid: string;
	/** The popover's test ID, when not the icon's with `-popover`. */
	popoverTestid?: string;
	/**
	 * The trigger is the slot's text, such as an inline badge, rather than
	 * an icon, and that text names it (#1584).
	 */
	inline?: boolean;
}>();
const emit = defineEmits<{
	/** Esc inside the popover; preventDefault keeps it open. */
	escapeKeyDown: [event: KeyboardEvent];
}>();

const alertID = useId();

const TRIGGER_CLS =
	"relative inline-flex h-6 w-6 shrink-0 items-center justify-center rounded-full text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-900 focus-visible:outline focus-visible:outline-2 focus-visible:outline-blue-500 data-[state=open]:bg-gray-100 data-[state=open]:text-gray-900 dark:text-gray-400 dark:hover:bg-gray-800 dark:hover:text-white dark:data-[state=open]:bg-gray-800 dark:data-[state=open]:text-white";

// An inline trigger is as wide as its text and as tall as a chip.
const INLINE_TRIGGER_CLS =
	"relative inline-flex min-w-0 shrink-0 items-center whitespace-nowrap rounded-full focus-visible:outline focus-visible:outline-2 focus-visible:outline-blue-500";

const {
	open,
	onOpenChange,
	onTriggerPointerDown,
	pin,
	onHoverStart,
	onHoverEnd,
	onOpenAutoFocus,
	onCloseAutoFocus,
} = useHoverPopover();
</script>
