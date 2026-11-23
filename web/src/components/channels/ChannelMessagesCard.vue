<template>
	<section data-testid="channels-overview-channel" :data-channel="name"
		class="flex min-w-0 flex-col overflow-hidden rounded-xl border border-gray-200 bg-white dark:border-gray-800 dark:bg-gray-900">
		<router-link :to="channelPath(agent, name)" data-testid="channels-overview-channel-link"
			class="group flex items-center gap-2 border-b border-gray-100 px-4 py-2.5 hover:bg-gray-50 dark:border-gray-800 dark:hover:bg-gray-800/50"
			:title="`Open the ${name} chat`">
			<MessagingLogo v-if="isKnownChannelType(channel.type)" :name="channel.type" :disabled="channel.enabled === false" class="h-4 w-4 shrink-0" />
			<span class="min-w-0 truncate text-sm font-semibold text-gray-900 group-hover:underline dark:text-white">{{ name }}</span>
			<span class="ml-auto inline-flex shrink-0 items-center gap-0.5 text-xs font-medium text-blue-600 dark:text-blue-400">
				Open chat
				<ChevronRightIcon aria-hidden="true" class="h-3.5 w-3.5" />
			</span>
		</router-link>

		<ul v-if="messages.length" class="divide-y divide-gray-100 dark:divide-gray-800">
			<li v-for="(m, i) in messages" :key="`${m.timestamp}-${i}`" data-testid="channels-overview-message"
				class="flex min-w-0 gap-3 px-4 py-2.5">
				<span aria-hidden="true" data-testid="message-avatar"
					class="mt-0.5 inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-full text-xs font-semibold"
					:class="m.sender ? avatarClass(m.sender) : 'bg-gray-100 text-gray-500 dark:bg-gray-800 dark:text-gray-400'">
					{{ initials(m.sender) }}
				</span>
				<div class="min-w-0 flex-1">
					<div class="flex min-w-0 items-baseline gap-2">
						<span class="min-w-0 truncate text-sm font-semibold"
							:class="m.sender ? 'text-gray-900 dark:text-white' : 'italic text-gray-500 dark:text-gray-400'"
							data-testid="message-sender">{{ m.sender || UNKNOWN_SENDER }}</span>
						<span v-if="m.conversation" class="hidden min-w-0 truncate text-xs text-gray-400 sm:inline">in {{ m.conversation }}</span>
						<time :datetime="m.timestamp" :title="fullTime(m.timestamp)" data-testid="message-time"
							class="ml-auto shrink-0 text-xs tabular-nums text-gray-400">{{ ago(m.timestamp, now) }}</time>
					</div>
					<button v-if="m.text" type="button" data-testid="message-text" :aria-expanded="expanded.has(i)"
						:title="expanded.has(i) ? undefined : m.text"
						class="block w-full min-w-0 text-left text-sm text-gray-700 dark:text-gray-300"
						:class="expanded.has(i) ? 'whitespace-pre-wrap break-words' : 'truncate'" @click="toggle(i)">{{ m.text }}</button>
					<div v-if="m.attachments?.length || m.workflows?.length" class="mt-1 flex min-w-0 flex-wrap items-center gap-1.5">
						<span v-for="a in attachmentChips(m.attachments)" :key="a.kind" data-testid="message-attachment"
							class="inline-flex items-center gap-1 rounded-full border border-gray-200 px-2 py-0.5 text-[11px] text-gray-600 dark:border-gray-700 dark:text-gray-300">
							<component :is="a.icon" aria-hidden="true" class="h-3.5 w-3.5" />
							{{ a.label }}
						</span>
						<span v-for="w in m.workflows" :key="w" data-testid="message-workflow" :title="`Handled by workflow ${w}`"
							class="inline-flex min-w-0 items-center gap-1 rounded-full bg-blue-50 px-2 py-0.5 text-[11px] font-medium text-blue-700 dark:bg-blue-950/50 dark:text-blue-300">
							<BoltIcon aria-hidden="true" class="h-3 w-3 shrink-0" />
							<span class="truncate">{{ w }}</span>
						</span>
					</div>
				</div>
			</li>
		</ul>
		<p v-else class="px-4 py-6 text-center text-sm text-gray-500 dark:text-gray-400" data-testid="channels-overview-no-messages">
			{{ loading ? "Loading…" : filtered ? "No messages match." : "No messages yet." }}</p>
	</section>
