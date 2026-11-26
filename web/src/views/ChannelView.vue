<template>
	<AppLayout>
		<!-- One channel: its conversation, with its settings behind the gear. -->
		<template v-if="channel && isKnownChannelType(channel.type)" #header-icon>
			<MessagingLogo :name="channel.type" :disabled="channel.enabled === false" class="h-4 w-4" />
		</template>
		<template v-if="channel" #header-status>
			<span data-testid="channel-state" :data-enabled="channel.enabled !== false"
				class="shrink-0 rounded-full px-2 py-0.5 text-xs font-semibold"
				:class="channel.enabled === false ? 'bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-300' : 'bg-green-50 text-green-700 dark:bg-green-950/50 dark:text-green-300'">
				{{ channel.enabled === false ? "Disabled" : "Enabled" }}
			</span>
		</template>
		<template v-if="channel" #header>
			<p class="truncate" data-testid="channel-detail">
				{{ typeLabel }}<template v-if="channel.enabled === false"> · disabled</template>
				<template v-if="channel.deliver_to"> · delivers to {{ channel.deliver_to }}</template>
			</p>
		</template>
		<template v-if="channel" #header-actions>
			<!-- The wrapper sizes the picker to its label and lets it shrink, so
				the gear stays beside it (#1347). -->
			<div v-if="sessions.length > 1" class="min-w-0 max-w-xs">
				<AppSelect v-model="selectedId" data-testid="channel-session-picker"
					aria-label="Conversation" :class="HEADER_SELECT_CLS"
					:options="sessions.map((s) => ({ value: s.id, label: sessionLabel(s) }))" />
			</div>
			<router-link :to="channelSettingsPath(agent, channelName(channel))" data-testid="channel-settings-link"
				aria-label="Channel settings" title="Channel settings" :class="HEADER_ICON_CLS">
				<Cog6ToothIcon aria-hidden="true" class="h-5 w-5" />
			</router-link>
		</template>
		<template v-if="!channelParam" #header-actions>
			<AddWorkflowMenu :agent="agent" :channels="channels" />
			<button type="button" data-testid="channels-add" :class="HEADER_ACTION_CLS"
				@click="openChannelDialog({ mode: 'create', agent })">
				<PlusIcon aria-hidden="true" class="h-4 w-4" />
				Add channel
			</button>
		</template>
		<div class="flex min-h-0 flex-1 flex-col">
			<WorkflowStrip v-if="channel" :agent="agent" :workflows="channelWorkflows" :status-of="statusOf" />

			<div v-if="channel" class="min-h-0 flex-1">
				<ChatView v-if="selectedId" :key="selectedId" :agent="agent" :session="selectedId" data-testid="channel-chat" />
				<p v-else-if="error" role="alert" class="px-6 py-4 text-sm text-red-600 dark:text-red-400">{{ error }}</p>
			</div>
			<ChannelsOverview v-else-if="!channelParam" :agent="agent" :channels="channels"
				:workflows="agentWorkflows" :status-of="statusOf" />
			<div v-else class="flex h-full flex-col items-center justify-center gap-3 px-6 text-center text-sm text-gray-500 dark:text-gray-400"
				data-testid="channel-missing">
				<p>No channel named <span class="font-medium text-gray-900 dark:text-white">{{ channelParam }}</span>.</p>
			</div>
		</div>
	</AppLayout>
</template>

<script setup lang="ts">
import Cog6ToothIcon from "@heroicons/vue/24/outline/Cog6ToothIcon";
import PlusIcon from "@heroicons/vue/24/outline/PlusIcon";
import { computed, ref, watch } from "vue";
import { useRoute } from "vue-router";
import AppLayout from "../components/AppLayout.vue";
import AddWorkflowMenu from "../components/channels/AddWorkflowMenu.vue";
import ChannelsOverview from "../components/channels/ChannelsOverview.vue";
import WorkflowStrip from "../components/channels/WorkflowStrip.vue";
import MessagingLogo from "../components/MessagingLogo.vue";
import { AppSelect } from "../components/ui/select";
import { useChannelDialogs } from "../composables/useChannelDialogs";
import { useMCP } from "../composables/useMCP";
import { useWorkflowStatus } from "../composables/useWorkflowStatus";
import {
	type ChannelSession,
	channelName,
	channelSettingsPath,
	channelTypeLabel,
	channelWorkflows as channelWorkflowsOf,
	isKnownChannelType,
} from "../lib/channels";
import {
	HEADER_ACTION_CLS,
	HEADER_ICON_CLS,
	HEADER_SELECT_CLS,
} from "../lib/headerActions";
import { isWorkflow, useSettingsStore } from "../stores/settings";
import ChatView from "./ChatView.vue";

const route = useRoute();
const settings = useSettingsStore();
const { callTool } = useMCP();
const { open: openChannelDialog } = useChannelDialogs();

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
	return c ? channelWorkflowsOf(c, agentWorkflows.value) : [];
});
const { statusOf } = useWorkflowStatus(agent);

const typeLabel = computed(() => channelTypeLabel(channel.value?.type ?? ""));

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
</script>
