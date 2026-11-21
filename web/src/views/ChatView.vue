<template>
	<component :is="props.agent ? 'div' : AppLayout" :class="props.agent ? 'relative h-full min-h-0' : undefined">
		<div class="flex h-full flex-col">
			<!-- Chat picker: the sidebar's Chats list covers wide screens. A pinned
				session is the only chat, so it has none. -->
			<div v-if="selectedAgent && !props.session" data-testid="chat-picker"
				class="items-center gap-2 border-b border-gray-200 bg-gray-50 px-4 py-2 select-none dark:border-gray-800 dark:bg-gray-900/50"
				:class="props.agent ? 'flex' : 'flex lg:hidden'">
				<DropdownMenuRoot>
					<DropdownMenuTrigger asChild>
						<button type="button"
							class="min-w-0 flex-1 truncate rounded-md border border-gray-300 bg-white px-3 py-1.5 text-left text-sm text-gray-900 dark:border-gray-700 dark:bg-gray-800 dark:text-white">
							{{ selectedSessionName }}
						</button>
					</DropdownMenuTrigger>
					<DropdownMenuContent align="start" :side-offset="4"
						class="z-50 max-h-64 overflow-y-auto rounded-lg border border-gray-200 bg-white p-1 shadow-md dark:border-gray-700 dark:bg-gray-900">
						<DropdownMenuItem v-for="s in sessions" :key="s.id" @select="selectSession(s.id)"
							class="cursor-pointer rounded px-3 py-1.5 text-sm transition-colors"
							:class="selectedSessionId === s.id
								? 'font-semibold text-blue-700 bg-blue-50 dark:text-blue-400 dark:bg-blue-950/40'
								: 'text-gray-700 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-gray-800'">
							{{ chatDisplayName(s) }}
						</DropdownMenuItem>
					</DropdownMenuContent>
				</DropdownMenuRoot>
				<button type="button" title="New chat" aria-label="New chat"
					class="shrink-0 rounded px-2 py-1 text-base leading-none text-gray-400 transition-colors hover:text-blue-600 dark:text-gray-500 dark:hover:text-blue-400"
					:disabled="sessionsLoading" @click="createSession">+</button>
				<span v-if="sessionsLoading" class="shrink-0 text-xs text-gray-400">Loading…</span>
			</div>

			<!-- Messages -->
			<div class="relative flex-1 overflow-hidden">
				<div ref="messagesEl" class="h-full overflow-y-auto px-6 py-4 space-y-4" @scroll="onMessagesScroll">
					<div v-if="!selectedAgent" class="flex h-full items-center justify-center text-sm text-gray-400">
						Pick an agent in the sidebar to start chatting.
					</div>
					<template v-else>
						<template v-for="item in displayItems"
							:key="item.type === 'message' ? messageKey(item.msg, item.key) : item.key">
							<!-- Date divider -->
							<div v-if="item.type === 'date-divider'" class="flex justify-center my-4">
								<span class="text-xs font-medium text-gray-400 dark:text-gray-500 select-none">{{ item.label }}</span>
							</div>
							<!-- Tool-use indicator -->
							<div v-else-if="item.type === 'message' && item.msg.role === 'tool'" class="text-left my-0.5">
								<ChatToolCall :name="toolName(item.msg)" :args="item.msg.toolData?.args"
									:result="item.msg.toolData?.result ?? (item.msg.toolData ? undefined : item.msg.text)"
									:error="item.msg.toolData?.error" :pending="!!item.msg.toolPending" />
							</div>
							<!-- Regular user / assistant messages -->
							<div v-else-if="item.type === 'message'" :class="item.msg.role === 'user' ? 'text-right' : 'text-left'">
								<div
									:class="item.msg.role === 'user'
										? 'inline-flex flex-col items-end gap-1 rounded-xl bg-blue-600 px-4 py-2 text-base text-white max-w-lg'
										: item.msg.isError
											? 'inline-flex flex-col items-start gap-1 rounded-xl border border-red-200 bg-red-50 px-4 py-2 text-base text-red-700 max-w-2xl dark:border-red-800 dark:bg-red-950 dark:text-red-300'
											: 'inline-flex flex-col items-start gap-1 rounded-xl bg-gray-100 px-4 py-2 text-base text-gray-900 max-w-2xl dark:bg-gray-800 dark:text-gray-100'">
									<template v-for="(mediaURL, mi) in item.msg.mediaURLs ?? []" :key="mi">
										<button v-if="isImageMedia(mediaURL)" type="button"
											class="cursor-zoom-in" @click="openExpandedImage(mediaURL)">
											<img :src="mediaURL" class="max-w-full rounded-lg" style="max-height:320px" />
										</button>
										<video v-else-if="isVideoMedia(mediaURL)" :src="mediaURL"
											controls class="max-w-full rounded-lg" style="max-height:320px" />
										<audio v-else-if="isAudioMedia(mediaURL)" :src="mediaURL"
											controls class="max-w-full" />
										<a v-else :href="mediaURL" target="_blank" rel="noopener noreferrer"
											:download="attachmentName(mediaURL) || undefined"
											class="text-sm underline underline-offset-2 opacity-90 hover:opacity-100">
											{{ attachmentName(mediaURL) || "Open attachment" }}
										</a>
									</template>
									<span v-if="item.msg.text && item.msg.role === 'user'"
										class="whitespace-pre-wrap">{{ item.msg.text }}</span>
									<span v-if="item.msg.text && item.msg.role === 'assistant' && item.msg.isError"
										class="whitespace-pre-wrap font-mono text-sm">{{ item.msg.text }}</span>
									<div v-if="item.msg.text && item.msg.role === 'assistant' && !item.msg.isError"
										class="prose dark:prose-invert max-w-none" v-html="renderMarkdown(item.msg.text)" />
									<span v-if="item.isLastInGroup && item.msg.timestamp"
										:class="item.msg.role === 'user' ? 'text-xs opacity-60 self-end' : item.msg.isError ? 'text-xs text-red-400 dark:text-red-500 self-end' : 'text-xs text-gray-400 dark:text-gray-500 self-end'">
										{{ formatTime(item.msg.timestamp) }}{{ item.msg.model ? ' | ' + item.msg.model : '' }}
									</span>
								</div>
							</div>
						</template>
						<div v-if="currentSessionProcessing" class="text-left">
							<span
								class="inline-block animate-pulse rounded-xl bg-gray-100 px-4 py-2 text-base text-gray-400 dark:bg-gray-800">…</span>
						</div>
						<div v-if="messages.length === 0 && !currentSessionProcessing"
							class="text-center text-sm text-gray-400 mt-8">
							No messages yet — say something!
						</div>
					</template>
				</div>

				<div v-if="showBelowScroller" class="pointer-events-none absolute inset-x-0 bottom-3 flex justify-center">
					<button type="button"
						class="pointer-events-auto rounded-full border border-gray-300 bg-white px-3 py-1 text-xs font-medium text-gray-700 shadow-sm hover:bg-gray-100 dark:border-gray-700 dark:bg-gray-900 dark:text-gray-200 dark:hover:bg-gray-800"
						@click="scrollBottom(true)">
						More below ↓
					</button>
				</div>
			</div>

			<!-- Input -->
			<div class="sticky bottom-0 z-10 bg-white px-6 pb-4 pt-2 dark:bg-gray-950">
				<ChatComposer v-model="input" v-model:attachments="attachments"
					:disabled="!selectedAgent || !selectedSessionId" :running="currentSessionProcessing"
					:history="userMessageHistory" @submit="send" @stop="stopSession" />
			</div>
		</div>
		<div v-if="expandedImageURL" class="fixed inset-0 z-50 flex items-center justify-center bg-black/80 px-4 py-6"
			@click="closeExpandedImage">
			<button type="button"
				class="absolute right-4 top-4 rounded-full bg-black/50 px-3 py-1 text-sm text-white hover:bg-black/70"
				aria-label="Close image" @click.stop="closeExpandedImage">
				Close
			</button>
			<img :src="expandedImageURL" class="max-h-full max-w-full rounded-xl shadow-2xl" @click.stop />
		</div>

	</component>
