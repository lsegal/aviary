<template>
	<AppLayout>
		<div class="flex h-full flex-col">
			<!-- Agent tabs -->
			<div class="flex items-end border-b border-gray-200 dark:border-gray-800">
				<div class="scrollbar-none flex flex-1 items-end overflow-x-auto">
					<button
						v-for="a in agentsStore.agents"
						:key="a.id"
						type="button"
						class="-mb-px shrink-0 border-b-2 px-4 py-2.5 text-sm transition-colors"
						:class="selectedAgent === a.name
							? 'border-blue-600 font-semibold text-blue-700 dark:border-blue-400 dark:text-blue-400'
							: 'border-transparent font-medium text-gray-500 hover:border-gray-300 hover:text-gray-800 dark:text-gray-400 dark:hover:border-gray-600 dark:hover:text-gray-200'"
						@click="selectAgent(a.name)">
						{{ a.name }}
					</button>
					<div v-if="!agentsStore.agents.length" class="px-4 py-2.5 text-sm text-gray-400 dark:text-gray-500">No agents configured.</div>
				</div>
			</div>

			<!-- Session subtabs -->
			<div v-if="selectedAgent" class="flex items-end border-b border-gray-200 bg-gray-50 px-4 dark:border-gray-800 dark:bg-gray-900/50">
				<div class="scrollbar-none flex flex-1 items-end overflow-x-auto">
					<button
						v-for="s in sessions"
						:key="s.id"
						type="button"
						class="-mb-px shrink-0 border-b-2 px-3 py-2 text-xs transition-colors"
						:class="selectedSessionId === s.id
							? 'border-blue-600 font-semibold text-blue-700 dark:border-blue-400 dark:text-blue-400'
							: 'border-transparent font-medium text-gray-500 hover:border-gray-300 hover:text-gray-700 dark:text-gray-400 dark:hover:border-gray-600 dark:hover:text-gray-200'"
						@click="selectSession(s.id)">
						{{ s.name || s.id }}
					</button>
					<button
						type="button"
						title="New session"
						class="-mb-px shrink-0 border-b-2 border-transparent px-2.5 py-2 text-base leading-none text-gray-400 transition-colors hover:text-blue-600 dark:text-gray-500 dark:hover:text-blue-400"
						:disabled="sessionsLoading"
						@click="createSession">+</button>
				</div>
				<span v-if="sessionsLoading" class="shrink-0 pb-2 text-xs text-gray-400">Loading…</span>
			</div>

			<!-- Messages -->
			<div class="relative flex-1 overflow-hidden">
				<div ref="messagesEl" class="h-full overflow-y-auto px-6 py-4 space-y-4" @scroll="onMessagesScroll">
					<div v-if="!selectedAgent" class="flex h-full items-center justify-center text-sm text-gray-400">
						Select an agent to start chatting.
					</div>
					<template v-else>
						<template v-for="item in displayItems"
							:key="item.type === 'message' ? (item.msg.id || item.key) : item.key">
							<!-- Date divider -->
							<div v-if="item.type === 'date-divider'" class="flex justify-center my-4">
								<span class="text-xs font-medium text-gray-400 dark:text-gray-500 select-none">{{ item.label }}</span>
							</div>
							<!-- Tool-use indicator -->
							<div v-else-if="item.type === 'message' && item.msg.role === 'tool'" class="text-left my-0.5">
								<details class="group inline-block">
									<summary
										class="inline-flex cursor-pointer list-none items-center gap-1.5 rounded-full border border-gray-200 bg-gray-50 px-2.5 py-1 text-xs text-gray-500 hover:border-gray-300 hover:bg-gray-100 dark:border-gray-700 dark:bg-gray-900 dark:text-gray-400 dark:hover:border-gray-600 dark:hover:bg-gray-800">
										<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16" fill="currentColor"
											class="h-3 w-3 shrink-0" aria-hidden="true">
											<path fill-rule="evenodd"
												d="M5.433 2.304A4.492 4.492 0 0 0 3.5 6c0 1.92 1.207 3.563 2.912 4.205l-1.69 3.668-.776-.776a.75.75 0 0 0-1.06 1.06l2 2a.75.75 0 0 0 1.172-.196l2-4.34A4.492 4.492 0 0 0 8 12.5c.578 0 1.131-.109 1.64-.307l2 4.34a.75.75 0 0 0 1.172.196l2-2a.75.75 0 1 0-1.06-1.06l-.777.776-1.69-3.668A4.5 4.5 0 1 0 5.433 2.304Zm3.388 6.787A3 3 0 1 1 8 3a3 3 0 0 1 .821 6.091Z"
												clip-rule="evenodd" />
										</svg>
										<span>{{ toolSummary(item.msg) }}</span>
										<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16" fill="currentColor"
											class="h-2.5 w-2.5 shrink-0 transition-transform group-open:rotate-180" aria-hidden="true">
											<path fill-rule="evenodd"
												d="M4.22 6.22a.75.75 0 0 1 1.06 0L8 8.94l2.72-2.72a.75.75 0 1 1 1.06 1.06l-3.25 3.25a.75.75 0 0 1-1.06 0L4.22 7.28a.75.75 0 0 1 0-1.06Z"
												clip-rule="evenodd" />
										</svg>
									</summary>
									<div
										class="mt-1.5 rounded-lg border border-gray-200 bg-gray-50 p-3 text-xs dark:border-gray-700 dark:bg-gray-900">
										<template v-if="item.msg.toolData">
											<div v-if="item.msg.toolData.args && Object.keys(item.msg.toolData.args).length > 0">
												<p class="mb-1 font-semibold uppercase tracking-wide text-gray-400 dark:text-gray-500">Arguments
												</p>
												<pre
													class="max-h-40 overflow-auto whitespace-pre-wrap break-all text-gray-700 dark:text-gray-300">{{ formatJSON(item.msg.toolData.args) }}
												</pre>
											</div>
											<div v-if="item.msg.toolData.result"
												:class="item.msg.toolData.args && Object.keys(item.msg.toolData.args).length ? 'mt-2' : ''">
												<p class="mb-1 font-semibold uppercase tracking-wide text-gray-400 dark:text-gray-500">Result
												</p>
												<pre
													class="max-h-48 overflow-auto whitespace-pre-wrap break-all text-gray-700 dark:text-gray-300">{{ item.msg.toolData.result }}
												</pre>
											</div>
											<div v-if="item.msg.toolData.error"
												:class="item.msg.toolData.args && Object.keys(item.msg.toolData.args).length ? 'mt-2' : ''">
												<p class="mb-1 font-semibold uppercase tracking-wide text-red-400">Error</p>
												<pre
													class="whitespace-pre-wrap break-all text-red-600 dark:text-red-400">{{ item.msg.toolData.error }}
												</pre>
											</div>
											<p v-if="!item.msg.toolData.result && !item.msg.toolData.error"
												class="italic text-gray-400 dark:text-gray-500">Running…</p>
										</template>
										<p v-else class="text-gray-500 dark:text-gray-400">{{ item.msg.text }}</p>
									</div>
								</details>
							</div>
							<!-- Regular user / assistant messages -->
							<div v-else-if="item.type === 'message'" :class="item.msg.role === 'user' ? 'text-right' : 'text-left'">
								<div
									:class="item.msg.role === 'user'
										? 'inline-flex flex-col items-end gap-1 rounded-xl bg-blue-600 px-4 py-2 text-base text-white max-w-lg'
										: item.msg.isError
											? 'inline-flex flex-col items-start gap-1 rounded-xl border border-red-200 bg-red-50 px-4 py-2 text-base text-red-700 max-w-2xl dark:border-red-800 dark:bg-red-950 dark:text-red-300'
											: 'inline-flex flex-col items-start gap-1 rounded-xl bg-gray-100 px-4 py-2 text-base text-gray-900 max-w-2xl dark:bg-gray-800 dark:text-gray-100'">
									<button v-if="item.msg.mediaURL && isImageMedia(item.msg.mediaURL)" type="button"
										class="cursor-zoom-in" @click="openExpandedImage(item.msg.mediaURL)">
										<img :src="item.msg.mediaURL" class="max-w-full rounded-lg" style="max-height:320px" />
									</button>
									<video v-else-if="item.msg.mediaURL && isVideoMedia(item.msg.mediaURL)" :src="item.msg.mediaURL"
										controls class="max-w-full rounded-lg" style="max-height:320px" />
									<audio v-else-if="item.msg.mediaURL && isAudioMedia(item.msg.mediaURL)" :src="item.msg.mediaURL"
										controls class="max-w-full" />
									<a v-else-if="item.msg.mediaURL" :href="item.msg.mediaURL" target="_blank" rel="noopener noreferrer"
										class="text-sm underline underline-offset-2 opacity-90 hover:opacity-100">
										Open attachment
									</a>
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
			<div class="sticky bottom-0 z-10 border-t border-gray-200 bg-white dark:border-gray-800 dark:bg-gray-950">
				<!-- Pending image preview -->
				<div v-if="pastedMedia" class="flex items-center gap-2 px-6 pt-3">
					<div class="relative inline-block">
						<img :src="pastedMedia" class="h-20 w-auto rounded-lg border border-gray-300 dark:border-gray-700" />
						<button type="button"
							class="absolute -right-2 -top-2 flex h-5 w-5 items-center justify-center rounded-full bg-gray-700 text-white text-xs hover:bg-gray-600"
							@click="pastedMedia = ''" aria-label="Remove image">✕</button>
					</div>
				</div>
				<form class="flex gap-3 px-6 py-4" @submit.prevent="send">
					<input ref="chatInputEl" v-model="input" type="text" :disabled="!selectedAgent || !selectedSessionId"
						placeholder="Type a message or paste an image…"
						class="flex-1 rounded-xl border border-gray-300 bg-white px-4 py-2.5 text-sm text-gray-900 placeholder-gray-400 focus:border-blue-500 focus:outline-none disabled:opacity-50 dark:border-gray-700 dark:bg-gray-800 dark:text-white dark:placeholder-gray-500"
						@input="onChatInput" @keydown="onChatInputKeydown" @paste="onPaste" />
					<button v-if="currentSessionProcessing" type="button" @click="stopSession" :disabled="!selectedSessionId"
						class="inline-flex h-11 w-11 items-center justify-center rounded-xl bg-red-600 text-white hover:bg-red-500 disabled:opacity-40"
						aria-label="Stop response" title="Stop">
						<svg viewBox="0 0 24 24" fill="currentColor" class="h-5 w-5" aria-hidden="true">
							<path fill-rule="evenodd"
								d="M2.25 12a9.75 9.75 0 1 1 19.5 0 9.75 9.75 0 0 1-19.5 0ZM9 9.75A.75.75 0 0 1 9.75 9h4.5a.75.75 0 0 1 .75.75v4.5a.75.75 0 0 1-.75.75h-4.5a.75.75 0 0 1-.75-.75v-4.5Z"
								clip-rule="evenodd" />
						</svg>
					</button>
					<button type="submit" :disabled="(!input.trim() && !pastedMedia) || !selectedAgent || !selectedSessionId"
						class="inline-flex h-11 w-11 items-center justify-center rounded-xl bg-blue-600 text-white hover:bg-blue-500 disabled:opacity-40"
						aria-label="Send message" title="Send">
						<svg viewBox="0 0 24 24" fill="currentColor" class="h-5 w-5" aria-hidden="true">
							<path
								d="M3.478 2.559a.75.75 0 0 0-.926.93l2.432 7.917H13.5a.75.75 0 0 1 0 1.5H4.984l-2.432 7.917a.75.75 0 0 0 .926.93 60.872 60.872 0 0 0 18.09-8.153.75.75 0 0 0 0-1.2A60.872 60.872 0 0 0 3.478 2.56Z" />
						</svg>
					</button>
				</form>
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

	</AppLayout>