</template>

<script setup lang="ts">
import BoltIcon from "@heroicons/vue/24/outline/BoltIcon";
import ChevronRightIcon from "@heroicons/vue/24/outline/ChevronRightIcon";
import DocumentIcon from "@heroicons/vue/24/outline/DocumentIcon";
import FilmIcon from "@heroicons/vue/24/outline/FilmIcon";
import MusicalNoteIcon from "@heroicons/vue/24/outline/MusicalNoteIcon";
import PhotoIcon from "@heroicons/vue/24/outline/PhotoIcon";
import { type Component, computed, ref, watch } from "vue";
import {
	channelName,
	channelPath,
	isKnownChannelType,
} from "../../lib/channels";
import type { AgentChannel } from "../../stores/settings";
import MessagingLogo from "../MessagingLogo.vue";
import {
	ago,
	type ChannelMessage,
	fullTime,
	UNKNOWN_SENDER,
} from "./channelActivity";

// A channel's latest messages on the Channels overview, styled like chat
// previews, under a heading that opens the channel's chat. A message's text
// fits one line; clicking it shows the rest.

const props = defineProps<{
	agent: string;
	channel: AgentChannel;
	/** Newest first. */
	messages: ChannelMessage[];
	loading: boolean;
	/** Whether a search hides some of the channel's messages. */
	filtered: boolean;
	now: number;
}>();

const name = computed(() => channelName(props.channel));

const expanded = ref(new Set<number>());
watch(
	() => props.messages,
	() => {
		expanded.value = new Set();
	},
);
function toggle(i: number) {
	const next = new Set(expanded.value);
	if (!next.delete(i)) next.add(i);
	expanded.value = next;
}

function initials(sender?: string): string {
	if (!sender) return "?";
	const words = sender.trim().split(/\s+/);
	return (words[0][0] + (words.length > 1 ? words[words.length - 1][0] : ""))
		.toUpperCase()
		.slice(0, 2);
}

const AVATAR_COLORS = [
	"bg-blue-100 text-blue-700 dark:bg-blue-950 dark:text-blue-300",
	"bg-green-100 text-green-700 dark:bg-green-950 dark:text-green-300",
	"bg-purple-100 text-purple-700 dark:bg-purple-950 dark:text-purple-300",
	"bg-amber-100 text-amber-700 dark:bg-amber-950 dark:text-amber-300",
	"bg-rose-100 text-rose-700 dark:bg-rose-950 dark:text-rose-300",
	"bg-teal-100 text-teal-700 dark:bg-teal-950 dark:text-teal-300",
];

// The same person gets the same color everywhere.
function avatarClass(sender: string): string {
	let h = 0;
	for (const ch of sender) h = (h * 31 + ch.charCodeAt(0)) >>> 0;
	return AVATAR_COLORS[h % AVATAR_COLORS.length];
}

const KINDS: Record<string, { icon: Component; one: string; many: string }> = {
	image: { icon: PhotoIcon, one: "Image", many: "images" },
	video: { icon: FilmIcon, one: "Video", many: "videos" },
	audio: { icon: MusicalNoteIcon, one: "Audio", many: "audio clips" },
	file: { icon: DocumentIcon, one: "File", many: "files" },
};

function attachmentChips(
	kinds: string[] = [],
): { kind: string; icon: Component; label: string }[] {
	const counts = new Map<string, number>();
	for (const k of kinds) {
		const kind = k in KINDS ? k : "file";
		counts.set(kind, (counts.get(kind) ?? 0) + 1);
	}
	return [...counts].map(([kind, n]) => ({
		kind,
		icon: KINDS[kind].icon,
		label: n === 1 ? KINDS[kind].one : `${n} ${KINDS[kind].many}`,
	}));
}
</script>
