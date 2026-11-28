// Mirrors web/e2e/helpers/voice.ts for component tests (#1636).
import { expect } from "vitest";
import { page } from "vitest/browser";
import { goto, setAuthToken } from "../../component-test/app";
import { mockMCP } from "../../component-test/server";

/** Opens an agent's chat and returns the composer and its controls. */
export async function openChat(runs: unknown[] = []) {
	setAuthToken();
	mockMCP({
		agent_list: [{ id: "a1", name: "assistant", model: "x/y", state: "idle" }],
		session_list: [
			{
				id: "s1",
				agent_id: "a1",
				name: "main",
				created_at: "2026-03-12T12:00:00Z",
			},
		],
		session_messages: [],
		config_validate: [],
		agent_run: (args?: Record<string, unknown>) => {
			runs.push(args);
			return { stream: ["ok"], result: "" };
		},
	});
	// Mounts the app when none is yet, as the spec's first page.goto.
	await goto("/agents/assistant/chat");
	const composer = page.getByTestId("chat-composer");
	await expect.element(composer).toBeVisible();
	return {
		composer,
		mic: composer.getByTestId("voice-button"),
		send: composer.getByRole("button", { name: "Send message" }),
		input: composer.getByPlaceholder("Describe a task or ask a question"),
	};
}
