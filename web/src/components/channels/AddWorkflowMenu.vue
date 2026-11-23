<template>
	<button v-if="!channels.length" type="button" disabled :class="buttonClass" data-testid="add-workflow"
		title="Add a channel first: workflows run on a channel's messages">
		<BoltIcon aria-hidden="true" class="h-4 w-4" />
		Add workflow
	</button>
	<router-link v-else-if="channels.length === 1" :to="workflowSettingsPath(agent, channelName(channels[0]))"
		:class="buttonClass" data-testid="add-workflow">
		<BoltIcon aria-hidden="true" class="h-4 w-4" />
		Add workflow
	</router-link>
	<DropdownMenuRoot v-else>
		<DropdownMenuTrigger asChild>
			<button type="button" :class="buttonClass" data-testid="add-workflow" aria-haspopup="menu">
				<BoltIcon aria-hidden="true" class="h-4 w-4" />
				Add workflow
				<ChevronDownIcon aria-hidden="true" class="h-3.5 w-3.5 opacity-70" />
			</button>
		</DropdownMenuTrigger>
		<DropdownMenuPortal>
			<DropdownMenuContent align="end" :side-offset="4" data-testid="add-workflow-menu"
				class="z-50 min-w-48 rounded-xl border border-gray-200 bg-white p-1 shadow-lg dark:border-gray-700 dark:bg-gray-900">
				<DropdownMenuLabel class="px-3 py-1.5 text-xs font-medium text-gray-500 dark:text-gray-400">Add a workflow to</DropdownMenuLabel>
				<DropdownMenuItem v-for="c in channels" :key="channelName(c)" data-testid="add-workflow-channel"
					class="flex cursor-pointer items-center gap-2 rounded-lg px-3 py-2 text-sm text-gray-800 outline-none data-[highlighted]:bg-gray-100 dark:text-gray-200 dark:data-[highlighted]:bg-gray-800"
					@select="router.push(workflowSettingsPath(agent, channelName(c)))">
					<MessagingLogo v-if="isKnownChannelType(c.type)" :name="c.type" :disabled="c.enabled === false" class="h-4 w-4 shrink-0" />
					<span class="truncate">{{ channelName(c) }}</span>
				</DropdownMenuItem>
			</DropdownMenuContent>
		</DropdownMenuPortal>
	</DropdownMenuRoot>
</template>

<script setup lang="ts">
import BoltIcon from "@heroicons/vue/24/outline/BoltIcon";
import ChevronDownIcon from "@heroicons/vue/24/outline/ChevronDownIcon";
import {
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuLabel,
	DropdownMenuPortal,
	DropdownMenuRoot,
	DropdownMenuTrigger,
} from "radix-vue";
import { computed } from "vue";
import { useRouter } from "vue-router";
import {
	channelName,
	isKnownChannelType,
	workflowSettingsPath,
} from "../../lib/channels";
import type { AgentChannel } from "../../stores/settings";
import MessagingLogo from "../MessagingLogo.vue";

// Adds a workflow: picks the channel it watches, then opens that channel's
// Workflows tab, where the workflow is set up.

const props = defineProps<{
	agent: string;
	channels: AgentChannel[];
	primary?: boolean;
}>();

const router = useRouter();

const buttonClass = computed(() => [
	"inline-flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-sm font-semibold disabled:cursor-not-allowed disabled:opacity-50",
	props.primary
		? "bg-blue-600 text-white hover:bg-blue-500"
		: "border border-gray-200 text-gray-700 hover:bg-gray-50 dark:border-gray-700 dark:text-gray-200 dark:hover:bg-gray-800",
]);
</script>