</template>

<script setup lang="ts">
import {
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuRoot,
	DropdownMenuTrigger,
} from "radix-vue";
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import AppLayout from "../components/AppLayout.vue";
import {
	attachmentName,
	type ChatAttachment,
} from "../components/chat/attachments";
import ChatComposer from "../components/chat/ChatComposer.vue";
import ChatToolCall from "../components/chat/ChatToolCall.vue";
import { useMCP } from "../composables/useMCP";
import { useStream } from "../composables/useStream";
import { renderMarkdown } from "../lib/markdown";
import { useAgentsStore } from "../stores/agents";
import { useAuthStore } from "../stores/auth";
import {
	chatDisplayName,
	isMainChat,
	useChatSessionsStore,
} from "../stores/chatSessions";

interface ToolData {
	/** Ties a streamed call to its result; the two share one block. */
	id?: string;
	name: string;
	args?: Record<string, unknown>;
	result?: string;
	error?: string;
	/** Set on the streamed result of a call. */
	done?: boolean;
}

interface Message {
	id?: string;
	localKey?: string;
	role: "user" | "assistant" | "tool";
	text: string;
	mediaURLs?: string[];
	toolData?: ToolData;
	/** A streamed tool call that has not reported its result yet. */
	toolPending?: boolean;
	timestamp?: string;
	model?: string;
	isError?: boolean;
}

