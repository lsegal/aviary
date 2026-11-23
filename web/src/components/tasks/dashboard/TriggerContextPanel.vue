<template>
	<section aria-label="Trigger context" data-testid="trigger-context"
		class="rounded-xl border border-gray-200 dark:border-gray-800">
		<header class="flex flex-wrap items-center gap-x-3 gap-y-1 border-b border-gray-200 px-3 py-2 text-xs dark:border-gray-800">
			<h3 class="text-sm font-semibold text-gray-900 dark:text-white">Trigger context</h3>
			<span class="text-gray-500 dark:text-gray-400" data-testid="trigger-context-where">
				{{ context.channel }}<template v-if="context.entry"> · {{ context.entry }}</template>
			</span>
			<span class="text-gray-500 dark:text-gray-400">received {{ when(context.message.timestamp) }}</span>
		</header>
		<ol class="space-y-1 px-3 py-2 text-sm">
			<li v-for="(m, i) in context.before ?? []" :key="`b${i}`" data-testid="trigger-context-before"
				class="text-gray-500 dark:text-gray-400">
				<TriggerLine :message="m" />
			</li>
			<li data-testid="trigger-context-message"
				class="rounded-lg border-l-4 border-amber-400 bg-amber-50 px-2 py-1 text-gray-900 dark:bg-amber-950/30 dark:text-gray-100">
				<TriggerLine :message="context.message" />
				<ul v-if="context.message.attachments?.length" class="mt-1 text-xs text-gray-600 dark:text-gray-400">
					<li v-for="a in context.message.attachments" :key="a" data-testid="trigger-context-attachment">📎 {{ a }}</li>
				</ul>
			</li>
			<li v-for="(m, i) in context.after ?? []" :key="`a${i}`" data-testid="trigger-context-after"
				class="text-gray-500 dark:text-gray-400">
				<TriggerLine :message="m" />
			</li>
		</ol>
	</section>
</template>

<script setup lang="ts">
import { defineComponent, h, type PropType } from "vue";
import type { TriggerContext, TriggerMessage } from "../../../stores/jobs";

// The channel message that started a workflow run, between the messages just
// before and after it. Senders show by display name only.

defineProps<{ context: TriggerContext }>();

function when(ts: string): string {
	const d = new Date(ts);
	return Number.isNaN(d.getTime()) ? ts : d.toLocaleString();
}

function senderOf(m: TriggerMessage): string {
	if (m.from_agent) return "Agent";
	return m.sender || "Unknown sender";
}

const TriggerLine = defineComponent({
	props: {
		message: { type: Object as PropType<TriggerMessage>, required: true },
	},
	setup(props) {
		return () =>
			h("span", { class: "block whitespace-pre-wrap break-words" }, [
				h(
					"span",
					{ class: "mr-2 text-xs tabular-nums opacity-70" },
					new Date(props.message.timestamp).toLocaleTimeString(),
				),
				h("span", { class: "font-semibold" }, `${senderOf(props.message)}: `),
				props.message.text,
			]);
	},
});
</script>
