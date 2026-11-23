import { age } from "../../lib/projects";

/** One message someone sent on a channel, from channel_activity. */
export interface ChannelMessage {
	entry: string;
	/** The title of the conversation it is on. */
	conversation?: string;
	/** The sender's display name; empty when only a raw ID is known. */
	sender?: string;
	text: string;
	timestamp: string;
	/** Each attachment's kind: image, audio, video or file. */
	attachments?: string[];
	/** The workflows that handled it. */
	workflows?: string[];
}

/** A channel's recent traffic, from channel_activity. */
export interface ChannelActivity {
	last_activity?: string;
	messages_24h: number;
	/** The latest messages others sent, oldest first. */
	messages: ChannelMessage[];
}

/** The label a message shows when its sender is truly unknown. */
export const UNKNOWN_SENDER = "Unknown sender";

/** "5m ago" for an ISO time, or "" when it does not parse. */
export function ago(iso: string | undefined, now = Date.now()): string {
	const a = iso ? age(iso, now) : "";
	return a ? `${a} ago` : "";
}

/** The full local date and time of an ISO time, for tooltips. */
export function fullTime(iso: string): string {
	const d = new Date(iso);
	return Number.isNaN(d.getTime()) ? "" : d.toLocaleString();
}