type DisplayItem =
	| { type: "date-divider"; key: string; label: string }
	| { type: "message"; key: string; msg: Message; isLastInGroup: boolean };

interface PersistedMessage {
	id?: string;
	session_id?: string;
	role: "user" | "assistant" | "system" | "tool";
	content: string;
	media_urls?: string[];
	model?: string;
	timestamp?: string;
}

const props = defineProps<{
	/** Pins the chat to this agent and embeds it without the app layout,
	 * agent tabs, or route changes, as the Projects board's side panel does. */
	agent?: string;
	/** The Projects board the embedded chat was opened from, sent with each
	 * prompt so the agent answers about that project. */
	project?: string;
	/** Pins the embedded chat to this one session, with no chat picker, as
	 * the Projects prompt chat does with its hidden session. */
	session?: string;
}>();

const agentsStore = useAgentsStore();
const chatSessions = useChatSessionsStore();
const authStore = useAuthStore();
const route = useRoute();
const router = useRouter();
const { streamAgent } = useStream();
function chatPath(agent: string, sessionId?: string): string {
	const base = `/agents/${encodeURIComponent(agent)}/chat`;
	return sessionId ? `${base}/${sessionId}` : base;
}

/** Returns true if an assistant message text looks like an error. */
function isErrorMessage(text: string): boolean {
	return text.startsWith("Error:") || text.startsWith("[no LLM provider");
}

/** Parse a "[tool] ..." content string into a Message. */
function parseToolMessage(
	content: string,
	timestamp?: string,
	model?: string,
	id?: string,
): Message {
	const raw = content.slice("[tool] ".length);
	try {
		const d = JSON.parse(raw) as ToolData;
		if (d.name)
			return { id, role: "tool", text: d.name, toolData: d, timestamp, model };
	} catch {
		/* legacy: just a bare name */
	}
	return { id, role: "tool", text: raw, timestamp, model };
}

function parseToolChunk(
	content: string,
	timestamp?: string,
	id?: string,
): Message {
	try {
		const d = JSON.parse(content) as ToolData;
		if (d.name)
			return { id, role: "tool", text: d.name, toolData: d, timestamp };
	} catch {
		// Fall through to a raw tool message.
	}
	return { id, role: "tool", text: content, timestamp };
}

function extractCompleteToolPayload(
	buffer: string,
): { payload: string; rest: string } | null {
	if (!buffer.startsWith("[tool] ")) return null;
	const raw = buffer.slice("[tool] ".length);
	if (!raw) return null;

	const first = raw[0];
	if (first !== "{") {
		const newline = raw.indexOf("\n");
		if (newline === -1) return null;
		return {
			payload: raw.slice(0, newline),
			rest: raw.slice(newline + 1),
		};
	}

	let depth = 0;
	let inString = false;
	let escaped = false;
	for (let i = 0; i < raw.length; i++) {
		const ch = raw[i];
		if (inString) {
			if (escaped) {
				escaped = false;
				continue;
			}
			if (ch === "\\") {
				escaped = true;
				continue;
			}
			if (ch === '"') {
				inString = false;
			}
			continue;
		}
		if (ch === '"') {
			inString = true;
			continue;
		}
		if (ch === "{") {
			depth++;
			continue;
		}
		if (ch === "}") {
			depth--;
			if (depth === 0) {
				return {
					payload: raw.slice(0, i + 1),
					rest: raw.slice(i + 1),
				};
			}
		}
	}

	return null;
}

