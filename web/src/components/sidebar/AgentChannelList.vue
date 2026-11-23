<template>
	<SidebarItemList :items="channels" :item-key="(c) => channelName(c)" more-testid="sidebar-channels-more"
		empty="No channels yet" data-testid="sidebar-channels">
		<template #default="{ item: c }">
			<SidebarItemRow data-testid="channel-row" :data-channel="channelName(c)" :data-disabled="isDisabled(c)"
				:to="channelPath(agent, channelName(c))" :label="channelName(c)" :active="channelName(c) === activeName"
				:muted="isDisabled(c)" :tooltip="isDisabled(c) ? 'Disabled' : undefined" :actions="actionsFor(c)"
				menu-testid="channel-menu-button"
				:editing="editing === channelName(c)" @select="(key) => select(key, channelName(c))"
				@dblclick="startRename(channelName(c))">
				<template #editor>
					<input :ref="setEditInput" v-model="editName" data-testid="channel-rename-input"
						:aria-label="`Rename ${channelName(c)}`"
						class="m-0.5 min-w-0 flex-1 rounded-md border border-blue-500 bg-white px-2.5 py-1.5 text-sm text-gray-900 focus:outline-none dark:bg-gray-900 dark:text-white"
						@keydown.enter.prevent="saveRename" @keydown.esc.prevent="cancelRename" @blur="saveRename" />
				</template>
				<template #meta>
					<span v-if="isDisabled(c)" data-testid="channel-row-disabled" title="Disabled"
						class="mr-1 inline-flex shrink-0 text-gray-400 dark:text-gray-500">
						<NoSymbolIcon aria-hidden="true" class="h-3.5 w-3.5" />
						<span class="sr-only">Disabled</span>
					</span>
				</template>
			</SidebarItemRow>
		</template>
	</SidebarItemList>
</template>

<script setup lang="ts">
import NoSymbolIcon from "@heroicons/vue/20/solid/NoSymbolIcon";
import CheckCircleIcon from "@heroicons/vue/24/outline/CheckCircleIcon";
import Cog6ToothIcon from "@heroicons/vue/24/outline/Cog6ToothIcon";
import NoSymbolOutlineIcon from "@heroicons/vue/24/outline/NoSymbolIcon";
import { computed, nextTick, ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useChannelDialogs } from "../../composables/useChannelDialogs";
import { useChannelEnabled } from "../../composables/useChannelEnabled";
import { useMCP } from "../../composables/useMCP";
import {
	channelName,
	channelPath,
	channelSettingsPath,
} from "../../lib/channels";
import { type AgentChannel, useSettingsStore } from "../../stores/settings";
import SidebarItemList from "./SidebarItemList.vue";
import SidebarItemRow from "./SidebarItemRow.vue";
import type { SidebarItemAction } from "./types";

const props = defineProps<{ agent: string }>();

const settings = useSettingsStore();
const route = useRoute();
const router = useRouter();
const { callTool } = useMCP();
const { open } = useChannelDialogs();
const { setChannelEnabled, isPending } = useChannelEnabled();

const isDisabled = (c: AgentChannel) => c.enabled === false;

// Disabled channels sort after the enabled ones, each kept in config order.
const channels = computed(() => {
	const all =
		settings.config?.agents.find((a) => a.name === props.agent)?.channels ?? [];
	return [
		...all.filter((c) => !isDisabled(c)),
		...all.filter((c) => isDisabled(c)),
	];
});

// The channel whose conversation or settings are showing.
const activeName = computed(() => {
	const match = /^\/agents\/[^/]+\/channels\/([^/]+)/.exec(route.path);
	return match ? decodeURIComponent(match[1]) : "";
});

function actionsFor(c: AgentChannel): SidebarItemAction[] {
	const disabled = isDisabled(c);
	return [
		{
			key: "toggle",
			label: disabled ? "Enable" : "Disable",
			icon: disabled ? CheckCircleIcon : NoSymbolOutlineIcon,
			disabled: isPending(props.agent, channelName(c)),
		},
		{ key: "rename", label: "Rename" },
		{ key: "settings", label: "Settings", icon: Cog6ToothIcon },
		{ key: "delete", label: "Delete", danger: true },
	];
}

function select(key: string, name: string) {
	const c = channels.value.find((ch) => channelName(ch) === name);
	if (key === "toggle") {
		if (c) void setChannelEnabled(props.agent, name, isDisabled(c));
	} else if (key === "settings")
		void router.push(channelSettingsPath(props.agent, name));
	else if (key === "rename") void startRename(name);
	else open({ mode: "remove", agent: props.agent, channel: name });
}

const editing = ref("");
const editName = ref("");
let editInput: HTMLInputElement | null = null;
function setEditInput(el: unknown) {
	editInput = el instanceof HTMLInputElement ? el : null;
}

async function startRename(name: string) {
	editing.value = name;
	editName.value = name;
	// Wait for the menu to close and hand focus back before taking it.
	await nextTick();
	setTimeout(() => {
		editInput?.focus();
		editInput?.select();
	}, 0);
}

function cancelRename() {
	editing.value = "";
}

async function saveRename() {
	const from = editing.value;
	if (!from) return;
	editing.value = "";
	const to = editName.value.trim();
	if (!to || to === from) return;
	try {
		await callTool("channel_rename", {
			agent: props.agent,
			channel: from,
			new_name: to,
		});
		await settings.fetchConfig();
		// Follow the renamed channel if it was showing.
		if (activeName.value === from) {
			const tail = route.path.endsWith("/settings")
				? channelSettingsPath(props.agent, to)
				: channelPath(props.agent, to);
			await router.replace(tail);
		}
	} catch (e) {
		console.error("Failed to rename channel", e);
	}
}
</script>