</template>

<script setup lang="ts">
import { marked } from "marked";
import { computed, nextTick, onMounted, onUnmounted, ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import AppLayout from "../components/AppLayout.vue";
import { useMCP } from "../composables/useMCP";
import { useStream } from "../composables/useStream";
import { useAgentsStore } from "../stores/agents";
import { useAuthStore } from "../stores/auth";

interface Session {
	id: string;
	agent_id: string;
	name: string;
	created_at: string;
	is_processing?: boolean;
}

interface ToolData {
	name: string;
	args?: Record<string, unknown>;
	result?: string;
	error?: string;
}

interface Message {
	id?: string;
	role: "user" | "assistant" | "tool";
	text: string;
	mediaURL?: string;
	toolData?: ToolData;
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
	media_url?: string;
	model?: string;
	timestamp?: string;
}

const agentsStore = useAgentsStore();
const authStore = useAuthStore();
const route = useRoute();
const router = useRouter();
const { streamAgent } = useStream();

function chatPath(agent: string, sessionId?: string): string {
	return sessionId ? `/chat/${agent}/${sessionId}` : `/chat/${agent}`;
}

function renderMarkdown(text: string): string {
	return marked.parse(text, { async: false }) as string;
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

/** Condensed one-line summary shown in the pill. */
function toolSummary(msg: Message): string {
	const d = msg.toolData;
	if (!d) return msg.text;
	const entries = Object.entries(d.args ?? {});
	if (entries.length === 0) return d.name;
	const parts = entries.slice(0, 2).map(([k, v]) => {
		const s = typeof v === "string" ? v : JSON.stringify(v);
		return `${k}=${s.length > 32 ? `${s.slice(0, 32)}…` : s}`;
	});
	if (entries.length > 2) parts.push("…");
	return `${d.name}(${parts.join(", ")})`;
}

function formatJSON(v: unknown): string {
	return JSON.stringify(v, null, 2);
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
const sessions = ref<Session[]>([]);
const sessionsLoading = ref(false);
const input = ref("");
const pastedMedia = ref(""); // base64 data URL of a pasted/dropped image
const messages = ref<Message[]>([]);
const sessionProcessing = ref<Record<string, boolean>>({});
const isStreaming = ref(false);
const hasInlineError = ref(false);
const expandedImageURL = ref("");
const chatInputEl = ref<HTMLInputElement | null>(null);
const messagesEl = ref<HTMLElement | null>(null);
const isAtBottom = ref(true);
const hasScrollOverflow = ref(false);
const historyIndex = ref(-1);
const historyDraft = ref("");
let ws: WebSocket | null = null;

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
	if (document.visibilityState === "visible" && !hasInlineError.value) {
		await loadSessionMessages();
	}
};

const onWindowKeydown = (e: KeyboardEvent) => {
	if (e.key === "Escape" && expandedImageURL.value) {
		closeExpandedImage();
	}
};

onMounted(async () => {
	await agentsStore.fetchAgents();
	const routeAgent =
		typeof route.params.agent === "string" ? route.params.agent : "";
	const routeSessionId =
		typeof route.params.sessionId === "string" ? route.params.sessionId : "";
	const agentName =
		agentsStore.agents.find((a) => a.name === routeAgent)?.name ??
		agentsStore.agents[0]?.name ??
		"";
	if (agentName) {
		selectedAgent.value = agentName;
		await loadSessions(routeSessionId);
		await loadSessionMessages();
		router.replace(chatPath(agentName, selectedSessionId.value));
	}
	document.addEventListener("visibilitychange", onVisible);
	window.addEventListener("keydown", onWindowKeydown);
	connectSessionWS();
	await nextTick();
	updateScrollState();
});

onUnmounted(() => {
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
	ws.onmessage = async (e) => {
		try {
			const data = JSON.parse(e.data as string) as {
				type?: string;
				session_id?: string;
				is_processing?: boolean;
			};
			if (data.type === "session_processing" && data.session_id) {
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
	sessionsLoading.value = true;
	try {
		const raw = await callTool("session_list", { agent: selectedAgent.value });
		sessions.value = (JSON.parse(raw) as Session[]) ?? [];
		const nextProcessing: Record<string, boolean> = {};
		for (const sess of sessions.value) {
			nextProcessing[sess.id] = sess.is_processing === true;
		}
		sessionProcessing.value = nextProcessing;
		// Prefer explicit session ID, then "main", then first.
		const preferred = preferredSessionId
			? sessions.value.find((s) => s.id === preferredSessionId)
			: undefined;
		const main = sessions.value.find((s) => s.name === "main");
		selectedSessionId.value =
			preferred?.id ?? main?.id ?? sessions.value[0]?.id ?? "";
		await loadSessionMessages();
	} catch (e) {
		console.error("Failed to load sessions", e);
		// Don't wipe existing session state on transient errors.
		if (sessions.value.length === 0) {
			selectedSessionId.value = "";
			messages.value = [];
			sessionProcessing.value = {};
		}
	} finally {
		sessionsLoading.value = false;
	}
}

async function selectAgent(name: string) {
	if (selectedAgent.value === name) return;
	selectedAgent.value = name;
	selectedSessionId.value = "";
	sessions.value = [];
	resetHistoryNavigation();
	await loadSessions();
	router.push(chatPath(name, selectedSessionId.value));
}

async function selectSession(id: string) {
	if (selectedSessionId.value === id) return;
	selectedSessionId.value = id;
	resetHistoryNavigation();
	router.push(chatPath(selectedAgent.value, id));
	await loadSessionMessages();
}

async function createSession() {
	if (!selectedAgent.value) return;
	try {
		const raw = await callTool("session_create", {
			agent: selectedAgent.value,
		});
		const sess = JSON.parse(raw) as Session;
		sessions.value.push(sess);
		selectedSessionId.value = sess.id;
		router.push(chatPath(selectedAgent.value, sess.id));
		messages.value = [];
		resetHistoryNavigation();
	} catch (e) {
		console.error("Failed to create session", e);
	}
}

async function loadSessionMessages() {
	if (!selectedSessionId.value) {
		messages.value = [];
		resetHistoryNavigation();
		updateScrollState();
		return;
	}
	try {
		const raw = await callTool("session_messages", {
			session_id: selectedSessionId.value,
		});
		const persisted = (JSON.parse(raw) as PersistedMessage[]) ?? [];
		messages.value = persisted
			.filter(
				(m): m is PersistedMessage & { role: "user" | "assistant" | "tool" } =>
					m.role === "user" || m.role === "assistant" || m.role === "tool",
			)
			.map((m) => {
				if (m.role === "tool") {
					return parseToolMessage(
						`[tool] ${m.content}`,
						m.timestamp,
						m.model,
						m.id,
					);
				}
				if (m.role === "assistant" && m.content.startsWith("[tool] ")) {
					return parseToolMessage(m.content, m.timestamp, m.model, m.id);
				}
				return {
					id: m.id,
					role: m.role,
					text: m.content,
					mediaURL: m.media_url,
					timestamp: m.timestamp,
					model: m.model,
					isError: m.role === "assistant" && isErrorMessage(m.content),
				};
			});
		resetHistoryNavigation();
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

function onPaste(e: ClipboardEvent) {
	const items = e.clipboardData?.items;
	if (!items) return;
	for (const item of items) {
		if (item.type.startsWith("image/")) {
			e.preventDefault();
			const file = item.getAsFile();
			if (!file) continue;
			const reader = new FileReader();
			reader.onload = (ev) => {
				pastedMedia.value = ev.target?.result as string;
			};
			reader.readAsDataURL(file);
			break;
		}
	}
}

function resetHistoryNavigation() {
	historyIndex.value = -1;
	historyDraft.value = "";
}

function onChatInput(e: Event) {
	if (historyIndex.value === -1) return;
	input.value = (e.target as HTMLInputElement).value;
	historyDraft.value = input.value;
	historyIndex.value = -1;
}

function setChatInputValue(value: string) {
	input.value = value;
	void nextTick(() => {
		const el = chatInputEl.value;
		if (!el) return;
		const end = value.length;
		el.setSelectionRange(end, end);
	});
}

function onChatInputKeydown(e: KeyboardEvent) {
	if (e.key !== "ArrowUp" && e.key !== "ArrowDown") return;
	if (e.altKey || e.ctrlKey || e.metaKey) return;
	if (!selectedAgent.value || !selectedSessionId.value) return;

	const history = userMessageHistory.value;
	if (history.length === 0) return;

	if (e.key === "ArrowUp") {
		e.preventDefault();
		if (historyIndex.value === -1) {
			historyDraft.value = input.value;
		}
		const nextIndex = Math.min(historyIndex.value + 1, history.length - 1);
		historyIndex.value = nextIndex;
		setChatInputValue(history[history.length - 1 - nextIndex]);
		return;
	}

	if (historyIndex.value === -1) return;
	e.preventDefault();
	const nextIndex = historyIndex.value - 1;
	if (nextIndex < 0) {
		const draft = historyDraft.value;
		resetHistoryNavigation();
		setChatInputValue(draft);
		return;
	}
	historyIndex.value = nextIndex;
	setChatInputValue(history[history.length - 1 - nextIndex]);
}

async function send() {
	const text = input.value.trim();
	const mediaURL = pastedMedia.value;
	if (!text && !mediaURL) return;
	if (!selectedAgent.value || !selectedSessionId.value) return;

	const now = new Date().toISOString();
	const agentModel = agentsStore.agents.find(
		(a) => a.name === selectedAgent.value,
	)?.model;

	input.value = "";
	pastedMedia.value = "";
	resetHistoryNavigation();
	sessionProcessing.value = {
		...sessionProcessing.value,
		[selectedSessionId.value]: true,
	};
	messages.value.push({
		role: "user",
		text,
		mediaURL: mediaURL || undefined,
		timestamp: now,
	});
	await scrollBottom(true);

	isStreaming.value = true;
	let streamError = false;
	hasInlineError.value = false;
	try {
		let assistantIndex = -1;
		let pendingText = "";
		const appendAssistantText = (chunk: string) => {
			if (!chunk) return;
			if (assistantIndex === -1) {
				messages.value.push({
					role: "assistant",
					text: "",
					timestamp: now,
					model: agentModel,
				});
				assistantIndex = messages.value.length - 1;
			}
			messages.value[assistantIndex].text += chunk;
		};
		const appendToolMessage = (payload: string) => {
			messages.value.push(
				parseToolMessage(`[tool] ${payload}`, now, agentModel),
			);
			assistantIndex = -1;
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
						role: "assistant",
						text: "",
						mediaURL: chunk,
						timestamp: now,
						model: agentModel,
					});
					scrollBottom();
				} else if (type === "tool" && chunk) {
					messages.value.push(parseToolChunk(chunk, now));
					scrollBottom();
				} else if (chunk) {
					pendingText += chunk;
					flushPendingChunks();
					scrollBottom();
				}
			},
			selectedSessionId.value,
			mediaURL || undefined,
		);
		flushPendingChunks();
	} catch (e) {
		const msg = e instanceof Error ? e.message : String(e);
		const normalized = msg.toLowerCase();
		if (
			normalized.includes("stopped") ||
			normalized.includes("canceled") ||
			normalized.includes("cancelled")
		) {
			isStreaming.value = false;
			await loadSessionMessages();
			await refreshSessionProcessing();
			await scrollBottom();
			return;
		}
		streamError = true;
		hasInlineError.value = true;
		messages.value.push({
			role: "assistant",
			text: `Error: ${msg}`,
			isError: true,
			timestamp: now,
			model: agentModel,
		});
	} finally {
		isStreaming.value = false;
	}
	// Reload canonical messages from server after streaming completes.
	// Skip if we showed an inline error so the error bubble stays visible.
	if (!streamError) {
		await loadSessionMessages();
	}
	await refreshSessionProcessing();
	await scrollBottom();
}

async function stopSession() {
	if (!selectedSessionId.value) return;
	const sessionID = selectedSessionId.value;
	try {
		await callTool("session_stop", { session_id: sessionID });
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
		const raw = await callTool("session_list", { agent: selectedAgent.value });
		const listed = (JSON.parse(raw) as Session[]) ?? [];
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
