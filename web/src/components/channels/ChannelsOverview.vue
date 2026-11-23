<template>
	<div class="min-h-0 flex-1 overflow-y-auto" data-testid="channels-overview">
		<div class="space-y-6 px-4 py-4 sm:px-6">
			<section aria-labelledby="channels-overview-workflows" data-testid="channels-overview-workflows">
				<h2 id="channels-overview-workflows" class="mb-2 text-sm font-semibold text-gray-800 dark:text-gray-200">Workflows</h2>
				<p v-if="!workflows.length" class="text-sm text-gray-500 dark:text-gray-400">
					No workflows yet. Add one from a channel's settings, under Workflows.
				</p>
				<div v-else class="flex flex-wrap gap-2">
					<WorkflowCard v-for="w in sortedWorkflows" :key="w.name" :agent="agent" :workflow="w" :status="statusOf(w.name)" />
				</div>
			</section>

			<section aria-labelledby="channels-overview-recent" data-testid="channels-overview-recent">
				<h2 id="channels-overview-recent" class="mb-2 text-sm font-semibold text-gray-800 dark:text-gray-200">Recent messages</h2>
				<div class="space-y-4">
					<div v-for="c in channels" :key="channelName(c)" data-testid="channels-overview-channel" :data-channel="channelName(c)">
						<router-link :to="channelPath(agent, channelName(c))" data-testid="channels-overview-channel-link"
							class="inline-flex items-center gap-2 text-sm font-semibold text-blue-600 hover:underline dark:text-blue-400">
							<MessagingLogo v-if="isKnownType(c.type)" :name="c.type" :disabled="c.enabled === false" class="h-3.5 w-3.5" />
							{{ channelName(c) }}
						</router-link>
						<ul v-if="recent[channelName(c)]?.length" class="mt-1 space-y-0.5 text-sm">
							<li v-for="(m, i) in recent[channelName(c)]" :key="i" data-testid="channels-overview-message"
								class="flex gap-2 text-gray-700 dark:text-gray-300">
								<span class="shrink-0 text-xs tabular-nums text-gray-400">{{ time(m.timestamp) }}</span>
								<span class="min-w-0 truncate"><span class="font-semibold">{{ m.sender || "Unknown sender" }}:</span> {{ m.text }}</span>
							</li>
						</ul>
						<p v-else class="mt-1 text-xs text-gray-500 dark:text-gray-400">No messages yet.</p>
					</div>
				</div>
			</section>
		</div>
	</div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useMCP } from "../../composables/useMCP";
import type { WorkflowStatus } from "../../composables/useWorkflowStatus";
import { channelName, channelPath } from "../../lib/channels";
import type { AgentChannel, AgentTask } from "../../stores/settings";
import MessagingLogo from "../MessagingLogo.vue";
import WorkflowCard from "./WorkflowCard.vue";

// The Channels page before a channel is picked: every workflow of the agent,
// then each channel's latest messages under a heading that opens its chat.

const props = defineProps<{
	agent: string;
	channels: AgentChannel[];
	workflows: AgentTask[];
	statusOf: (name: string) => WorkflowStatus;
}>();

const { callTool } = useMCP();

const sortedWorkflows = computed(() =>
	props.workflows
		.slice()
		.sort(
			(a, b) =>
				Number(a.enabled === false) - Number(b.enabled === false) ||
				a.name.localeCompare(b.name),
		),
);

function isKnownType(type: string): type is "slack" | "discord" | "signal" {
	return type === "slack" || type === "discord" || type === "signal";
}

function time(ts: string): string {
	const d = new Date(ts);
	return Number.isNaN(d.getTime())
		? ""
		: d.toLocaleString([], {
				month: "short",
				day: "numeric",
				hour: "2-digit",
				minute: "2-digit",
			});
}

interface RecentMessage {
	sender?: string;
	text: string;
	timestamp: string;
}
const recent = ref<Record<string, RecentMessage[]>>({});

async function load() {
	const next: Record<string, RecentMessage[]> = {};
	// One at a time: the live server's MCP transport prefers sequential calls.
	for (const c of props.channels) {
		const name = channelName(c);
		try {
			const raw = await callTool("workflow_preview", {
				agent: props.agent,
				channel: name,
				limit: 5,
			});
			next[name] = (
				(JSON.parse(raw) as RecentMessage[] | null) ?? []
			).reverse();
		} catch {
			next[name] = [];
		}
	}
	recent.value = next;
}

watch(
	() => [props.agent, props.channels.map((c) => channelName(c)).join("\n")],
	() => void load(),
	{ immediate: true },
);
</script>
