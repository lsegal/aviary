<template>
	<div class="space-y-0.5 pl-2" data-testid="sidebar-chats">
		<div v-for="s in shown" :key="s.id" data-testid="chat-row" :data-session="s.id"
			class="group relative flex items-center rounded-lg transition-colors"
			:class="s.id === activeId
				? 'bg-gray-100 dark:bg-gray-800'
				: 'hover:bg-gray-100 dark:hover:bg-gray-800'">
			<input v-if="editingId === s.id" ref="editInput" v-model="editName" data-testid="chat-rename-input"
				:aria-label="`Rename ${chatDisplayName(s)}`"
				class="m-0.5 min-w-0 flex-1 rounded-md border border-blue-500 bg-white px-2.5 py-1.5 text-sm text-gray-900 focus:outline-none dark:bg-gray-900 dark:text-white"
				@keydown.enter.prevent="saveRename" @keydown.esc.prevent="cancelRename" @blur="saveRename" />
			<template v-else>
				<router-link :to="chatPath(agent, s.id)" :title="chatDisplayName(s)"
					class="min-w-0 flex-1 truncate py-2 pl-3 pr-1 text-sm"
					:class="s.id === activeId
						? 'font-semibold text-gray-950 dark:text-white'
						: 'text-gray-500 group-hover:text-gray-900 dark:text-gray-400 dark:group-hover:text-white'"
					@dblclick.prevent="startRename(s)">
					{{ chatDisplayName(s) }}
				</router-link>
				<span v-if="s.last_message_at" data-testid="chat-last-message"
					:title="new Date(s.last_message_at).toLocaleString()"
					class="hidden shrink-0 pr-1 text-xs text-gray-400 group-hover:inline group-focus-within:inline">
					{{ age(s.last_message_at, now) }}
				</span>
				<DropdownMenuRoot :open="menuId === s.id" @update:open="(open) => (menuId = open ? s.id : '')">
					<DropdownMenuTrigger asChild>
						<button type="button" :aria-label="`More options for ${chatDisplayName(s)}`"
							data-testid="chat-menu-button"
							class="mr-1 inline-flex h-6 w-6 shrink-0 items-center justify-center rounded-md text-gray-500 transition hover:bg-gray-200 hover:text-gray-900 focus:opacity-100 group-hover:opacity-100 group-focus-within:opacity-100 dark:text-gray-400 dark:hover:bg-gray-700 dark:hover:text-white"
							:class="menuId === s.id ? 'opacity-100' : 'opacity-0'">
							<svg viewBox="0 0 16 16" fill="currentColor" aria-hidden="true" class="h-4 w-4">
								<circle cx="3" cy="8" r="1.4" /><circle cx="8" cy="8" r="1.4" /><circle cx="13" cy="8" r="1.4" />
							</svg>
						</button>
					</DropdownMenuTrigger>
					<DropdownMenuContent align="start" :side-offset="4"
						class="z-50 min-w-36 rounded-xl border border-gray-200 bg-white p-1 shadow-lg dark:border-gray-700 dark:bg-gray-900">
						<DropdownMenuItem
							class="flex cursor-pointer items-center gap-2 rounded-lg px-3 py-2 text-sm text-gray-700 outline-none data-[highlighted]:bg-gray-100 dark:text-gray-200 dark:data-[highlighted]:bg-gray-800"
							@select="startRename(s)">
							<PencilIcon aria-hidden="true" class="h-4 w-4" />
							Rename
						</DropdownMenuItem>
						<DropdownMenuItem v-if="!isMainChat(s)"
							class="flex cursor-pointer items-center gap-2 rounded-lg px-3 py-2 text-sm text-red-600 outline-none data-[highlighted]:bg-red-50 dark:text-red-400 dark:data-[highlighted]:bg-red-950/40"
							@select="askDelete(s)">
							<TrashIcon aria-hidden="true" class="h-4 w-4" />
							Delete
						</DropdownMenuItem>
					</DropdownMenuContent>
				</DropdownMenuRoot>
			</template>
		</div>
		<button v-if="ordered.length > visibleLimit" type="button" data-testid="sidebar-chats-more"
			:aria-expanded="showAll"
			class="block w-full rounded-lg px-3 py-2 text-left text-sm text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-900 dark:text-gray-400 dark:hover:bg-gray-800 dark:hover:text-white"
			@click="showAll = !showAll">
			{{ showAll ? "Less" : "More" }}
		</button>
		<button type="button" data-testid="sidebar-new-chat"
			class="block w-full rounded-lg px-3 py-2 text-left text-sm text-blue-600 transition-colors hover:bg-gray-100 disabled:opacity-50 dark:text-blue-400 dark:hover:bg-gray-800"
			:disabled="creating" @click="newChat">
			+ New chat
		</button>

		<AlertDialogRoot :open="deleteOpen" @update:open="(open) => (deleteOpen = open)">
			<AlertDialogPortal>
				<AlertDialogOverlay class="fixed inset-0 z-50 bg-black/50" />
				<AlertDialogContent
					class="fixed left-1/2 top-1/2 z-50 w-full max-w-lg -translate-x-1/2 -translate-y-1/2 rounded-xl bg-white p-6 shadow-2xl dark:bg-gray-900">
					<AlertDialogTitle class="text-base font-bold text-gray-900 dark:text-white">
						Delete chat?
					</AlertDialogTitle>
					<AlertDialogDescription class="mt-2 text-sm text-gray-600 dark:text-gray-400">
						This will permanently delete
						<span class="break-all font-medium text-gray-900 dark:text-white">{{ deleteTarget ? chatDisplayName(deleteTarget) : "" }}</span>
						and all its messages. This cannot be undone.
					</AlertDialogDescription>
					<div class="mt-6 flex justify-end gap-3">
						<AlertDialogCancel
							class="rounded-lg border border-gray-200 px-4 py-2 text-sm text-gray-600 hover:bg-gray-50 dark:border-gray-700 dark:text-gray-400 dark:hover:bg-gray-800">
							Cancel
						</AlertDialogCancel>
						<AlertDialogAction
							class="rounded-lg bg-red-600 px-4 py-2 text-sm font-semibold text-white hover:bg-red-500"
							@click="confirmDelete">
							Delete
						</AlertDialogAction>
					</div>
				</AlertDialogContent>
			</AlertDialogPortal>
		</AlertDialogRoot>
	</div>
