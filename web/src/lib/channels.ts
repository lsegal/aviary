import type { AgentChannel } from "../stores/settings";

/** The channel's display name: its name, else its ID, else its type. */
export function channelName(
	ch: Pick<AgentChannel, "name" | "id" | "type">,
): string {
	return ch.name?.trim() || ch.id?.trim() || ch.type;
}

/** The delivery target that picks the channel named name. */
export function channelTarget(name: string): string {
	return `channel:${name.trim()}`;
}

/** The channel name a "channel:<name>" target picks, or "". */
export function channelTargetName(target?: string): string {
	const t = target?.trim() ?? "";
	return t.startsWith("channel:") ? t.slice("channel:".length).trim() : "";
}

/** The channel's conversation view. */
export function channelPath(agent: string, name = ""): string {
	const base = `/agents/${encodeURIComponent(agent)}/channels`;
	return name ? `${base}/${encodeURIComponent(name)}` : base;
}

/** The channel's settings form. */
export function channelSettingsPath(agent: string, name: string): string {
	return `${channelPath(agent, name)}/settings`;
}

/** The channel's Workflows settings tab, optionally opened on one workflow. */
export function workflowSettingsPath(
	agent: string,
	channel: string,
	workflow = "",
): { path: string; query: Record<string, string> } {
	const query: Record<string, string> = { tab: "workflows" };
	if (workflow) query.workflow = workflow;
	return { path: channelSettingsPath(agent, channel), query };
}

/** A channel's session, from channel_sessions. */
export interface ChannelSession {
	id: string;
	agent_id: string;
	name?: string;
	title?: string;
	created_at: string;
	last_message_at?: string;
	is_processing?: boolean;
	/** The channel's own session, where deliveries to the channel land. */
	own: boolean;
}

export const CHANNEL_TYPES = [
	{ value: "slack", label: "Slack" },
	{ value: "discord", label: "Discord" },
	{ value: "signal", label: "Signal" },
] as const;