/** Tool name shown in the collapsed pill; details stay inside the expansion. */
function toolName(msg: Message): string {
	return msg.toolData?.name || msg.text;
}

function formatDateLabel(d: Date): string {
	const now = new Date();
	const today = new Date(now.getFullYear(), now.getMonth(), now.getDate());
	const yesterday = new Date(today.getTime() - 86_400_000);
	const msgDay = new Date(d.getFullYear(), d.getMonth(), d.getDate());
	if (msgDay.getTime() === today.getTime()) return "Today";
	if (msgDay.getTime() === yesterday.getTime()) return "Yesterday";
	const opts: Intl.DateTimeFormatOptions = {
		weekday: "short",
		month: "short",
		day: "numeric",
	};
	if (d.getFullYear() !== now.getFullYear()) opts.year = "numeric";
	return d.toLocaleDateString(undefined, opts);
}

function formatTime(timestamp: string): string {
	return new Date(timestamp).toLocaleTimeString(undefined, {
		hour: "numeric",
		minute: "2-digit",
	});
}

function mediaTypeFromURL(mediaURL: string): string {
	const s = mediaURL.trim().toLowerCase();
	if (s.startsWith("data:")) {
		const end = s.indexOf(";");
		return end > 5 ? s.slice(5, end) : "";
	}
	if (/\.(png|apng|jpg|jpeg|gif|webp|bmp|svg)(\?|#|$)/.test(s))
		return "image/*";
	if (/\.(mp4|webm|ogg|mov|m4v)(\?|#|$)/.test(s)) return "video/*";
	if (/\.(mp3|wav|ogg|m4a|aac|flac)(\?|#|$)/.test(s)) return "audio/*";
	return "";
}

function isImageMedia(mediaURL: string): boolean {
	return mediaTypeFromURL(mediaURL).startsWith("image/");
}

function isVideoMedia(mediaURL: string): boolean {
	return mediaTypeFromURL(mediaURL).startsWith("video/");
}

function isAudioMedia(mediaURL: string): boolean {
	return mediaTypeFromURL(mediaURL).startsWith("audio/");
}

const { callTool } = useMCP();

const selectedAgent = ref("");
const selectedSessionId = ref("");
const selectedSessionName = computed(() => {
	const s = sessions.value.find((x) => x.id === selectedSessionId.value);
	return s ? chatDisplayName(s) : "Select a chat";
});
const sessions = computed(() => chatSessions.sessions(selectedAgent.value));
const sessionsLoading = computed(
	() => chatSessions.loading[selectedAgent.value] === true,
);
const input = ref("");
const attachments = ref<ChatAttachment[]>([]);
const messages = ref<Message[]>([]);
const sessionProcessing = ref<Record<string, boolean>>({});
// Each send streams its own reply, and several can run at once.
const activeStreams = ref(0);
const isStreaming = computed(() => activeStreams.value > 0);
const hasInlineError = ref(false);
const expandedImageURL = ref("");
const messagesEl = ref<HTMLElement | null>(null);
const isAtBottom = ref(true);
const hasScrollOverflow = ref(false);
let ws: WebSocket | null = null;
let nextMessageKey = 0;
let loadMessagesSeq = 0;

const showBelowScroller = computed(
	() => hasScrollOverflow.value && !isAtBottom.value,
);

const displayItems = computed((): DisplayItem[] => {
	const items: DisplayItem[] = [];
	let lastDateKey = "";
	for (let i = 0; i < messages.value.length; i++) {
		const msg = messages.value[i];
		// Insert date divider when the calendar day changes (skip tool messages)
		if (msg.timestamp && msg.role !== "tool") {
			const d = new Date(msg.timestamp);
			const dateKey = d.toDateString();
			if (dateKey !== lastDateKey) {
				lastDateKey = dateKey;
				items.push({
					type: "date-divider",
					key: `date-${i}`,
					label: formatDateLabel(d),
				});
			}
		}
		// A message is last-in-group when the next message has a different role
		const next = messages.value[i + 1];
		const isLastInGroup =
			msg.role !== "tool" && (!next || next.role !== msg.role);
		items.push({ type: "message", key: `msg-${i}`, msg, isLastInGroup });
	}
	return items;
});

function newMessageKey(): string {
	nextMessageKey += 1;
	return `local-${nextMessageKey}`;
}

function messageKey(msg: Message, fallback: string): string {
	return msg.id || msg.localKey || fallback;
}
const currentSessionProcessing = computed(() => {
	if (!selectedSessionId.value) return false;
	return sessionProcessing.value[selectedSessionId.value] === true;
});
const userMessageHistory = computed(() =>
	messages.value
		.filter(
			(message) => message.role === "user" && message.text.trim().length > 0,
		)
		.map((message) => message.text),
);

const onVisible = async () => {
	if (
		document.visibilityState === "visible" &&
		!isStreaming.value &&
		!hasInlineError.value
	) {
		await loadSessionMessages();
	}
};

const onWindowKeydown = (e: KeyboardEvent) => {
	if (e.key === "Escape" && expandedImageURL.value) {
		closeExpandedImage();
	}
};

let mounted: Promise<void> = Promise.resolve();
onMounted(async () => {
	let markMounted = () => {};
	mounted = new Promise((resolve) => {
		markMounted = resolve;
	});
	try {
		await mountChat();
	} finally {
		markMounted();
	}
});

async function mountChat() {
	await agentsStore.fetchAgents();
	if (props.agent) {
		selectedAgent.value = props.agent;
		if (props.session) selectedSessionId.value = props.session;
		else await loadSessions();
		await loadSessionMessages();
	} else {
		await mountRouteChat();
	}
	document.addEventListener("visibilitychange", onVisible);
	window.addEventListener("keydown", onWindowKeydown);
	connectSessionWS();
	await nextTick();
	updateScrollState();
}

async function mountRouteChat() {
	const routeAgent =
		typeof route.params.agent === "string" ? route.params.agent : "";
	const routeSessionId =
		typeof route.params.sessionId === "string" ? route.params.sessionId : "";
	// The sidebar picks the agent: the route names it.
	const agentName =
		agentsStore.agents.find((a) => a.name === routeAgent)?.name ?? "";
	if (agentName) {
		selectedAgent.value = agentName;
		await loadSessions(routeSessionId);
		await loadSessionMessages();
		router.replace(chatPath(agentName, selectedSessionId.value));
	}
}

/** Sends text as if typed, once the chat has loaded its session. */
async function sendText(text: string) {
	await mounted;
	input.value = text;
	await send();
}

defineExpose({ sendText });

/** Keeps the address bar on the selected chat, unless embedded. */
function pushChatPath() {
	if (!props.agent)
		router.push(chatPath(selectedAgent.value, selectedSessionId.value));
}

onUnmounted(() => {
	clearTimeout(sessionsRefreshTimer);
	document.removeEventListener("visibilitychange", onVisible);
	window.removeEventListener("keydown", onWindowKeydown);
	if (ws) {
		ws.close();
		ws = null;
	}
});

function openExpandedImage(mediaURL: string) {
	expandedImageURL.value = mediaURL;
}

function closeExpandedImage() {
	expandedImageURL.value = "";
}

function connectSessionWS() {
	const protocol = location.protocol === "https:" ? "wss:" : "ws:";
	const tok = authStore.getToken();
	const qs = tok ? `?token=${encodeURIComponent(tok)}` : "";
	ws = new WebSocket(`${protocol}//${location.host}/api/ws${qs}`);
	// Events name agents by ID, which is the agent's name.
	const forSelectedAgent = (id?: string) =>
		!!selectedAgent.value &&
		(id === selectedAgent.value || id === `agent_${selectedAgent.value}`);
	ws.onmessage = async (e) => {
		try {
			const data = JSON.parse(e.data as string) as {
				type?: string;
				agent_id?: string;
				session_id?: string;
				is_processing?: boolean;
			};
			if (data.type === "session_processing" && data.session_id) {
				if (!forSelectedAgent(data.agent_id)) return;
				sessionProcessing.value = {
					...sessionProcessing.value,
					[data.session_id]: data.is_processing === true,
				};
				// Don't reload messages while streaming — send() manages its own
				// state and will reload on completion via refreshSessionProcessing.
				if (
					data.session_id === selectedSessionId.value &&
					data.is_processing === false &&
					!isStreaming.value &&
					!hasInlineError.value
				) {
					await loadSessionMessages();
				}
				return;
			}
			if (data.type !== "session_message") return;
			if (!forSelectedAgent(data.agent_id)) return;
			// New messages and titles change the Chats list.
			scheduleSessionsRefresh();
			if (!selectedSessionId.value) return;
			if (data.session_id !== selectedSessionId.value) return;
			// Skip WS-triggered reloads while streaming — ongoing chunks would be
			// lost if messages.value is replaced mid-stream.
			if (!isStreaming.value && !hasInlineError.value) {
				await loadSessionMessages();
			}
		} catch {
			// ignore malformed frames
		}
	};
}

async function loadSessions(preferredSessionId = "") {
	if (!selectedAgent.value) return;
	try {
		const listed = await chatSessions.load(selectedAgent.value);
		const nextProcessing: Record<string, boolean> = {};
		for (const sess of listed) {
			nextProcessing[sess.id] = sess.is_processing === true;
		}
		sessionProcessing.value = nextProcessing;
		// Prefer explicit session ID, then "main", then first.
		const preferred = preferredSessionId
			? listed.find((s) => s.id === preferredSessionId)
			: undefined;
		const main = listed.find(isMainChat);
		selectedSessionId.value = preferred?.id ?? main?.id ?? listed[0]?.id ?? "";
		await loadSessionMessages();
	} catch (e) {
		console.error("Failed to load sessions", e);
		// Don't wipe existing session state on transient errors.
		if (sessions.value.length === 0) {
			selectedSessionId.value = "";
			messages.value = [];
			sessionProcessing.value = {};
		}
	}
}

let sessionsRefreshTimer: ReturnType<typeof setTimeout> | undefined;
/** Reloads the Chats list once a burst of session events settles. */
function scheduleSessionsRefresh() {
	clearTimeout(sessionsRefreshTimer);
	sessionsRefreshTimer = setTimeout(() => {
		void refreshSessionProcessing();
	}, 300);
}

/** Switches to the agent the sidebar navigated to, keeping its session in the URL. */
async function selectAgent(name: string, sessionId = "") {
	if (selectedAgent.value === name) return;
	loadMessagesSeq += 1;
	selectedAgent.value = name;
	selectedSessionId.value = "";
	messages.value = [];
	updateScrollState();
	await loadSessions(sessionId);
	await loadSessionMessages();
	router.replace(chatPath(name, selectedSessionId.value));
}

// The sidebar's Chat links reuse this view, so follow the route's agent.
watch(
	() => route.params.agent,
	async (param) => {
		if (props.agent || typeof param !== "string") return;
		await mounted;
		const name = agentsStore.agents.find((a) => a.name === param)?.name;
		if (name) {
			const sessionId =
				typeof route.params.sessionId === "string"
					? route.params.sessionId
					: "";
			await selectAgent(name, sessionId);
		}
	},
);

async function selectSession(id: string, push = true) {
	if (selectedSessionId.value === id) return;
	loadMessagesSeq += 1;
	selectedSessionId.value = id;
	messages.value = [];
	updateScrollState();
	if (push) pushChatPath();
	await loadSessionMessages();
}

// The sidebar's Chats list links to each chat; a bare chat path is Main Chat.
watch(
	() => route.params.sessionId,
	async (param) => {
		if (props.agent || typeof route.params.agent !== "string") return;
		await mounted;
		if (route.params.agent !== selectedAgent.value) return;
		const id =
			typeof param === "string" && param
				? param
				: (sessions.value.find(isMainChat)?.id ?? "");
		if (!id || id === selectedSessionId.value) return;
		if (!sessions.value.some((s) => s.id === id)) {
			await loadSessions(id);
			return;
		}
		await selectSession(id, false);
	},
);

async function createSession() {
	if (!selectedAgent.value) return;
	try {
		const sess = await chatSessions.create(selectedAgent.value);
		selectedSessionId.value = sess.id;
		pushChatPath();
		loadMessagesSeq += 1;
		messages.value = [];
		updateScrollState();
	} catch (e) {
		console.error("Failed to create session", e);
	}
}

async function loadSessionMessages(force = false) {
	if (isStreaming.value && !force) return;
	if (!selectedSessionId.value) {
		loadMessagesSeq += 1;
		messages.value = [];
		updateScrollState();
		return;
	}
	loadMessagesSeq += 1;
	const requestSeq = loadMessagesSeq;
	const requestSessionId = selectedSessionId.value;
	const requestAgent = selectedAgent.value;
	try {
		const raw = await callTool("session_messages", {
			session_id: requestSessionId,
			agent: requestAgent,
		});
		if (
			requestSeq !== loadMessagesSeq ||
			requestSessionId !== selectedSessionId.value ||
			requestAgent !== selectedAgent.value ||
			(isStreaming.value && !force)
		) {
			return;
		}
		const persisted = (JSON.parse(raw) as PersistedMessage[]) ?? [];
		messages.value = persisted
			.filter(
				(m): m is PersistedMessage & { role: "user" | "assistant" | "tool" } =>
					m.role === "user" || m.role === "assistant" || m.role === "tool",
			)
			.map((m) => {
				if (m.role === "tool") {
					return {
						...parseToolMessage(
							`[tool] ${m.content}`,
							m.timestamp,
							m.model,
							m.id,
						),
					};
				}
				if (m.role === "assistant" && m.content.startsWith("[tool] ")) {
					return parseToolMessage(m.content, m.timestamp, m.model, m.id);
				}
				return {
					id: m.id,
					role: m.role,
					text: m.content,
					mediaURLs: m.media_urls,
					timestamp: m.timestamp,
					model: m.model,
					isError: m.role === "assistant" && isErrorMessage(m.content),
				};
			});
		await scrollBottom(true);
	} catch (e) {
		console.error("Failed to load session messages", e);
		// Don't wipe existing messages on transient errors.
		if (messages.value.length === 0) {
			updateScrollState();
		}
	}
}

function updateScrollState() {
	const el = messagesEl.value;
	if (!el) {
		hasScrollOverflow.value = false;
		isAtBottom.value = true;
		return;
	}
	hasScrollOverflow.value = el.scrollHeight > el.clientHeight + 1;
	const distanceFromBottom = el.scrollHeight - el.scrollTop - el.clientHeight;
	isAtBottom.value = distanceFromBottom <= 8;
}

function onMessagesScroll() {
	updateScrollState();
}

async function scrollBottom(force = false) {
	await nextTick();
	if (!messagesEl.value) return;
	if (!force && !isAtBottom.value) {
		updateScrollState();
		return;
	}
	messagesEl.value.scrollTop = messagesEl.value.scrollHeight;
	updateScrollState();
}

// send never waits for a running reply: each message is added and delivered
// at once, and its reply streams alongside any others still running.
async function send() {
	const text = input.value.trim();
	const mediaURLs = attachments.value.map((a) => a.dataURL);
	if (!text && mediaURLs.length === 0) return;
	if (!selectedAgent.value || !selectedSessionId.value) return;

	const now = new Date().toISOString();
	const agentModel = agentsStore.agents.find(
		(a) => a.name === selectedAgent.value,
	)?.model;

	input.value = "";
	attachments.value = [];
	sessionProcessing.value = {
		...sessionProcessing.value,
		[selectedSessionId.value]: true,
	};
	messages.value.push({
		localKey: newMessageKey(),
		role: "user",
		text,
		mediaURLs: mediaURLs.length ? mediaURLs : undefined,
		timestamp: now,
	});
	// Count the stream before yielding so no reload replaces the new message.
	loadMessagesSeq += 1;
	activeStreams.value += 1;
	let streamError = false;
	hasInlineError.value = false;
	await scrollBottom(true);
	try {
		// Other sends can add messages while this reply streams, so the
		// reply's bubble is found by key rather than by position.
		let assistantKey = "";
		let pendingText = "";
		const appendAssistantText = (chunk: string) => {
			if (!chunk) return;
			let assistantIndex = assistantKey
				? messages.value.findIndex((m) => m.localKey === assistantKey)
				: -1;
			if (assistantIndex === -1) {
				assistantKey = newMessageKey();
				messages.value.push({
					localKey: assistantKey,
					role: "assistant",
					text: "",
					timestamp: now,
					model: agentModel,
				});
				assistantIndex = messages.value.length - 1;
			}
			const current = messages.value[assistantIndex];
			messages.value.splice(assistantIndex, 1, {
				...current,
				text: current.text + chunk,
			});
		};
		const appendToolMessage = (payload: string) => {
			messages.value.push({
				...parseToolMessage(`[tool] ${payload}`, now, agentModel),
				localKey: newMessageKey(),
			});
			assistantKey = "";
		};
		// A call and its result arrive as separate chunks sharing an ID, so
		// the result finishes the call's block instead of adding another.
		const appendToolChunk = (chunk: string) => {
			const msg = parseToolChunk(chunk, now);
			const data = msg.toolData;
			const index = data?.id
				? messages.value.findLastIndex(
						(m) =>
							m.role === "tool" && m.toolPending && m.toolData?.id === data.id,
					)
				: -1;
			if (index !== -1 && data) {
				const current = messages.value[index];
				messages.value.splice(index, 1, {
					...current,
					toolData: { ...current.toolData, ...data },
					toolPending: !data.done,
				});
				return;
			}
			messages.value.push({
				...msg,
				toolPending: !!data && !data.done,
				localKey: newMessageKey(),
			});
			assistantKey = "";
		};
		const flushPendingChunks = () => {
			while (pendingText) {
				const toolIndex = pendingText.indexOf("[tool] ");
				if (toolIndex === -1) {
					appendAssistantText(pendingText);
					pendingText = "";
					return;
				}
				if (toolIndex > 0) {
					appendAssistantText(pendingText.slice(0, toolIndex));
					pendingText = pendingText.slice(toolIndex);
				}
				const parsed = extractCompleteToolPayload(pendingText);
				if (!parsed) return;
				appendToolMessage(parsed.payload);
				pendingText = parsed.rest;
			}
		};
		await streamAgent(
			selectedAgent.value,
			text,
			(chunk, type) => {
				if (type === "media" && chunk) {
					messages.value.push({
						localKey: newMessageKey(),
						role: "assistant",
						text: "",
						mediaURLs: [chunk],
						timestamp: now,
						model: agentModel,
					});
					scrollBottom();
				} else if (type === "tool" && chunk) {
					appendToolChunk(chunk);
					scrollBottom();
				} else if (chunk) {
					pendingText += chunk;
					flushPendingChunks();
					scrollBottom();
				}
			},
			// A pinned session is opened by name, which creates it on first use.
			props.session ? "" : selectedSessionId.value,
			mediaURLs,
			props.project,
			props.session,
		);
		flushPendingChunks();
	} catch (e) {
		const msg = e instanceof Error ? e.message : String(e);
		const normalized = msg.toLowerCase();
		const stopped =
			normalized.includes("stopped") ||
			normalized.includes("canceled") ||
			normalized.includes("cancelled");
		if (!stopped) {
			streamError = true;
			hasInlineError.value = true;
			messages.value.push({
				localKey: newMessageKey(),
				role: "assistant",
				text: `Error: ${msg}`,
				isError: true,
				timestamp: now,
				model: agentModel,
			});
		}
	} finally {
		activeStreams.value -= 1;
	}
	// The last reply to finish reloads the canonical messages; reloading
	// earlier would drop replies still streaming. Skip if we showed an inline
	// error so the error bubble stays visible.
	if (activeStreams.value > 0) return;
	if (!streamError && !hasInlineError.value) {
		await loadSessionMessages(true);
	}
	await refreshSessionProcessing();
	await scrollBottom();
}

async function stopSession() {
	if (!selectedSessionId.value) return;
	const sessionID = selectedSessionId.value;
	try {
		await callTool("session_stop", {
			agent: selectedAgent.value,
			session_id: sessionID,
		});
		sessionProcessing.value = {
			...sessionProcessing.value,
			[sessionID]: false,
		};
		await loadSessionMessages();
	} catch (e) {
		console.error("Failed to stop session", e);
	} finally {
		await refreshSessionProcessing();
	}
}

async function refreshSessionProcessing() {
	if (!selectedAgent.value) return;
	try {
		const listed = await chatSessions.load(selectedAgent.value);
		const next = { ...sessionProcessing.value };
		for (const sess of listed) {
			next[sess.id] = sess.is_processing === true;
		}
		sessionProcessing.value = next;
	} catch {
		// Keep existing processing state on transient errors.
	}
}
</script>
