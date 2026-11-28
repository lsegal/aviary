import { expect } from "vitest";
import type { Locator } from "vitest/browser";
import { mockMCP, routeWebSocket } from "../component-test/server";

// The fake server and scroll helpers the auto-follow scenarios share (#1072).

export const NOW = new Date().toISOString();

export interface TurnItem {
	text?: string;
	tool?: { id: string; name: string; done?: boolean };
}

/** The mock server: a long stored history plus a running turn. */
export class Server {
	messages: object[] = Array.from({ length: 40 }, (_, i) => ({
		id: `m${i}`,
		role: i % 2 ? "assistant" : "user",
		content: `Message ${i}\n\nwith a second paragraph to give it height`,
		timestamp: NOW,
	}));
	items: TurnItem[] = [{ text: "Working" }];
	sockets: Array<{ send: (m: string) => void }> = [];
	session = "";
	lastFrame = "";

	/** Pushes the running turn to every client. */
	broadcast() {
		const turn = {
			agent_id: "assistant",
			session_id: this.session,
			processing: true,
			items: this.items,
		};
		const frame = JSON.stringify({
			type: "session_turn",
			agent_id: "assistant",
			session_id: this.session,
			turn,
		});
		this.lastFrame = frame;
		for (const ws of this.sockets) ws.send(frame);
	}

	/** Streams more text into the turn's reply. */
	stream(text: string) {
		const last = this.items.at(-1);
		if (last?.text !== undefined) last.text += text;
		else this.items.push({ text });
		this.broadcast();
	}

	/** Starts a new tool call: one more entry in the chat. */
	addTool(id: string) {
		this.items.push({ tool: { id, name: "exec" } });
		this.broadcast();
	}

	attach(opts: { workflow?: boolean } = {}) {
		routeWebSocket(/\/api\/ws/, (ws) => {
			this.sockets.push(ws);
			// The app opens more than one socket (server status, the chat), and
			// the chat's can connect after the turn started streaming; it gets
			// the turn as it stands, as a client that just subscribed would.
			if (this.lastFrame) ws.send(this.lastFrame);
		});
		mockMCP({
			agent_list: [
				{ id: "assistant", name: "assistant", model: "x/y", state: "idle" },
			],
			config_get: () => ({
				agents: [
					{
						name: "assistant",
						model: "x/y",
						channels: [{ type: "slack", id: "team", name: "Team" }],
						tasks: [
							opts.workflow
								? {
										name: "report",
										trigger: "message",
										channel: "slack:team",
										prompt: "Report",
									}
								: { name: "report", every: "15m", prompt: "Report" },
						],
					},
				],
			}),
			config_validate: [],
			job_list: [],
			auth_list: [],
			tool_list: [],
			task_list: () => [
				{
					id: "assistant/report",
					agent_name: "assistant",
					name: "report",
					enabled: true,
					history: [],
				},
			],
			task_session: () => ({
				id: "task-report",
				agent_id: "assistant",
				name: "task:report",
			}),
			channel_sessions: () => [
				{
					id: "channel:slack:team",
					agent_id: "assistant",
					name: "channel:slack:team",
					created_at: NOW,
					own: true,
				},
			],
			session_list: [
				{
					id: "main",
					agent_id: "assistant",
					name: "main",
					created_at: NOW,
					is_processing: true,
				},
			],
			session_messages: (args?: Record<string, unknown>) => {
				this.session = String(args?.session_id ?? "");
				return this.messages;
			},
			session_turn: (args?: Record<string, unknown>) => ({
				agent_id: "assistant",
				session_id: String(args?.session_id ?? ""),
				processing: true,
				items: this.items,
			}),
			agent_run: { stream: ["on it"], result: "" },
		});
	}
}

export const count = (loc: Locator) => loc.elements().length;

export const distanceFromBottom = (scroller: Locator) => {
	const el = scroller.element();
	return el.scrollHeight - el.scrollTop - el.clientHeight;
};

export async function expectAtBottom(scroller: Locator) {
	await expect.poll(() => distanceFromBottom(scroller)).toBeLessThanOrEqual(8);
}

/** Asserts nothing covers the pill, so a user can see and click it. */
export async function expectPillUncovered(pill: Locator) {
	await expect
		.poll(() => {
			const el = pill.element();
			const r = el.getBoundingClientRect();
			const hit = document.elementFromPoint(
				r.left + r.width / 2,
				r.top + r.height / 2,
			);
			return !!hit && el.contains(hit);
		})
		.toBe(true);
}

/** Scrolls the view to its top, as a user scrolling up would. */
export function scrollToTop(scroller: Locator) {
	scroller.element().scrollTop = 0;
}
