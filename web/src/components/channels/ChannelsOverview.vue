<template>
	<div class="min-h-0 flex-1 overflow-y-auto" data-testid="channels-overview">
		<div class="mx-auto max-w-7xl px-4 py-6 sm:px-6">
			<div class="space-y-8">
				<section aria-label="Channel summary" class="space-y-3">
					<p class="text-sm text-gray-600 dark:text-gray-400">
						Channels connect {{ agent }} to Slack, Discord and Signal. Each keeps its conversations here, and its workflows act on the messages it receives.
					</p>
					<div class="grid grid-cols-2 gap-3 lg:grid-cols-4" data-testid="channels-stats">
						<StatCard label="Channels" :value="channels.length" tone="neutral" data-testid="stat-channels"
							:caption="`${enabledCount} enabled`" />
						<StatCard label="Messages 24h" :value="loaded ? messages24h : '…'" tone="green" data-testid="stat-messages"
							:caption="lastActivity ? `last ${lastActivity}` : 'no messages yet'" />
						<StatCard label="Workflows" :value="workflows.length" tone="purple" data-testid="stat-workflows"
							:caption="`${activeWorkflows} active`" />
						<StatCard label="Running" :value="runningWorkflows" tone="blue" data-testid="stat-running"
							:pulse="runningWorkflows > 0" :caption="runningWorkflows === 1 ? 'workflow running' : 'workflows running'" />
					</div>
				</section>

				<section aria-labelledby="channels-overview-channels" data-testid="channels-overview-channels">
					<SectionHeading id="channels-overview-channels" title="Channels" :count="channels.length" />
					<EmptyStateCard v-if="!channels.length" title="No channels yet" data-testid="channels-empty">
						<template #icon><ChatBubbleLeftRightIcon class="h-5 w-5" /></template>
						A channel connects this agent to Slack, Discord or Signal, and keeps its conversation here.
						<template #actions>
							<button type="button" data-testid="channels-empty-add"
								class="inline-flex items-center gap-1 rounded-lg bg-blue-600 px-4 py-2 text-sm font-semibold text-white hover:bg-blue-500"
								@click="openCreate">
								<PlusIcon aria-hidden="true" class="h-4 w-4" />
								Add channel
							</button>
						</template>
					</EmptyStateCard>
					<div v-else class="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
						<ChannelCard v-for="c in channels" :key="channelName(c)" :agent="agent" :channel="c"
							:activity="activity[channelName(c)]" :workflow-count="channelWorkflows(c, workflows).length" :now="now" />
						<button type="button" data-testid="channel-card-add"
							class="flex min-h-32 flex-col items-center justify-center gap-1 rounded-xl border border-dashed border-gray-300 px-4 py-3 text-sm font-medium text-gray-600 hover:border-blue-400 hover:text-blue-600 dark:border-gray-700 dark:text-gray-300 dark:hover:border-blue-600 dark:hover:text-blue-400"
							@click="openCreate">
							<PlusIcon aria-hidden="true" class="h-5 w-5" />
							Add channel
						</button>
					</div>
				</section>

				<section aria-labelledby="channels-overview-workflows-heading" data-testid="channels-overview-workflows">
					<SectionHeading id="channels-overview-workflows-heading" title="Workflows" :count="workflows.length">
						<AddWorkflowMenu v-if="workflows.length" :agent="agent" :channels="channels" />
					</SectionHeading>
					<EmptyStateCard v-if="!workflows.length" title="No workflows yet" data-testid="workflows-empty">
						<template #icon><BoltIcon class="h-5 w-5" /></template>
						A workflow runs when a message matching its pattern arrives on a channel, and replaces the agent's reply to it.
						<template #actions>
							<AddWorkflowMenu :agent="agent" :channels="channels" primary />
						</template>
					</EmptyStateCard>
					<div v-else class="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
						<WorkflowCard v-for="w in sortedWorkflows" :key="w.name" :agent="agent" :workflow="w" :status="statusOf(w.name)" wide />
					</div>
				</section>

				<section v-if="channels.length" aria-labelledby="channels-overview-recent" data-testid="channels-overview-recent">
					<SectionHeading id="channels-overview-recent" title="Recent messages">
						<div class="relative w-full min-w-0 sm:w-64">
							<span class="pointer-events-none absolute inset-y-0 left-3 flex items-center text-gray-400 dark:text-gray-500" aria-hidden="true">
								<MagnifyingGlassIcon class="h-4 w-4" />
							</span>
							<input v-model="search" type="search" aria-label="Search recent messages" placeholder="Search messages…"
								data-testid="channels-overview-search" class="field-input pl-9!" />
						</div>
					</SectionHeading>
					<div class="grid items-start gap-3 lg:grid-cols-2">
						<ChannelMessagesCard v-for="c in channels" :key="channelName(c)" :agent="agent" :channel="c"
							:messages="shownMessages(c)" :loading="!activity[channelName(c)]" :filtered="Boolean(search.trim())" :now="now" />
					</div>
				</section>
			</div>
		</div>
	</div>
