import { expect, test } from "@playwright/test";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";

// The rest of the chat scenarios are component tests in
// web/src/scenarios/chat.component.test.ts. This one stays a Playwright
// spec: it follows a link into a real popup page.

test("links in chat messages open in a new tab", async ({ page, context }) => {
	await setAuthToken(page);
	await mockMCP(page, {
		agent_list: [
			{
				id: "a1",
				name: "assistant",
				model: "anthropic/claude-sonnet-4-5",
				fallbacks: [],
				state: "idle",
			},
		],
		session_list: [
			{
				id: "s1",
				agent_id: "a1",
				name: "main",
				created_at: "2026-03-12T12:00:00Z",
			},
		],
		config_validate: [],
		session_messages: [
			{
				id: "m1",
				session_id: "s1",
				role: "user",
				content: "read https://example.com/user please",
				timestamp: "2026-03-12T12:00:00Z",
			},
			{
				id: "m2",
				session_id: "s1",
				role: "assistant",
				content:
					"See [the docs](https://example.com/docs) or https://example.com/bare",
				timestamp: "2026-03-12T12:00:01Z",
			},
		],
	});
	await context.route("https://example.com/**", (route) =>
		route.fulfill({ contentType: "text/html", body: "<p>external</p>" }),
	);
	await page.goto("/agents/assistant/chat");

	const links = [
		page.getByTestId("chat-user-message").getByRole("link", {
			name: "https://example.com/user",
		}),
		page.getByRole("link", { name: "the docs" }),
		page.getByRole("link", { name: "https://example.com/bare" }),
	];
	for (const link of links) {
		await expect(link).toHaveAttribute("target", "_blank");
		await expect(link).toHaveAttribute("rel", "noopener noreferrer");
	}

	const chatURL = page.url();
	const popup = context.waitForEvent("page");
	await links[1].click();
	await expect((await popup).locator("p")).toHaveText("external");
	expect(page.url()).toBe(chatURL);
});
