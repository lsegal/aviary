<template>
	<AppLayout>
		<div class="flex h-full min-h-0 flex-col">
			<!-- One channel: its conversation, with its settings behind the gear. -->
			<header v-if="channel" data-testid="channel-header"
				class="flex flex-wrap items-center gap-3 border-b border-gray-200 px-4 py-3 sm:px-6 dark:border-gray-800">
				<span class="inline-flex h-7 w-7 shrink-0 items-center justify-center rounded-lg bg-gray-100 dark:bg-gray-800">
					<MessagingLogo v-if="isKnownType(channel.type)" :name="channel.type" :disabled="channel.enabled === false" class="h-4 w-4" />
				</span>
				<div class="min-w-0">
					<h2 class="truncate text-lg font-bold text-gray-900 dark:text-white" data-testid="channel-title">{{ channelName(channel) }}</h2>
					<p class="text-xs text-gray-500 dark:text-gray-400">
						{{ typeLabel }}<template v-if="channel.enabled === false"> · disabled</template>
						<template v-if="channel.deliver_to"> · delivers to {{ channel.deliver_to }}</template>
					</p>
				</div>
				<div class="ml-auto flex items-center gap-2">
					<AppSelect v-if="sessions.length > 1" v-model="selectedId" data-testid="channel-session-picker"
						aria-label="Conversation" class="max-w-xs"
						:options="sessions.map((s) => ({ value: s.id, label: sessionLabel(s) }))" />
					<router-link :to="channelSettingsPath(agent, channelName(channel))" data-testid="channel-settings-link"
						aria-label="Channel settings" title="Channel settings"
						class="rounded-lg border border-gray-200 p-1.5 text-gray-700 hover:bg-gray-50 dark:border-gray-700 dark:text-gray-200 dark:hover:bg-gray-800">
						<Cog6ToothIcon aria-hidden="true" class="h-5 w-5" />
					</router-link>
				</div>
			</header>

			<WorkflowStrip v-if="channel" :agent="agent" :workflows="channelWorkflows" :status-of="statusOf" />

			<div v-if="channel" class="min-h-0 flex-1">
				<ChatView v-if="selectedId" :key="selectedId" :agent="agent" :session="selectedId" data-testid="channel-chat" />
				<p v-else-if="error" role="alert" class="px-6 py-4 text-sm text-red-600 dark:text-red-400">{{ error }}</p>
			</div>
			<ChannelsOverview v-else-if="!channelParam && channels.length" :agent="agent" :channels="channels"
				:workflows="agentWorkflows" :status-of="statusOf" />
			<div v-else class="flex h-full flex-col items-center justify-center gap-3 px-6 text-center text-sm text-gray-500 dark:text-gray-400"
				data-testid="channels-empty">
				<template v-if="channelParam">
					<p>No channel named <span class="font-medium text-gray-900 dark:text-white">{{ channelParam }}</span>.</p>
				</template>
				<template v-else>
					<p>No channels yet. A channel connects this agent to Slack, Discord or Signal, and keeps its conversation here.</p>
					<button type="button" data-testid="channels-empty-add"
						class="rounded-lg bg-blue-600 px-4 py-2 text-sm font-semibold text-white hover:bg-blue-500"
						@click="openCreate">Add channel</button>
				</template>
			</div>
		</div>
	</AppLayout>
</template>

<script setup lang="ts">
import Cog6ToothIcon from "@heroicons/vue/24/outline/Cog6ToothIcon";
import { computed, ref, watch } from "vue";
import { useRoute } from "vue-router";
import AppLayout from "../components/AppLayout.vue";
import ChannelsOverview from "../components/channels/ChannelsOverview.vue";
import WorkflowStrip from "../components/channels/WorkflowStrip.vue";
import MessagingLogo from "../components/MessagingLogo.vue";
import { AppSelect } from "../components/ui/select";
import { useChannelDialogs } from "../composables/useChannelDialogs";
import { useMCP } from "../composables/useMCP";
import { useWorkflowStatus } from "../composables/useWorkflowStatus";
import {
	CHANNEL_TYPES,
	type ChannelSession,
	channelName,
	channelSettingsPath,
} from "../lib/channels";
import { isWorkflow, useSettingsStore } from "../stores/settings";
import ChatView from "./ChatView.vue";

const route = useRoute();
const settings = useSettingsStore();
const { callTool } = useMCP();
const { open } = useChannelDialogs();

const agent = computed(() =>
	typeof route.params.agent === "string" ? route.params.agent : "",
);
const channelParam = computed(() =>
	typeof route.params.channel === "string" ? route.params.channel : "",
);

const channels = computed(
	() =>
		settings.config?.agents.find((a) => a.name === agent.value)?.channels ?? [],
);
const channel = computed(() =>
	channelParam.value
		? channels.value.find(
				(c) =>
					channelName(c).toLowerCase() === channelParam.value.toLowerCase(),
			)
		: undefined,
);

// The agent's workflows, and the open channel's, with their live state.
const agentWorkflows = computed(
	() =>
		settings.config?.agents
			.find((a) => a.name === agent.value)
			?.tasks.filter((t) => isWorkflow(t)) ?? [],
);
const channelWorkflows = computed(() => {
	const c = channel.value;
	if (!c) return [];
	const names = [channelName(c).toLowerCase(), (c.id ?? "").toLowerCase()];
	return agentWorkflows.value.filter((t) =>
		names.includes((t.channel ?? "").trim().toLowerCase()),
	);
});
const { statusOf } = useWorkflowStatus(agent);

const typeLabel = computed(
	() =>
		CHANNEL_TYPES.find((t) => t.value === channel.value?.type)?.label ??
		channel.value?.type ??
		"",
);

function isKnownType(type: string): type is "slack" | "discord" | "signal" {
	return type === "slack" || type === "discord" || type === "signal";
}

const sessions = ref<ChannelSession[]>([]);
const selectedId = ref("");
const error = ref("");

function sessionLabel(s: ChannelSession): string {
	if (s.own) return "Channel conversation";
	return s.title || s.name || s.id;
}

// The channel's sessions: its own first, then conversations it received.
async function loadSessions() {
	sessions.value = [];
	selectedId.value = "";
	error.value = "";
	const c = channel.value;
	if (!c) return;
	try {
		const raw = await callTool("channel_sessions", {
			agent: agent.value,
			channel: channelName(c),
		});
		sessions.value = (JSON.parse(raw) as ChannelSession[]) ?? [];
		selectedId.value = sessions.value[0]?.id ?? "";
	} catch (e) {
		error.value = e instanceof Error ? e.message : String(e);
	}
}

// Session IDs are keyed by the channel's type and ID, which a rename keeps.
watch(
	() => [agent.value, channel.value?.type, channel.value?.id],
	() => void loadSessions(),
	{ immediate: true },
);

function openCreate() {
	open({ mode: "create", agent: agent.value });
}
</script>
