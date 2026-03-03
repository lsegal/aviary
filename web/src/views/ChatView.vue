<template>
	<AppLayout>
		<div class="flex h-full flex-col">
			<!-- Agent + Session picker -->
			<div class="flex flex-wrap items-center gap-3 border-b border-gray-200 px-6 py-3 dark:border-gray-800">
				<!-- Agent selector -->
				<div class="flex items-center gap-2">
					<label class="text-xs font-medium text-gray-500 dark:text-gray-400">Agent</label>
					<select v-model="selectedAgent"
						class="rounded-lg border border-gray-300 bg-white px-3 py-1.5 text-sm text-gray-900 dark:border-gray-700 dark:bg-gray-800 dark:text-white"
						@change="onAgentChange">
						<option value="">Select agent…</option>
						<option v-for="a in agentsStore.agents" :key="a.id" :value="a.name">{{ a.name }}</option>
					</select>
				</div>

				<!-- Session selector -->
				<div v-if="selectedAgent" class="flex items-center gap-2">
					<label class="text-xs font-medium text-gray-500 dark:text-gray-400">Session</label>
					<select v-model="selectedSessionId"
						class="rounded-lg border border-gray-300 bg-white px-3 py-1.5 text-sm text-gray-900 dark:border-gray-700 dark:bg-gray-800 dark:text-white"
						@change="onSessionChange">
						<option v-for="s in sessions" :key="s.id" :value="s.id">
							{{ s.name || s.id }}
						</option>
					</select>
					<button
						class="rounded-lg border border-gray-300 px-2.5 py-1.5 text-xs font-medium text-gray-600 hover:bg-gray-100 dark:border-gray-700 dark:text-gray-300 dark:hover:bg-gray-800"
						title="Start a new session" @click="createSession">+ New</button>
				</div>

				<span v-if="sessionsLoading" class="text-xs text-gray-400">Loading sessions…</span>
			</div>

			<!-- Messages -->
			<div class="relative flex-1 overflow-hidden">
				<div ref="messagesEl" class="h-full overflow-y-auto px-6 py-4 space-y-4" @scroll="onMessagesScroll">
					<div v-if="!selectedAgent" class="flex h-full items-center justify-center text-sm text-gray-400">
						Select an agent to start chatting.
					</div>
					<template v-else>
						<div v-for="(msg, i) in messages" :key="i" :class="msg.role === 'user' ? 'text-right' : 'text-left'">
							<div
								:class="msg.role === 'user'
									? 'inline-flex flex-col items-end gap-1 rounded-xl bg-blue-600 px-4 py-2 text-sm text-white max-w-lg'
									: 'inline-flex flex-col items-start gap-1 rounded-xl bg-gray-100 px-4 py-2 text-sm text-gray-900 max-w-2xl dark:bg-gray-800 dark:text-gray-100'">
								<img v-if="msg.mediaURL" :src="msg.mediaURL" class="max-w-full rounded-lg" style="max-height:320px" />
								<span v-if="msg.text && msg.role === 'user'" class="whitespace-pre-wrap">{{ msg.text }}</span>
								<div v-if="msg.text && msg.role === 'assistant'" class="prose prose-sm dark:prose-invert max-w-none"
									v-html="renderMarkdown(msg.text)" />
							</div>
						</div>
						<div v-if="currentSessionProcessing" class="text-left">
							<span
								class="inline-block animate-pulse rounded-xl bg-gray-100 px-4 py-2 text-sm text-gray-400 dark:bg-gray-800">…</span>
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
					<input v-model="input" type="text" :disabled="!selectedAgent || !selectedSessionId"
						placeholder="Type a message or paste an image…"
						class="flex-1 rounded-xl border border-gray-300 bg-white px-4 py-2.5 text-sm text-gray-900 placeholder-gray-400 focus:border-blue-500 focus:outline-none disabled:opacity-50 dark:border-gray-700 dark:bg-gray-800 dark:text-white dark:placeholder-gray-500"
						@paste="onPaste" />
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
	</AppLayout>
</template>

<script setup lang="ts">
import { marked } from "marked";
import { computed, nextTick, onMounted, onUnmounted, ref } from "vue";
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
interface Message {
	role: "user" | "assistant";
	text: string;
	mediaURL?: string;
}

interface PersistedMessage {
	id?: string;
	session_id?: string;
	role: "user" | "assistant" | "system";
	content: string;
	media_url?: string;
	timestamp?: string;
}