</template>

<script setup lang="ts">
import PencilIcon from "@heroicons/vue/24/outline/PencilIcon";
import TrashIcon from "@heroicons/vue/24/outline/TrashIcon";
import {
	AlertDialogAction,
	AlertDialogCancel,
	AlertDialogContent,
	AlertDialogDescription,
	AlertDialogOverlay,
	AlertDialogPortal,
	AlertDialogRoot,
	AlertDialogTitle,
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuRoot,
	DropdownMenuTrigger,
} from "radix-vue";
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { age } from "../../lib/projects";
import { useAuthStore } from "../../stores/auth";
import {
	type ChatSession,
	chatDisplayName,
	isMainChat,
	useChatSessionsStore,
} from "../../stores/chatSessions";

const props = defineProps<{ agent: string }>();

const store = useChatSessionsStore();
const auth = useAuthStore();
const route = useRoute();
const router = useRouter();

const sessions = computed(() => store.sessions(props.agent));
const activeId = computed(() => {
	// The list shows on every page; only the agent's chat pages select a chat.
	if (
		route.path.replace(/\/$/, "") !== chatPath(props.agent, "") &&
		!route.path.startsWith(`${chatPath(props.agent, "")}/`)
	)
		return "";
	if (typeof route.params.sessionId === "string") return route.params.sessionId;
	return sessions.value.find(isMainChat)?.id ?? "";
});

/** How many chats the list shows before More. */
const visibleLimit = 5;
const showAll = ref(false);

