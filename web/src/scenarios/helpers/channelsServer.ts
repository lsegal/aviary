// The fake server side of channels (#857, #981) for the channels component
// tests (#1636): one agent whose channels the channel tools change, shared
// by channels*.component.test.ts.

import { mockMCP } from "../../component-test/server";

export interface Channel {
	type: string;
	id: string;
	name?: string;
	enabled?: boolean;
	deliver_to?: string;
}

export interface Task {
	name: string;
	schedule?: string;
	prompt?: string;
	target?: string;
}

export interface Config {
	agents: Array<{
		name: string;
		model: string;
		channels?: Channel[];
		tasks?: Task[];
	}>;
}

const NOW = new Date().toISOString();

/**
 * Mocks one agent with `channels`, keeping them as the channel tools change
 * them. Each channel's own session is named after its type and ID, as the
 * server names it, and lands in `sessions`.
 */
export function mockChannels(
	channels: Channel[],
	opts: { tasks?: Task[]; agents?: string[] } = {},
) {
	const config: Config = {
		agents: [
			{
				name: "assistant",
				model: "x/y",
				// A copy, so the channel tools never change the shared fixtures.
				channels: structuredClone(channels),
				tasks: opts.tasks ?? [],
			},
			...(opts.agents ?? []).map((name) => ({ name, model: "x/y" })),
		],
	};
	const calls: Record<string, Record<string, unknown>[]> = {};
	// While fail is set, channel_update fails.
	const failUpdates = { fail: false };
	const record = (tool: string, args?: Record<string, unknown>) => {
		calls[tool] ??= [];
		calls[tool].push(args ?? {});
	};
	const sessionOf = (c: Channel) => ({
		id: `channel:${c.type}:${c.id}`,
		agent_id: "assistant",
		name: `channel:${c.type}:${c.id}`,
		created_at: NOW,
		own: true,
	});
	const find = (name: unknown) => {
		const ch = config.agents[0].channels ?? [];
		const i = ch.findIndex(
			(c) =>
				(c.name || c.id).toLowerCase() === String(name ?? "").toLowerCase(),
		);
		if (i < 0)
			throw new Error(`agent "assistant" has no channel named ${name}`);
		return i;
	};
	const messages: Record<string, unknown[]> = {
		"channel:slack:team-slack": [
			{
				role: "assistant",
				content: "Daily report: all green",
				timestamp: NOW,
			},
		],
	};
	mockMCP({
		agent_list: opts.agents?.length
			? [
					{ id: "assistant", name: "assistant", model: "x/y", state: "idle" },
					...opts.agents.map((name) => ({
						id: name,
						name,
						model: "x/y",
						state: "idle",
					})),
				]
			: [{ id: "assistant", name: "assistant", model: "x/y", state: "idle" }],
		config_get: () => config,
		config_save: (args?: Record<string, unknown>) => {
			record("config_save", { ...args });
			Object.assign(config, JSON.parse(String(args?.config ?? "{}")));
			return "ok";
		},
		config_validate: [],
		job_list: [],
		auth_list: [],
		tool_list: [],
		session_list: [
			{ id: "main", agent_id: "assistant", name: "main", created_at: NOW },
		],
		session_messages: (args?: Record<string, unknown>) =>
			messages[String(args?.session_id)] ?? [],
		channel_sessions: (args?: Record<string, unknown>) => {
			record("channel_sessions", args);
			return [
				sessionOf((config.agents[0].channels ?? [])[find(args?.channel)]),
			];
		},
		channel_create: (args?: Record<string, unknown>) => {
			record("channel_create", args);
			const name = String(args?.name);
			const ch: Channel = {
				type: String(args?.type),
				id: name.toLowerCase().replace(/[^a-z0-9]+/g, "-"),
				name,
				enabled: false,
			};
			config.agents[0].channels = [...(config.agents[0].channels ?? []), ch];
			return { channel: ch, session_id: sessionOf(ch).id };
		},
		channel_rename: (args?: Record<string, unknown>) => {
			record("channel_rename", args);
			const ch = config.agents[0].channels ?? [];
			ch[find(args?.channel)].name = String(args?.new_name);
			return "renamed";
		},
		channel_update: (args?: Record<string, unknown>) => {
			record("channel_update", args);
			if (failUpdates.fail) throw new Error("disk is read-only");
			const ch = config.agents[0].channels ?? [];
			ch[find(args?.channel)].enabled = Boolean(args?.enabled);
			return "updated";
		},
		channel_remove: (args?: Record<string, unknown>) => {
			record("channel_remove", args);
			const i = find(args?.channel);
			config.agents[0].channels = (config.agents[0].channels ?? []).filter(
				(_, j) => j !== i,
			);
			return "removed";
		},
	});
	return { config, calls, failUpdates };
}

export const TEAM: Channel = {
	type: "slack",
	id: "team-slack",
	name: "Team Slack",
	deliver_to: "C123",
};
