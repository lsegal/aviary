import { defineStore } from "pinia";
import { ref } from "vue";
import { useMCP } from "../composables/useMCP";

export interface ChatSession {
	id: string;
	agent_id: string;
	name: string;
	/** Display name generated from the first prompt or set by a rename. */
	title?: string;
	created_at: string;
	last_message_at?: string;
	is_processing?: boolean;
}

/** The default chat, which every agent has and which cannot be deleted. */
export function isMainChat(s: Pick<ChatSession, "name">): boolean {
	return s.name === "main";
}

/** The name a chat shows in the Chats list. */
export function chatDisplayName(s: ChatSession): string {
	if (s.title) return s.title;
	if (isMainChat(s)) return "Main Chat";
	// A chat from session_create is named by its ID until it gets a title.
	return s.name && s.name !== s.id ? s.name : "New chat";
}

/** Each agent's chats, shared by the sidebar's Chats list and the chat view. */
export const useChatSessionsStore = defineStore("chatSessions", () => {
	const { callTool } = useMCP();
	const byAgent = ref<Record<string, ChatSession[]>>({});
	const loading = ref<Record<string, boolean>>({});

	function sessions(agent: string): ChatSession[] {
		return byAgent.value[agent] ?? [];
	}

	async function load(agent: string): Promise<ChatSession[]> {
		loading.value = { ...loading.value, [agent]: true };
		try {
			const raw = await callTool("session_list", { agent });
			const listed = (JSON.parse(raw) as ChatSession[]) ?? [];
			byAgent.value = { ...byAgent.value, [agent]: listed };
			return listed;
		} finally {
			loading.value = { ...loading.value, [agent]: false };
		}
	}

	async function create(agent: string): Promise<ChatSession> {
		const raw = await callTool("session_create", { agent });
		const sess = JSON.parse(raw) as ChatSession;
		byAgent.value = {
			...byAgent.value,
			[agent]: [...sessions(agent), sess],
		};
		return sess;
	}

	async function rename(agent: string, id: string, name: string) {
		const title = name.trim();
		if (!title) return;
		await callTool("session_rename", { agent, session_id: id, name: title });
		byAgent.value = {
			...byAgent.value,
			[agent]: sessions(agent).map((s) => (s.id === id ? { ...s, title } : s)),
		};
	}

	async function remove(agent: string, id: string) {
		await callTool("session_remove", { agent, session_id: id });
		byAgent.value = {
			...byAgent.value,
			[agent]: sessions(agent).filter((s) => s.id !== id),
		};
	}

	return { byAgent, loading, sessions, load, create, rename, remove };
});