const agentsStore = useAgentsStore();
const authStore = useAuthStore();
const { streamAgent } = useStream();

function renderMarkdown(text: string): string {
	return marked.parse(text, { async: false }) as string;
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
const messagesEl = ref<HTMLElement | null>(null);
const isAtBottom = ref(true);
const hasScrollOverflow = ref(false);
let ws: WebSocket | null = null;

const showBelowScroller = computed(() => hasScrollOverflow.value && !isAtBottom.value);
const currentSessionProcessing = computed(() => {
	if (!selectedSessionId.value) return false;
	return sessionProcessing.value[selectedSessionId.value] === true;
});

const onVisible = async () => {
	if (document.visibilityState === "visible") {
		await loadSessionMessages();
	}
};

onMounted(async () => {
	await agentsStore.fetchAgents();
	// Auto-select first agent.
	if (agentsStore.agents.length > 0) {
		selectedAgent.value = agentsStore.agents[0].name;
		await loadSessions();
		await loadSessionMessages();
	}
	document.addEventListener("visibilitychange", onVisible);
	connectSessionWS();
	await nextTick();
	updateScrollState();
});

onUnmounted(() => {
	document.removeEventListener("visibilitychange", onVisible);
	if (ws) {
		ws.close();
		ws = null;
	}
});

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
				if (data.session_id === selectedSessionId.value && data.is_processing === false) {
					await loadSessionMessages();
				}
				return;
			}
			if (data.type !== "session_message") return;
			if (!selectedSessionId.value) return;
			if (data.session_id !== selectedSessionId.value) return;
			await loadSessionMessages();
		} catch {
			// ignore malformed frames
		}
	};
}

async function loadSessions() {
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
		// Default to "main" session.
		const main = sessions.value.find((s) => s.name === "main");
		selectedSessionId.value = main?.id ?? sessions.value[0]?.id ?? "";
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

async function onAgentChange() {
	selectedSessionId.value = "";
	sessions.value = [];
	await loadSessions();
}

async function onSessionChange() {
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
		messages.value = [];
	} catch (e) {
		console.error("Failed to create session", e);
	}
}

async function loadSessionMessages() {
	if (!selectedSessionId.value) {
		messages.value = [];
		updateScrollState();
		return;
	}
	try {
		const raw = await callTool("session_messages", { session_id: selectedSessionId.value });
		const persisted = (JSON.parse(raw) as PersistedMessage[]) ?? [];
		messages.value = persisted
			.filter((m): m is PersistedMessage & { role: "user" | "assistant" } => m.role === "user" || m.role === "assistant")
			.map((m) => ({ role: m.role, text: m.content, mediaURL: m.media_url }));
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

// Map session ID → session name for agent_run.
function selectedSessionName(): string {
	const s = sessions.value.find((s) => s.id === selectedSessionId.value);
	return s?.name || s?.id || "main";
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

async function send() {
	const text = input.value.trim();
	const mediaURL = pastedMedia.value;
	if (!text && !mediaURL) return;
	if (!selectedAgent.value || !selectedSessionId.value) return;
	input.value = "";
	pastedMedia.value = "";
	sessionProcessing.value = {
		...sessionProcessing.value,
		[selectedSessionId.value]: true,
	};
	messages.value.push({ role: "user", text, mediaURL: mediaURL || undefined });
	await scrollBottom(true);

	try {
		let assistantIndex = -1;
		await streamAgent(
			selectedAgent.value,
			text,
			(chunk, isMedia) => {
				if (isMedia && chunk) {
					messages.value.push({ role: "assistant", text: "", mediaURL: chunk });
					scrollBottom();
				} else if (chunk) {
					if (assistantIndex === -1) {
						messages.value.push({ role: "assistant", text: "" });
						assistantIndex = messages.value.length - 1;
					}
					messages.value[assistantIndex].text += chunk;
					scrollBottom();
				}
			},
			selectedSessionName(),
			mediaURL || undefined,
		);
	} catch (e) {
		const msg = e instanceof Error ? e.message : String(e);
		const normalized = msg.toLowerCase();
		if (normalized.includes("stopped") || normalized.includes("canceled") || normalized.includes("cancelled")) {
			await scrollBottom();
			return;
		}
		messages.value.push({
			role: "assistant",
			text: `Error: ${msg}`,
		});
	} finally {
		await refreshSessionProcessing();
	}
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
