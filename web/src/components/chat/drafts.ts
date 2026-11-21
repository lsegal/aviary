import type { ChatAttachment } from "./attachments";

/** A chat's unsent composer text and attachments. */
export interface ChatDraft {
	text: string;
	attachments: ChatAttachment[];
}

interface StoredDraft extends ChatDraft {
	/** When the draft last changed, in ms since the epoch. */
	updated: number;
}

const STORAGE_KEY = "aviary.chatDrafts";
/** Drafts untouched this long are dropped when the app loads. */
export const DRAFT_MAX_AGE_MS = 30 * 24 * 60 * 60 * 1000;

// The drafts of every chat, kept for the app session whatever storage allows,
// and mirrored to localStorage so they also survive a reload.
const drafts = new Map<string, StoredDraft>(loadDrafts());

function loadDrafts(): [string, StoredDraft][] {
	try {
		const raw = JSON.parse(localStorage.getItem(STORAGE_KEY) ?? "{}");
		const cutoff = Date.now() - DRAFT_MAX_AGE_MS;
		return Object.entries(raw as Record<string, StoredDraft>).filter(
			([, d]) =>
				typeof d?.text === "string" &&
				Array.isArray(d.attachments) &&
				typeof d.updated === "number" &&
				d.updated >= cutoff,
		);
	} catch {
		return [];
	}
}

let persistTimer: ReturnType<typeof setTimeout> | undefined;
/** Persists soon, so typing does not rewrite storage on every key. */
function schedulePersist() {
	clearTimeout(persistTimer);
	persistTimer = setTimeout(persist, 250);
}

function persist() {
	clearTimeout(persistTimer);
	const all = Object.fromEntries(drafts);
	try {
		localStorage.setItem(STORAGE_KEY, JSON.stringify(all));
	} catch {
		// Attachments can outgrow the storage quota; keep at least the text.
		try {
			localStorage.setItem(
				STORAGE_KEY,
				JSON.stringify(
					Object.fromEntries(
						Object.entries(all).map(([k, d]) => [k, { ...d, attachments: [] }]),
					),
				),
			);
		} catch {
			// Storage is unavailable; drafts last for this app session only.
		}
	}
}

/** The draft key of an agent's chat session. */
export function chatDraftKey(agent: string, sessionId: string): string {
	return `${agent}/${sessionId}`;
}

/** The chat's saved draft, empty when it has none. */
export function getChatDraft(key: string): ChatDraft {
	const d = drafts.get(key);
	return d
		? { text: d.text, attachments: [...d.attachments] }
		: { text: "", attachments: [] };
}

/** Saves the chat's draft, dropping it once it is empty. */
export function setChatDraft(key: string, draft: ChatDraft) {
	const prev = drafts.get(key);
	if (
		prev?.text === draft.text &&
		prev.attachments.length === draft.attachments.length &&
		prev.attachments.every((a, i) => a.id === draft.attachments[i].id)
	)
		return;
	if (!draft.text && !draft.attachments.length) {
		if (!prev) return;
		drafts.delete(key);
	} else {
		drafts.set(key, {
			text: draft.text,
			attachments: [...draft.attachments],
			updated: Date.now(),
		});
	}
	schedulePersist();
}

// Write back what loading pruned, and anything pending when the page goes.
persist();
window.addEventListener("pagehide", persist);