</template>

<script setup lang="ts">
import BoltIcon from "@heroicons/vue/24/outline/BoltIcon";
import ChatBubbleLeftRightIcon from "@heroicons/vue/24/outline/ChatBubbleLeftRightIcon";
import MagnifyingGlassIcon from "@heroicons/vue/24/outline/MagnifyingGlassIcon";
import PlusIcon from "@heroicons/vue/24/outline/PlusIcon";
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import { useChannelDialogs } from "../../composables/useChannelDialogs";
import { useMCP } from "../../composables/useMCP";
import type { WorkflowStatus } from "../../composables/useWorkflowStatus";
import { channelName, channelWorkflows } from "../../lib/channels";
import type { AgentChannel, AgentTask } from "../../stores/settings";
import EmptyStateCard from "../ui/EmptyStateCard.vue";
import SectionHeading from "../ui/SectionHeading.vue";
import StatCard from "../ui/StatCard.vue";
import ChannelCard from "./ChannelCard.vue";
import ChannelMessagesCard from "./ChannelMessagesCard.vue";
import { ago, type ChannelActivity } from "./channelActivity";
import WorkflowCard from "./WorkflowCard.vue";
import AddWorkflowMenu from "./AddWorkflowMenu.vue";

// The Channels page before a channel is picked: a summary of the agent's
// channels, a card per channel, every workflow, and each channel's latest
// messages under a heading that opens its chat.

const props = defineProps<{
	agent: string;
	channels: AgentChannel[];
	workflows: AgentTask[];
	statusOf: (name: string) => WorkflowStatus;
}>();

const { callTool } = useMCP();
const { open } = useChannelDialogs();

function openCreate() {
	open({ mode: "create", agent: props.agent });
}
// Relative times stay current while the page is open.
const now = ref(Date.now());
let clock: ReturnType<typeof setInterval> | undefined;
onMounted(() => {
	clock = setInterval(() => {
		now.value = Date.now();
	}, 30_000);
});
onUnmounted(() => clearInterval(clock));

const sortedWorkflows = computed(() =>
	props.workflows
		.slice()
		.sort(
			(a, b) =>
				Number(a.enabled === false) - Number(b.enabled === false) ||
				a.name.localeCompare(b.name),
		),
);

const enabledCount = computed(
	() => props.channels.filter((c) => c.enabled !== false).length,
);
const activeWorkflows = computed(
	() => props.workflows.filter((w) => w.enabled !== false).length,
);
const runningWorkflows = computed(
	() =>
		props.workflows.filter((w) => props.statusOf(w.name).activity !== "idle")
			.length,
);

const activity = ref<Record<string, ChannelActivity>>({});
const loaded = computed(() =>
	props.channels.every((c) => activity.value[channelName(c)]),
);
const messages24h = computed(() =>
	Object.values(activity.value).reduce((n, a) => n + a.messages_24h, 0),
);
const lastActivity = computed(() => {
	const latest = Object.values(activity.value)
		.map((a) => a.last_activity ?? "")
		.sort()
		.at(-1);
	return ago(latest, now.value);
});

const search = ref("");
function shownMessages(c: AgentChannel) {
	const all = (activity.value[channelName(c)]?.messages ?? [])
		.slice()
		.reverse();
	const q = search.value.trim().toLowerCase();
	if (!q) return all;
	return all.filter((m) =>
		[m.text, m.sender, m.conversation, ...(m.workflows ?? [])].some((s) =>
			s?.toLowerCase().includes(q),
		),
	);
}

async function load() {
	// One at a time: the live server's MCP transport prefers sequential calls.
	for (const c of props.channels) {
		const name = channelName(c);
		let got: Partial<ChannelActivity> | null = null;
		try {
			got = JSON.parse(
				await callTool("channel_activity", {
					agent: props.agent,
					channel: name,
					limit: 5,
				}),
			);
		} catch {
			// Shown as a channel with no traffic.
		}
		const value: ChannelActivity = {
			last_activity: got?.last_activity,
			messages_24h: got?.messages_24h ?? 0,
			messages: Array.isArray(got?.messages) ? got.messages : [],
		};
		activity.value = { ...activity.value, [name]: value };
	}
}

watch(
	() => [props.agent, props.channels.map((c) => channelName(c)).join("\n")],
	() => {
		activity.value = {};
		void load();
	},
	{ immediate: true },
);
</script>
