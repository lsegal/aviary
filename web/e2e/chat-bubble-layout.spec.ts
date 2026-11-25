import { mockMCP, setAuthToken } from "./helpers/mockMCP";
import { expect, type Page, test } from "./helpers/test";

const longPath = `/very/long/${"segment".repeat(40)}/file.txt`;

const assistantReply = [
	"Here is what I found while checking the repository layout.",
	"",
	"1. List the tree and filter for the config file:",
	"",
	"```sh",
	`gh api repos/lsegal/glorp/git/trees/main?recursive=1 --jq '.tree[].path' | grep -E '(^|/)vite\\.config' ${"--extra-flag ".repeat(12)}`,
	"```",
	"",
	`2. Then open \`${longPath}\` and run \`gh\`.`,
	"",
	`| ${Array.from({ length: 14 }, (_, i) => `column ${i}`).join(" | ")} |`,
	`| ${Array.from({ length: 14 }, () => "---").join(" | ")} |`,
	`| ${Array.from({ length: 14 }, (_, i) => `value-${i}-wide`).join(" | ")} |`,
].join("\n");

async function openChat(page: Page) {
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
		session_messages: [
			{
				id: "m1",
				session_id: "s1",
				role: "user",
				content: `please check ${longPath}`,
				timestamp: "2026-03-12T12:00:00Z",
			},
			{
				id: "m2",
				session_id: "s1",
				role: "assistant",
				content: assistantReply,
				timestamp: "2026-03-12T12:00:01Z",
			},
			{
				id: "m3",
				session_id: "s1",
				role: "assistant",
				content: "short",
				timestamp: "2026-03-12T12:00:02Z",
			},
		],
		config_validate: [],
	});
	await page.goto("/agents/assistant/chat");
	await expect(page.getByTestId("chat-message-bubble")).toHaveCount(3);
}

/** Reads each bubble's box, its content's box, and its scroll container's width. */
function measure(page: Page) {
	return page.getByTestId("chat-message-bubble").evaluateAll((bubbles) =>
		bubbles.map((bubble) => {
			const content = bubble.querySelector<HTMLElement>(
				'[data-testid="chat-message-content"]',
			);
			const box = bubble.getBoundingClientRect();
			const inner = content?.getBoundingClientRect();
			const overflowing = [
				...bubble.querySelectorAll<HTMLElement>("p, li, pre, table, span"),
			].filter((el) => el.getBoundingClientRect().right > box.right + 0.5);
			return {
				left: box.left,
				right: box.right,
				width: box.width,
				column: bubble.parentElement?.getBoundingClientRect().width ?? 0,
				contentRight: inner?.right ?? 0,
				overflowing: overflowing.map((el) => el.tagName),
			};
		}),
	);
}

for (const viewport of [
	{ name: "desktop", width: 1280, height: 900 },
	{ name: "mobile", width: 390, height: 844 },
]) {
	test(`chat bubbles contain their content at ${viewport.name} width`, async ({
		page,
	}) => {
		await page.setViewportSize(viewport);
		await openChat(page);

		const [user, assistant, short] = await measure(page);
		for (const bubble of [user, assistant, short]) {
			expect(bubble.contentRight).toBeLessThanOrEqual(bubble.right + 0.5);
			expect(bubble.overflowing).toEqual([]);
		}

		// The wide reply grows to the bubble's cap, a share of the chat column,
		// and short replies still shrink to fit.
		expect(assistant.width).toBeGreaterThan(assistant.column * 0.85);
		expect(assistant.width).toBeLessThanOrEqual(assistant.column * 0.9 + 1);
		expect(short.width).toBeLessThan(assistant.width / 2);

		// Wide code blocks and tables scroll inside the bubble.
		const assistantBubble = page.getByTestId("chat-message-bubble").nth(1);
		for (const selector of ["pre", "table"]) {
			const scroll = await assistantBubble
				.locator(selector)
				.evaluate((el) => ({ client: el.clientWidth, scroll: el.scrollWidth }));
			expect(scroll.scroll).toBeGreaterThan(scroll.client);
		}
	});
}

test("inline code renders without literal backticks", async ({ page }) => {
	await openChat(page);
	const code = page
		.getByTestId("chat-message-bubble")
		.nth(1)
		.locator("p code, li code")
		.last();
	await expect(code).toHaveText("gh");
	const pseudo = await code.evaluate((el) => [
		getComputedStyle(el, "::before").content,
		getComputedStyle(el, "::after").content,
	]);
	expect(pseudo).toEqual(["none", "none"]);
});
