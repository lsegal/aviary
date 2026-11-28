import { expect, test } from "vitest";
import { commands, page } from "vitest/browser";
import { renderApp } from "../component-test/app";
import { expectAtBottom, Server, scrollToTop } from "./auto-follow.fixtures";
// The pages this file renders (see component-test/routes.ts).
import "../views/ChatView.vue";
import "../views/SettingsView.vue";

// Its own file: the other auto-follow scenarios leave the agent's running
// turn in module state, where it keeps the composer busy for the rest of
// the page, so a message sent after them never shows (#1072).

test("sending a message scrolls to the bottom and follows again", async () => {
	const server = new Server();
	server.items = [];
	server.attach();
	await renderApp("/agents/assistant/chat");
	const scroller = page.getByTestId("chat-messages");
	await expect.element(page.getByText("Message 39")).toBeVisible();
	scrollToTop(scroller);
	await expect.element(page.getByTestId("chat-more-below")).toBeVisible();

	await page.getByRole("textbox").first().fill("hello there");
	await commands.keyPress("Enter");
	await expect
		.element(page.getByTestId("chat-user-message").last())
		.toMatchTextContent("hello there");
	await expectAtBottom(scroller);
	await expect
		.element(page.getByTestId("chat-more-below"))
		.not.toBeInTheDocument();
});
