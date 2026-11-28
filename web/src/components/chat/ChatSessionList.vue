<template>
	<div>
		<SidebarItemList data-testid="sidebar-chats" :items="ordered" :item-key="(s) => s.id" more-testid="sidebar-chats-more">
			<template #default="{ item: s }">
				<SidebarItemRow data-testid="chat-row" :data-session="s.id" :to="chatPath(agent, s.id)"
					:label="chatDisplayName(s)" :active="s.id === activeId" :actions="chatActions(s)"
					menu-testid="chat-menu-button" :editing="editingId === s.id"
					:menu-open="menuId === s.id" @update:menu-open="(open) => (menuId = open ? s.id : '')"
					@select="(key) => onAction(key, s)" @dblclick="startRename(s)">
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

		<ConfirmDialog v-model:open="confirmOpen" :title="confirmTitle" :action="confirmAction"
			:confirm-label="confirmKind === 'clear' ? 'Clear' : 'Delete'"
			:busy-label="confirmKind === 'clear' ? 'Clearing…' : 'Deleting…'" test-id="chat-confirm-dialog">
			<p v-if="confirmKind === 'clear'">
				This removes all messages in this chat. The chat itself stays. This cannot be undone.
			</p>
			<p v-else>
				This will permanently delete
				<span class="break-all font-medium text-gray-900 dark:text-white">{{ confirmTarget ? chatDisplayName(confirmTarget) : "" }}</span>
				and all its messages. This cannot be undone.
			</p>
		</ConfirmDialog>
	</div>
</template>

<script setup lang="ts">
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
import ConfirmDialog from "../ui/ConfirmDialog.vue";

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
const clearAction: SidebarItemAction = {
	key: "clear",
	label: "Clear",
	danger: true,
};
// Chats the server will not delete, such as Main Chat, are cleared instead.
function chatActions(s: ChatSession): SidebarItemAction[] {
	return [renameAction, s.deletable ? deleteAction : clearAction];
}

function onAction(key: string, s: ChatSession) {
	if (key === "delete" || key === "clear") askConfirm(key, s);
	else startRename(s);
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
				session_id?: string;
			};
			if (
				data.agent_id !== props.agent &&
				data.agent_id !== `agent_${props.agent}`
			)
				return;
			// A chat removed elsewhere, such as on System → Sessions, leaves the list.
			if (data.type === "session_removed" && data.session_id) {
				store.forget(props.agent, data.session_id);
				return;
			}
			if (data.type !== "session_message") return;
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

const confirmTarget = ref<ChatSession | null>(null);
const confirmKind = ref<"delete" | "clear">("delete");
const confirmOpen = ref(false);
function askConfirm(kind: "delete" | "clear", s: ChatSession) {
	menuId.value = "";
	confirmKind.value = kind;
	confirmTarget.value = s;
	confirmOpen.value = true;
}

const confirmTitle = computed(() =>
	confirmKind.value === "clear"
		? `Clear ${confirmTarget.value ? chatDisplayName(confirmTarget.value) : ""}?`
		: "Delete chat?",
);

async function confirmAction() {
	const s = confirmTarget.value;
	if (!s) return;
	if (confirmKind.value === "clear") {
		await store.clear(props.agent, s.id);
		return;
	}
	await store.remove(props.agent, s.id);
	if (activeId.value === s.id) {
		const main = sessions.value.find(isMainChat);
		await router.push(chatPath(props.agent, main?.id ?? ""));
	}
}
</script>