const lastActive = (s: ChatSession) =>
	Date.parse(s.last_message_at || s.created_at) || 0;

// Main Chat leads; the rest follow, most recent message first.
const ordered = computed(() =>
	[...sessions.value].sort(
		(a, b) =>
			Number(isMainChat(b)) - Number(isMainChat(a)) ||
			lastActive(b) - lastActive(a),
	),
);
const shown = computed(() =>
	showAll.value ? ordered.value : ordered.value.slice(0, visibleLimit),
);

const now = ref(Date.now());
let clock: ReturnType<typeof setInterval> | undefined;
let ws: WebSocket | null = null;
let refreshTimer: ReturnType<typeof setTimeout> | undefined;

function reload() {
	store
		.load(props.agent)
		.catch((e) => console.error("Failed to load chats", e));
}

// New messages and generated titles change the list, on any page.
function watchSessions() {
	const protocol = location.protocol === "https:" ? "wss:" : "ws:";
	const tok = auth.getToken();
	const qs = tok ? `?token=${encodeURIComponent(tok)}` : "";
	ws = new WebSocket(`${protocol}//${location.host}/api/ws${qs}`);
	ws.onmessage = (e) => {
		try {
			const data = JSON.parse(e.data as string) as {
				type?: string;
				agent_id?: string;
			};
			if (data.type !== "session_message") return;
			if (
				data.agent_id !== props.agent &&
				data.agent_id !== `agent_${props.agent}`
			)
				return;
			clearTimeout(refreshTimer);
			refreshTimer = setTimeout(reload, 300);
		} catch {
			// ignore malformed frames
		}
	};
}

watch(() => props.agent, reload);

onMounted(() => {
	clock = setInterval(() => {
		now.value = Date.now();
	}, 30_000);
	reload();
	watchSessions();
});
onUnmounted(() => {
	clearInterval(clock);
	clearTimeout(refreshTimer);
	ws?.close();
	ws = null;
});

function chatPath(agent: string, sessionId: string): string {
	const base = `/agents/${encodeURIComponent(agent)}/chat`;
	return sessionId ? `${base}/${sessionId}` : base;
}

const creating = ref(false);
async function newChat() {
	creating.value = true;
	try {
		const sess = await store.create(props.agent);
		await router.push(chatPath(props.agent, sess.id));
	} catch (e) {
		console.error("Failed to create chat", e);
	} finally {
		creating.value = false;
	}
}

const menuId = ref("");
const editingId = ref("");
const editName = ref("");
const editInput = ref<HTMLInputElement[] | null>(null);

async function startRename(s: ChatSession) {
	menuId.value = "";
	editingId.value = s.id;
	editName.value = chatDisplayName(s);
	// Wait for the menu to close and hand focus back before taking it.
	await nextTick();
	setTimeout(() => {
		const el = editInput.value?.[0];
		el?.focus();
		el?.select();
	}, 0);
}

function cancelRename() {
	editingId.value = "";
}

async function saveRename() {
	const id = editingId.value;
	if (!id) return;
	editingId.value = "";
	const s = sessions.value.find((x) => x.id === id);
	const name = editName.value.trim();
	if (!s || !name || name === chatDisplayName(s)) return;
	try {
		await store.rename(props.agent, id, name);
	} catch (e) {
		console.error("Failed to rename chat", e);
	}
}

const deleteTarget = ref<ChatSession | null>(null);
const deleteOpen = ref(false);
function askDelete(s: ChatSession) {
	menuId.value = "";
	deleteTarget.value = s;
	deleteOpen.value = true;
}

async function confirmDelete() {
	const s = deleteTarget.value;
	deleteOpen.value = false;
	if (!s) return;
	try {
		await store.remove(props.agent, s.id);
		if (activeId.value === s.id) {
			const main = sessions.value.find(isMainChat);
			await router.push(chatPath(props.agent, main?.id ?? ""));
		}
	} catch (e) {
		console.error("Failed to delete chat", e);
	}
}
</script>
