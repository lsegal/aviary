import { afterEach, expect, test } from "vitest";
import { commands, page } from "vitest/browser";
import { renderApp } from "../component-test/app";
import { declarePages } from "../component-test/routes";
import { eventSourcesOpened, mockMCP, route } from "../component-test/server";
import {
	count,
	distanceFromBottom,
	expectAtBottom,
	expectPillUncovered,
	NOW,
	Server,
	scrollToTop,
} from "./auto-follow.fixtures";

// The pages this file renders (see component-test/routes.ts).
declarePages(
	() => import("../views/ChannelView.vue"),
	() => import("../views/ChatView.vue"),
	() => import("../views/LogsView.vue"),
	() => import("../views/TaskView.vue"),
);

/*
 * Every chat and live log view shares one auto-follow behavior (#1072): it
 * opens at its newest line, follows new content while the user is at the
 * bottom, and stops following with a "More below" pill once they scroll up.
 */

const HOSTS = [
	{
		name: "agent chat",
		path: "/agents/assistant/chat",
		scope: () => page.locator("body"),
	},
	{
		name: "channel chat",
		path: "/agents/assistant/channels/Team",
		scope: () => page.getByTestId("channel-chat"),
	},
	{
		name: "task session",
		path: "/agents/assistant/tasks/report",
		scope: () => page.getByTestId("task-chat"),
	},
	{
		name: "workflow session",
		path: "/agents/assistant/tasks/report",
		workflow: true,
		scope: () => page.getByTestId("task-chat"),
	},
];

for (const host of HOSTS) {
	test(`${host.name} opens at the bottom, follows, and pauses with the pill`, async () => {
		const server = new Server();
		server.attach({ workflow: host.workflow });
		await renderApp(host.path);
		const chat = host.scope();
		const scroller = chat.getByTestId("chat-messages");
		const pill = chat.getByTestId("chat-more-below");
		await expect.element(chat.getByText("Message 39")).toBeVisible();
		await expect
			.poll(() => {
				const el = scroller.element();
				return el.scrollHeight > el.clientHeight;
			})
			.toBe(true);
		await expectAtBottom(scroller);
		await expect.poll(() => server.sockets.length).toBeGreaterThan(0);
		// The newest line ends above the composer, not hidden under it.
		const list = scroller.element().getBoundingClientRect();
		const composer = chat
			.getByTestId("chat-composer")
			.element()
			.getBoundingClientRect();
		expect(list.y + list.height).toBeLessThanOrEqual(composer.y + 1);

		// Streaming text keeps a following view pinned.
		for (let i = 0; i < 5; i++) server.stream("\nmore streamed output line");
		await expect
			.element(chat.getByText(/more streamed output line/))
			.toBeVisible();
		await expectAtBottom(scroller);
		await expect.element(pill).not.toBeInTheDocument();

		// Scrolled up, new content doesn't move the view, and the pill counts it.
		scrollToTop(scroller);
		await expect.element(pill).toBeVisible();
		await expect.element(pill).toHaveText("More below ↓");
		await expectPillUncovered(pill);
		server.addTool("call_1");
		server.stream("\nafter the tool");
		await expect.poll(() => count(chat.getByTestId("chat-tool-call"))).toBe(1);
		await expect.element(pill).toHaveText("2 new · More below ↓");
		expect(scroller.element().scrollTop).toBe(0);

		// The pill returns to the end and follows again.
		await pill.click();
		await expectAtBottom(scroller);
		await expect.element(pill).not.toBeInTheDocument();
		server.stream("\nfollowing again");
		await expect.element(chat.getByText(/following again/)).toBeVisible();
		await expectAtBottom(scroller);

		// Scrolling back down by hand follows again too.
		scrollToTop(scroller);
		await expect.element(pill).toBeVisible();
		const el = scroller.element();
		el.scrollTop = el.scrollHeight;
		await expect.element(pill).not.toBeInTheDocument();
		server.stream("\nstill following");
		await expect.element(chat.getByText(/still following/)).toBeVisible();
		await expectAtBottom(scroller);
	});
}

// Images load straight from the network, not through fetch, so the fake
// server can't hold one back. Holding the src assignment does the same:
// the image arrives once the test releases it.
let restoreImages = () => {};
afterEach(() => {
	restoreImages();
	restoreImages = () => {};
});

function holdImage(path: string, body: string) {
	const proto = HTMLImageElement.prototype;
	const desc = Object.getOwnPropertyDescriptor(proto, "src");
	const setAttribute = proto.setAttribute;
	if (!desc?.set) throw new Error("no img src setter");
	const set = desc.set;
	const held: HTMLImageElement[] = [];
	const late = `data:image/svg+xml,${encodeURIComponent(body)}`;
	let released = false;
	const isHeld = (v: unknown) => !released && String(v).endsWith(path);
	Object.defineProperty(proto, "src", {
		...desc,
		set(this: HTMLImageElement, v: string) {
			if (isHeld(v)) held.push(this);
			else set.call(this, v);
		},
	});
	proto.setAttribute = function (this: HTMLImageElement, name, value) {
		if (name === "src" && isHeld(value)) held.push(this);
		else setAttribute.call(this, name, value);
	};
	restoreImages = () => {
		Object.defineProperty(proto, "src", desc);
		proto.setAttribute = setAttribute;
	};
	return () => {
		released = true;
		for (const img of held) set.call(img, late);
	};
}

