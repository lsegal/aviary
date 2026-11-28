// Pure helpers for the chat view's messages (#1607): parsing stored and
// streamed tool records, and classifying media URLs.

export interface ToolData {
	/** Ties a streamed call to its result; the two share one block. */
	id?: string;
	name: string;
	args?: Record<string, unknown>;
	result?: string;
	error?: string;
	/** Set on the streamed result of a call. */
	done?: boolean;
}

export interface Message {
	id?: string;
	localKey?: string;
	role: "user" | "assistant" | "tool";
	text: string;
	mediaURLs?: string[];
	toolData?: ToolData;
	/** A streamed tool call that has not reported its result yet. */
	toolPending?: boolean;
	/** When this client saw a pending tool call start, in epoch ms. */
	toolStartedAt?: number;
	toolFinishedAt?: number;
	timestamp?: string;
	model?: string;
	isError?: boolean;
	/** The key of the user message whose turn this error failed. */
	errorFor?: string;
	/** The workflows that handled this channel message instead of the agent. */
	workflows?: MessageWorkflow[];
}

/** A workflow that handled a channel message, and its run. */
export interface MessageWorkflow {
	task_id: string;
	name: string;
	job_id?: string;
}

/** Returns true if an assistant message text looks like an error. */
export function isErrorMessage(text: string): boolean {
	return text.startsWith("Error:") || text.startsWith("[no LLM provider");
}

/** Parse a "[tool] ..." content string into a Message. */
export function parseToolMessage(
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

/**
 * Collapse stored tool records that share a tool-use ID into one entry, the
 * latest, so a call stored more than once shows once (#740). Records without
 * an ID are distinct calls and are all kept.
 */
export function dedupeToolMessages(list: Message[]): Message[] {
	const last = new Map<string, number>();
	list.forEach((m, i) => {
		if (m.role === "tool" && m.toolData?.id) last.set(m.toolData.id, i);
	});
	return list.filter(
		(m, i) =>
			m.role !== "tool" || !m.toolData?.id || last.get(m.toolData.id) === i,
	);
}

export function parseToolChunk(
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

export function extractCompleteToolPayload(
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

export function formatDateLabel(d: Date): string {
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

export function formatTime(timestamp: string): string {
	return new Date(timestamp).toLocaleTimeString(undefined, {
		hour: "numeric",
		minute: "2-digit",
	});
}

export const SAVED_AUDIO_PREFIX = "/api/media?";

/** Maps a saved channel audio file:// URL to a URL the browser can play. */
export function savedAudioURL(mediaURL: string, token?: string | null): string {
	if (!mediaURL.startsWith("file:")) return mediaURL;
	let path: string;
	try {
		// Windows paths come through as /C:/...
		path = decodeURIComponent(new URL(mediaURL).pathname).replace(
			/^\/([A-Za-z]:)/,
			"$1",
		);
	} catch {
		return mediaURL;
	}
	if (!/\.(ogg|oga|opus|m4a|aac|mp3|wav|webm|flac|amr|caf)$/i.test(path))
		return mediaURL;
	const params = new URLSearchParams({ path });
	if (token) params.set("token", token);
	return SAVED_AUDIO_PREFIX + params.toString();
}

export function mediaTypeFromURL(mediaURL: string): string {
	const s = mediaURL.trim().toLowerCase();
	if (s.startsWith(SAVED_AUDIO_PREFIX)) return "audio/*";
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

export function isImageMedia(mediaURL: string): boolean {
	return mediaTypeFromURL(mediaURL).startsWith("image/");
}

export function isVideoMedia(mediaURL: string): boolean {
	return mediaTypeFromURL(mediaURL).startsWith("video/");
}

export function isAudioMedia(mediaURL: string): boolean {
	return mediaTypeFromURL(mediaURL).startsWith("audio/");
}

/** Tool name shown in the collapsed pill; details stay inside the expansion. */
export function toolName(msg: Message): string {
	return msg.toolData?.name || msg.text;
}
