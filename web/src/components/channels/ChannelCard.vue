<template>
	<!-- The whole card opens the channel's chat: its name is a link stretched
		over the card, and the controls sit above it. -->
	<article data-testid="channel-card" :data-channel="name"
		class="relative flex min-w-0 flex-col gap-3 rounded-xl border border-gray-200 bg-white px-4 py-3 focus-within:ring-2 focus-within:ring-blue-500 hover:border-blue-300 hover:shadow-sm dark:border-gray-800 dark:bg-gray-900 dark:hover:border-blue-700"
		:class="enabled ? '' : 'opacity-75'">
		<div class="flex items-center gap-3">
			<span class="inline-flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-gray-100 dark:bg-gray-800">
				<MessagingLogo v-if="isKnownChannelType(channel.type)" :name="channel.type" :disabled="!enabled" class="h-5 w-5" />
			</span>
			<div class="min-w-0 flex-1">
				<h3 class="truncate text-sm font-semibold text-gray-900 dark:text-white">
					<router-link :to="channelPath(agent, name)" data-testid="channel-card-link"
						class="after:absolute after:inset-0 after:rounded-xl focus:outline-none">{{ name }}</router-link>
				</h3>
				<p class="truncate text-xs text-gray-500 dark:text-gray-400">{{ channelTypeLabel(channel.type) }}</p>
			</div>
			<span data-testid="channel-card-status" class="inline-flex shrink-0 items-center gap-1 rounded-full px-2 py-0.5 text-xs font-medium"
				:class="enabled ? 'bg-green-50 text-green-700 dark:bg-green-950/50 dark:text-green-300' : 'bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-300'">
				<span class="h-1.5 w-1.5 rounded-full" :class="enabled ? 'bg-green-500' : 'bg-gray-400'" />
				{{ enabled ? "Enabled" : "Disabled" }}
			</span>
		</div>

		<dl class="grid grid-cols-3 gap-2 text-xs">
			<div class="min-w-0">
				<dt class="text-gray-500 dark:text-gray-400">Last activity</dt>
				<dd class="truncate font-medium text-gray-900 dark:text-white" data-testid="channel-card-last-activity"
					:title="activity?.last_activity ? fullTime(activity.last_activity) : undefined">
					{{ activity ? ago(activity.last_activity, now) || "None yet" : "…" }}</dd>
			</div>
			<div class="min-w-0">
				<dt class="text-gray-500 dark:text-gray-400">Messages 24h</dt>
				<dd class="font-medium tabular-nums text-gray-900 dark:text-white" data-testid="channel-card-messages">
					{{ activity ? activity.messages_24h : "…" }}</dd>
			</div>
			<div class="min-w-0">
				<dt class="text-gray-500 dark:text-gray-400">Workflows</dt>
				<dd class="font-medium tabular-nums text-gray-900 dark:text-white" data-testid="channel-card-workflows">{{ workflowCount }}</dd>
			</div>
		</dl>

		<div class="relative z-10 mt-auto flex items-center gap-2 border-t border-gray-100 pt-2 dark:border-gray-800">
			<router-link :to="channelPath(agent, name)" data-testid="channel-card-chat"
				class="inline-flex items-center gap-1 rounded-lg px-2 py-1 text-xs font-medium text-blue-600 hover:bg-blue-50 dark:text-blue-400 dark:hover:bg-blue-950/40">
				<ChatBubbleLeftRightIcon aria-hidden="true" class="h-4 w-4" />
				Open chat
			</router-link>
			<router-link :to="channelSettingsPath(agent, name)" data-testid="channel-card-settings"
				class="inline-flex items-center gap-1 rounded-lg px-2 py-1 text-xs font-medium text-gray-700 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-gray-800">
				<Cog6ToothIcon aria-hidden="true" class="h-4 w-4" />
				Settings
			</router-link>
			<TaskEnabledSwitch class="ml-auto" :task="name" :enabled="enabled" :busy="isPending(agent, name)"
				data-testid="channel-card-enabled" @update="setChannelEnabled(agent, name, $event)" />
		</div>
	</article>
</template>

<script setup lang="ts">
import ChatBubbleLeftRightIcon from "@heroicons/vue/24/outline/ChatBubbleLeftRightIcon";
import Cog6ToothIcon from "@heroicons/vue/24/outline/Cog6ToothIcon";
import { computed } from "vue";
import { useChannelEnabled } from "../../composables/useChannelEnabled";
import {
	channelName,
	channelPath,
	channelSettingsPath,
	channelTypeLabel,
	isKnownChannelType,
} from "../../lib/channels";
import type { AgentChannel } from "../../stores/settings";
import MessagingLogo from "../MessagingLogo.vue";
import TaskEnabledSwitch from "../tasks/TaskEnabledSwitch.vue";
import { ago, type ChannelActivity, fullTime } from "./channelActivity";

// A channel on the Channels overview: its type and status, its recent
// traffic and workflows, and its chat, settings and enable switch.

const props = defineProps<{
	agent: string;
	channel: AgentChannel;
	/** Its traffic; undefined while it loads. */
	activity?: ChannelActivity;
	workflowCount: number;
	now: number;
}>();

const { setChannelEnabled, isPending } = useChannelEnabled();

const name = computed(() => channelName(props.channel));
const enabled = computed(() => props.channel.enabled !== false);
</script>
