import { expect, test, vi } from "vitest";
import { page } from "vitest/browser";
import { renderApp } from "../component-test/app";
import { declarePages } from "../component-test/routes";
import { mockMCP } from "../component-test/server";

// The pages this file renders (see component-test/routes.ts).
declarePages(() => import("../views/ChatView.vue"));

// From unsaved-changes.spec.ts. Its own file: chat drafts are read from
// storage once, when their module loads, and the harness loads the app
// before any test runs. So this test seeds storage, then loads a fresh copy
// of the drafts module the way a page load does, and checks both what it
// keeps and what it writes back.

test("stale stored drafts are dropped on load", async () => {
	localStorage.setItem(
		"aviary.chatDrafts",
		JSON.stringify({
			"assistant/s1": { text: "old", attachments: [], updated: 1 },
			"assistant/s2": {
				text: "fresh",
				attachments: [],
				updated: Date.now(),
			},
		}),
	);
	vi.resetModules();
	const drafts = await import("../components/chat/drafts");
	expect(drafts.getChatDraft(drafts.chatDraftKey("assistant", "s1")).text).toBe(
		"",
	);
	expect(drafts.getChatDraft(drafts.chatDraftKey("assistant", "s2")).text).toBe(
		"fresh",
	);

	mockMCP({
		agent_list: [
			{ id: "assistant", name: "assistant", model: "x/y", state: "idle" },
		],
		config_get: { agents: [{ name: "assistant", model: "x/y" }] },
		config_validate: [],
		job_list: [],
		session_list: [
			{
				id: "s1",
				agent_id: "assistant",
				name: "main",
				created_at: "2026-03-12T12:00:00Z",
			},
		],
		session_messages: [],
	});
	await renderApp("/agents/assistant/chat/s1");
	await expect
		.element(page.getByTestId("chat-composer").getByRole("textbox"))
		.toHaveValue("");
	const stored = JSON.parse(localStorage.getItem("aviary.chatDrafts") ?? "{}");
	expect(Object.keys(stored)).toEqual(["assistant/s2"]);
});
