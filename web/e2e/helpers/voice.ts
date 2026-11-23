import { expect, type Page } from "@playwright/test";
import { mockMCP, setAuthToken } from "./mockMCP";

/** Opens an agent's chat and returns the composer and its controls. */
export async function openChat(page: Page, runs: unknown[] = []) {
	await setAuthToken(page);
	await mockMCP(page, {
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
	await page.goto("/agents/assistant/chat");
	const composer = page.getByTestId("chat-composer");
	await expect(composer).toBeVisible();
	return {
		composer,
		mic: composer.getByTestId("voice-button"),
		send: composer.getByRole("button", { name: "Send message" }),
		input: composer.getByPlaceholder("Describe a task or ask a question"),
	};
}
