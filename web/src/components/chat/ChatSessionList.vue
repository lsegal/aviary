<template>
	<div>
		<SidebarItemList data-testid="sidebar-chats" :items="ordered" :item-key="(s) => s.id" more-testid="sidebar-chats-more">
			<template #default="{ item: s }">
				<SidebarItemRow data-testid="chat-row" :data-session="s.id" :to="chatPath(agent, s.id)"
					:label="chatDisplayName(s)" :active="s.id === activeId" :actions="chatActions(s)"
					menu-testid="chat-menu-button" :editing="editingId === s.id"
					:menu-open="menuId === s.id" @update:menu-open="(open) => (menuId = open ? s.id : '')"
					@select="(key) => (key === 'delete' ? askDelete(s) : startRename(s))" @dblclick="startRename(s)">
					<template #editor>
						<input :ref="setEditInput" v-model="editName" data-testid="chat-rename-input"
							:aria-label="`Rename ${chatDisplayName(s)}`"
							class="m-0.5 min-w-0 flex-1 rounded-md border border-blue-500 bg-white px-2.5 py-1.5 text-sm text-gray-900 focus:outline-none dark:bg-gray-900 dark:text-white"
							@keydown.enter.prevent="saveRename" @keydown.esc.prevent="cancelRename" @blur="saveRename" />
					</template>
					<template #meta>
						<span v-if="s.last_message_at" data-testid="chat-last-message"
							:title="new Date(s.last_message_at).toLocaleString()"
							class="hidden shrink-0 pr-1 text-xs text-gray-400 group-hover:inline group-focus-within:inline">
							{{ age(s.last_message_at, now) }}
						</span>
					</template>
				</SidebarItemRow>
			</template>
		</SidebarItemList>

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
import {
	AlertDialogAction,
	AlertDialogCancel,
	AlertDialogContent,
	AlertDialogDescription,
	AlertDialogOverlay,
	AlertDialogPortal,
	AlertDialogRoot,
	AlertDialogTitle,
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
import SidebarItemList from "../sidebar/SidebarItemList.vue";
import SidebarItemRow from "../sidebar/SidebarItemRow.vue";
import type { SidebarItemAction } from "../sidebar/types";

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

// A chat ranks by its last message; one with no messages yet ranks by when it
// was created, but shows no time, since it has no last message (#661).
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
const renameAction: SidebarItemAction = { key: "edit", label: "Rename" };
const deleteAction: SidebarItemAction = {
	key: "delete",
	label: "Delete",
	danger: true,
};
// Main Chat cannot be deleted.
function chatActions(s: ChatSession): SidebarItemAction[] {
	return isMainChat(s) ? [renameAction] : [renameAction, deleteAction];
}

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

const menuId = ref("");
const editingId = ref("");
const editName = ref("");
let editInput: HTMLInputElement | null = null;
function setEditInput(el: unknown) {
	editInput = el instanceof HTMLInputElement ? el : null;
}

async function startRename(s: ChatSession) {
	menuId.value = "";
	editingId.value = s.id;
	editName.value = chatDisplayName(s);
	// Wait for the menu to close and hand focus back before taking it.
	await nextTick();
	setTimeout(() => {
		const el = editInput;
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