test("a chat opens at the bottom after a late image loads", async () => {
	const server = new Server();
	server.items = [];
	server.messages.push({
		id: "img",
		role: "assistant",
		content: "",
		media_urls: ["/late-image.png"],
		timestamp: NOW,
	});
	const release = holdImage(
		"/late-image.png",
		'<svg xmlns="http://www.w3.org/2000/svg" width="200" height="300"><rect width="200" height="300" fill="teal"/></svg>',
	);
	server.attach();
	await renderApp("/agents/assistant/chat");
	const scroller = page.getByTestId("chat-messages");
	await expect.element(page.getByText("Message 39")).toBeVisible();
	await expectAtBottom(scroller);
	const before = scroller.element().scrollHeight;

	release();
	await expect
		.poll(() => scroller.element().scrollHeight)
		.toBeGreaterThan(before + 100);
	await expectAtBottom(scroller);
});

test("a deep link to a message opens there instead of the bottom", async () => {
	const server = new Server();
	server.items = [];
	server.attach();
	await renderApp("/agents/assistant/chat#message-m5");
	const scroller = page.getByTestId("chat-messages");
	const target = page.locator("#message-m5");
	await expect.element(target).toBeInViewport();
	expect(distanceFromBottom(scroller)).toBeGreaterThan(100);
	await expect.element(page.getByTestId("chat-more-below")).toBeVisible();
});

test("the pill scrolls instantly under prefers-reduced-motion", async () => {
	const server = new Server();
	server.items = [];
	server.attach();
	await renderApp("/agents/assistant/chat");
	const scroller = page.getByTestId("chat-messages");
	await expect.element(page.getByText("Message 39")).toBeVisible();

	// The click and the measurement run in one task, before any smooth
	// scroll could advance.
	const clickAndMeasure = () => {
		const el = scroller.element();
		el.scrollTop = 0;
		el.dispatchEvent(new Event("scroll"));
		return new Promise<number>((resolve) =>
			requestAnimationFrame(() => {
				(
					document.querySelector("[data-testid=chat-more-below]") as HTMLElement
				).click();
				resolve(el.scrollHeight - el.scrollTop - el.clientHeight);
			}),
		);
	};

	await commands.emulateMedia({ reducedMotion: "no-preference" });
	expect(await clickAndMeasure()).toBeGreaterThan(8);
	await expectAtBottom(scroller);

	await commands.emulateMedia({ reducedMotion: "reduce" });
	expect(await clickAndMeasure()).toBeLessThanOrEqual(8);
});

test("a following chat stays at the bottom when the on-screen keyboard opens and closes", async () => {
	await page.viewport(390, 844);
	const server = new Server();
	server.items = [];
	server.attach();
	await renderApp("/agents/assistant/chat");
	const scroller = page.getByTestId("chat-messages");
	await expect.element(page.getByText("Message 39")).toBeVisible();
	await expectAtBottom(scroller);

	// The keyboard takes the bottom of the viewport, then gives it back.
	await page.viewport(390, 500);
	await expectAtBottom(scroller);
	await page.viewport(390, 844);
	await expectAtBottom(scroller);
});

test("the system log pane follows, pauses with the pill, and keeps its place loading older lines", async () => {
	const entry = (seq: number) => ({
		seq,
		ts: NOW,
		level: "info",
		component: "server",
		msg: `log line ${seq}`,
		off: seq * 100,
	});
	route(/\/api\/logs\/history\?/, (r) =>
		r.fulfill({
			json: {
				entries: Array.from({ length: 50 }, (_, i) => entry(100 + i)),
				hasMore: true,
			},
		}),
	);
	mockMCP({ config_validate: [], agent_list: [] });
	await renderApp("/system/logs");
	// One stream of live lines.
	await expect
		.poll(() =>
			eventSourcesOpened().find(
				(es) => new URL(es.url).pathname === "/api/logs" && es.readyState === 1,
			),
		)
		.toBeTruthy();
	const es = eventSourcesOpened().find(
		(s) => new URL(s.url).pathname === "/api/logs",
	);
	for (let i = 0; i < 60; i++) es?.emit(JSON.stringify(entry(200 + i)));

	const scroller = page.getByTestId("logs-scroll");
	await expect.element(scroller.getByText("log line 259")).toBeVisible();
	await expectAtBottom(scroller);

	scrollToTop(scroller);
	await expect.element(page.getByTestId("logs-more-below")).toBeVisible();
	await expectPillUncovered(page.getByTestId("logs-more-below"));

	// Older lines load above without moving what's on screen.
	const first = scroller.getByText("log line 200", { exact: true });
	await expect.element(first).toBeInViewport();
	await scroller.getByRole("button", { name: /Load previous/ }).click();
	await expect
		.element(scroller.getByText("log line 149", { exact: true }))
		.toBeInTheDocument();
	await expect.poll(() => scroller.element().scrollTop).toBeGreaterThan(100);
	await expect.element(first).toBeInViewport();

	await page.getByTestId("logs-more-below").click();
	await expectAtBottom(scroller);
	await expect
		.element(page.getByTestId("logs-more-below"))
		.not.toBeInTheDocument();
});
