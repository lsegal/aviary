// Ported from web/e2e/projects.spec.ts (#1636): the prompt chat and logs panels, and job details beside them.
import { beforeEach, describe, expect, test } from "vitest";
import { commands, type Locator, page, userEvent } from "vitest/browser";
import {
	currentURL,
	goBack,
	goForward,
	goto,
	reload,
	renderApp,
} from "../component-test/app";
import { headerSettled } from "./helpers/boardHeader";
import { boundingBoxOf } from "./helpers/box";
import {
	type Calls,
	chatFixtures,
	STATUS,
	setup,
	stateChip,
} from "./projects-fixtures";

/** Playwright's isVisible: attached, with a box, and not hidden. */
function isVisible(locator: Locator): boolean {
	const el = locator.query();
	if (!el) return false;
	return (
		el.getClientRects().length > 0 &&
		getComputedStyle(el).visibility !== "hidden"
	);
}

/** Playwright's toBeHidden: absent, or present but not visible. */
async function expectHidden(locator: Locator) {
	await expect.poll(() => isVisible(locator)).toBe(false);
}

test("prompt chat opens beside the board and remembers it is open", async () => {
	const calls: Calls = {};
	Object.assign(calls, setup(undefined, chatFixtures(calls)));
	await renderApp("/agents/coder/projects/lsegal%2Faviary");

	await expect
		.poll(() => page.getByTestId("project-chat").elements().length)
		.toBe(0);
	await page.getByTestId("prompt-chat-toggle").click();
	const chat = page.getByTestId("project-chat");
	await expect.element(chat).toBeVisible();
	await expect
		.element(chat)
		.toMatchTextContent("Ask the projects agent about lsegal/aviary");
	// It talks to the built-in Code Loop agent's one hidden session, never to
	// a job, and offers no other chats (#624).
	await expect
		.poll(() => calls.session_messages?.at(-1))
		.toMatchObject({ agent: "projects", session_id: "projects-chat" });
	await expect
		.poll(() => chat.getByTestId("chat-picker").elements().length)
		.toBe(0);
	await expect
		.poll(
			() => chat.getByRole("button", { name: "New chat" }).elements().length,
		)
		.toBe(0);
	// The sidebar lists the sole agent's own chats; none are the Projects
	// agent's.
	expect(
		(calls.session_list ?? []).filter((c) => c.agent === "projects"),
	).toHaveLength(0);
	// Images show in both directions.
	await expect.poll(() => chat.locator("img").elements().length).toBe(2);
	await expect.element(chat).toMatchTextContent("Here is the failing run.");
	// The board's cards stay visible beside the panel.
	await expect.element(page.getByTestId("job-card").first()).toBeVisible();

	// Dragging the handle widens the panel, and both survive a reload.
	const before = (await boundingBoxOf(chat))?.width ?? 0;
	const handle = page.getByTestId("project-chat-resize");
	const box = await boundingBoxOf(handle);
	if (!box) throw new Error("no resize handle");
	await commands.mouseMove(box.x + box.width / 2, box.y + 100);
	await commands.mouseDown();
	await commands.mouseMove(box.x - 80, box.y + 100);
	await commands.mouseUp();
	const after = (await boundingBoxOf(chat))?.width ?? 0;
	expect(after).toBeGreaterThan(before + 60);

	await reload();
	await expect.element(page.getByTestId("project-chat")).toBeVisible();
	expect(
		(await boundingBoxOf(page.getByTestId("project-chat")))?.width,
	).toBeCloseTo(after, 0);

	await page.getByRole("button", { name: "Close prompt chat" }).click();
	await expect
		.poll(() => page.getByTestId("project-chat").elements().length)
		.toBe(0);
	await reload();
	await expect
		.element(page.getByRole("heading", { name: "lsegal/aviary" }))
		.toBeVisible();
	await expect
		.poll(() => page.getByTestId("project-chat").elements().length)
		.toBe(0);
});

test("prompt chat previews a pending image when it is clicked", async () => {
	const calls: Calls = {};
	Object.assign(calls, setup(undefined, chatFixtures(calls)));
	await renderApp("/agents/coder/projects/lsegal%2Faviary");
	await page.getByTestId("prompt-chat-toggle").click();
	const chat = page.getByTestId("project-chat");
	const png =
		"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAADElEQVR4nGP4z8AAAAMBAQDJ/pLvAAAAAElFTkSuQmCC";
	// The Attach files button opens the file input's chooser; set its files
	// directly, as the chooser would.
	await expect
		.element(chat.getByRole("button", { name: "Attach files" }))
		.toBeVisible();
	const bytes = Uint8Array.from(atob(png), (c) => c.charCodeAt(0));
	await userEvent.upload(
		chat.locator('input[type="file"]'),
		new File([bytes], "board.png", { type: "image/png" }),
	);

	await chat.getByRole("button", { name: "Preview board.png" }).click();
	const preview = page.getByTestId("image-preview");
	await expect
		.element(preview.getByTestId("image-preview-img"))
		.toHaveAttribute("src", `data:image/png;base64,${png}`);
	// Esc closes only the preview, leaving the chat and its attachment.
	await commands.keyPress("Escape");
	await expect.poll(() => preview.elements().length).toBe(0);
	await expect.element(chat).toBeVisible();
	await expect
		.poll(() => chat.getByTestId("attachment-image").elements().length)
		.toBe(1);
});

test("logs panel docks below the board, streams lines, and resizes", async () => {
	// Each status poll after the first adds a log line.
	let polls = 0;
	setup(() => {
		polls++;
		const extra = Array.from(
			{ length: Math.max(0, polls - 1) },
			(_, i) => `12:05:${String(i).padStart(2, "0")} live line ${i + 1}`,
		);
		return {
			...STATUS,
			recent_logs: [STATUS.recent_logs, ...extra].join("\n"),
		};
	});
	await renderApp("/agents/coder/projects/lsegal%2Faviary");

	// The log lines no longer sit under the job cards.
	await expect.element(page.getByTestId("job-card").first()).toBeVisible();
	await expect
		.poll(() => page.getByText("Project log").elements().length)
		.toBe(0);
	await expect
		.poll(() => page.getByTestId("project-logs").elements().length)
		.toBe(0);

	// The Logs button sits just left of the Prompt chat button.
	const toggle = page.getByTestId("logs-toggle");
	await expect.element(toggle).toHaveAttribute("title", "Logs");
	await expect.element(toggle).toHaveAttribute("aria-pressed", "false");
	const toggleBox = await boundingBoxOf(toggle);
	const chatBox = await boundingBoxOf(page.getByTestId("prompt-chat-toggle"));
	if (!toggleBox || !chatBox) throw new Error("no header buttons");
	expect(toggleBox.x).toBeLessThan(chatBox.x);

	await toggle.click();
	const panel = page.getByTestId("project-logs");
	await expect.element(panel).toBeVisible();
	await expect.element(toggle).toHaveAttribute("aria-pressed", "true");
	await expect.element(panel).toMatchTextContent("claimed #221");
	// It docks at the bottom of the board.
	const viewport = { height: window.innerHeight };
	const panelBox = await boundingBoxOf(panel);
	if (!viewport || !panelBox) throw new Error("no panel");
	expect(panelBox.y + panelBox.height).toBeCloseTo(viewport.height, 0);

	// New lines stream in, and the view stays pinned to the newest one.
	await expect
		.element(panel, { timeout: 10_000 })
		.toMatchTextContent("live line 2");
	const lines = page.getByTestId("project-logs-lines");
	await expect
		.poll(() =>
			((el) => el.scrollHeight - el.scrollTop - el.clientHeight)(
				lines.element() as HTMLElement,
			),
		)
		.toBeLessThan(2);

	// Dragging the top edge up makes the panel taller, and the height and
	// open state survive a reload.
	const before = panelBox.height;
	const handle = page.getByTestId("project-logs-resize");
	const box = await boundingBoxOf(handle);
	if (!box) throw new Error("no resize handle");
	await commands.mouseMove(box.x + 200, box.y + box.height / 2);
	await commands.mouseDown();
	await commands.mouseMove(box.x + 200, box.y - 100);
	await commands.mouseUp();
	const after = (await boundingBoxOf(panel))?.height ?? 0;
	expect(after).toBeGreaterThan(before + 80);

	await reload();
	await expect.element(page.getByTestId("project-logs")).toBeVisible();
	expect(
		(await boundingBoxOf(page.getByTestId("project-logs")))?.height,
	).toBeCloseTo(after, 0);

	// Clicking Logs again closes it, and it stays closed after a reload.
	await page.getByTestId("logs-toggle").click();
	await expect
		.poll(() => page.getByTestId("project-logs").elements().length)
		.toBe(0);
	await expect
		.element(page.getByTestId("logs-toggle"))
		.toHaveAttribute("aria-pressed", "false");
	await reload();
	await expect
		.element(page.getByRole("heading", { name: "lsegal/aviary" }))
		.toBeVisible();
	await expect
		.poll(() => page.getByTestId("project-logs").elements().length)
		.toBe(0);
});

test("logs panel pauses following with the pill when scrolled up (#1072)", async () => {
	let polls = 0;
	const old = Array.from(
		{ length: 40 },
		(_, i) => `12:00:${String(i).padStart(2, "0")} old line ${i + 1}`,
	);
	setup(() => {
		polls++;
		const extra = Array.from(
			{ length: polls },
			(_, i) => `12:05:${String(i).padStart(2, "0")} live line ${i + 1}`,
		);
		return { ...STATUS, recent_logs: [...old, ...extra].join("\n") };
	});
	await renderApp("/agents/coder/projects/lsegal%2Faviary");
	await page.getByTestId("logs-toggle").click();
	const lines = page.getByTestId("project-logs-lines");
	const pill = page.getByTestId("project-logs-more-below");
	const gap = () =>
		((el) => el.scrollHeight - el.scrollTop - el.clientHeight)(
			lines.element() as HTMLElement,
		);
	await expect.element(lines).toMatchTextContent("old line 40");
	await expect.poll(gap).toBeLessThanOrEqual(8);

	await ((el) => {
		el.scrollTop = 0;
	})(lines.element() as HTMLElement);
	await expect.element(pill).toBeVisible();
	const seen = polls;
	await expect
		.element(lines, {
			timeout: 10_000,
		})
		.toMatchTextContent(`live line ${seen + 1}`);
	expect(await ((el) => el.scrollTop)(lines.element() as HTMLElement)).toBe(0);
	await expect.element(pill).toMatchTextContent("new · More below ↓");

	await pill.click();
	await expect.poll(gap).toBeLessThanOrEqual(8);
	await expectHidden(pill);
});

test("prompt chat inserts a newline on Shift+Enter and sends on Enter", async () => {
	const calls: Calls = {};
	Object.assign(calls, setup(undefined, chatFixtures(calls)));
	await renderApp("/agents/coder/projects/lsegal%2Faviary");
	await page.getByTestId("prompt-chat-toggle").click();
	const chat = page.getByTestId("project-chat");
	const input = chat.getByPlaceholder("Describe a task or ask a question");
	await expect.element(input).toBeEnabled();

	await input.fill("line one");
	const before = (await boundingBoxOf(input))?.height ?? 0;
	(input.element() as HTMLElement).focus();
	await commands.keyPress("Shift+Enter");
	await commands.keyType("line two");
	await expect.element(input).toHaveValue("line one\nline two");
	await expect
		.poll(async () => (await boundingBoxOf(input))?.height ?? 0)
		.toBeGreaterThan(before);
	expect(calls.agent_run ?? []).toHaveLength(0);

	(input.element() as HTMLElement).focus();
	await commands.keyPress("Enter");
	await expect.poll(() => calls.agent_run?.length ?? 0).toBe(1);
	expect(calls.agent_run[0].message).toBe("line one\nline two");
	await expect.element(input).toHaveValue("");
	const sent = chat
		.getByTestId("chat-message-content")
		.filter({ hasText: "line two" });
	await expect.element(sent).toBeVisible();
	// biome-ignore lint/performance/useDomNodeTextContent: the rendered line breaks are the point, as in the e2e innerText check.
	expect((sent.element() as HTMLElement).innerText).toBe("line one\nline two");
});

test("job details open under the project header, keeping the chat and logs panels", async () => {
	const calls: Calls = {};
	Object.assign(calls, setup(undefined, chatFixtures(calls)));
	await renderApp("/agents/coder/projects/lsegal%2Faviary");
	await page.getByTestId("prompt-chat-toggle").click();
	await page.getByTestId("logs-toggle").click();
	const chat = page.getByTestId("project-chat");
	const input = chat.getByPlaceholder("Describe a task or ask a question");
	await expect.element(chat).toMatchTextContent("Here is the failing run.");
	await input.fill("what about #218?");
	await expect.element(page.getByTestId("project-logs")).toBeVisible();

	await page
		.getByTestId("job-card")
		.filter({ hasText: "#218" })
		.getByRole("link", { name: "Details" })
		.click();
	await expect
		.poll(currentURL)
		.toBe("/agents/coder/projects/lsegal%2Faviary/jobs/218");
	await expect
		.element(page.getByTestId("job-detail-title"))
		.toMatchTextContent(
			/#218 • (Draft pull request)?PR #233 Cap scheduler retry backoff at 10 minutes/,
		);
	// The shared header names the job under its project, and the project's
	// buttons stay.
	await expect
		.element(
			page
				.getByTestId("settings-breadcrumb")
				.getByRole("link", { name: "lsegal/aviary", exact: true }),
		)
		.toBeVisible();
	await expect.element(page.getByTestId("settings-title")).toHaveText("#218");
	await expect
		.poll(() => page.getByTestId("delivery-status").elements().length)
		.toBe(0);
	await expect.element(page.getByTestId("board-more-menu")).toBeVisible();
	await expect.element(page.getByTestId("logs-toggle")).toBeVisible();
	// The open panels keep their content and the unsent draft.
	await expect.element(chat).toMatchTextContent("Here is the failing run.");
	await expect.element(input).toHaveValue("what about #218?");
	await expect.element(page.getByTestId("project-logs")).toBeVisible();

	// Switching jobs and closing details keeps them too.
	await goto("/agents/coder/projects/lsegal%2Faviary");
	await page
		.getByTestId("job-card")
		.filter({ hasText: "#218" })
		.getByRole("link", { name: "Details" })
		.click();
	await page.getByTestId("job-detail-close").click();
	await expect.poll(currentURL).toBe("/agents/coder/projects/lsegal%2Faviary");
	await expect.element(page.getByTestId("job-card").first()).toBeVisible();
	await expect.element(chat).toBeVisible();
	await expect.element(page.getByTestId("project-logs")).toBeVisible();
	expect(calls.agent_run ?? []).toHaveLength(0);
});

test("closing job details returns to the board with its filters and search", async () => {
	setup();
	await renderApp("/agents/coder/projects/lsegal%2Faviary");
	const search = page.getByRole("searchbox", { name: "Search jobs" });
	await search.fill("scheduler");
	await stateChip("Running").click();
	const card = page.getByTestId("job-card").filter({ hasText: "#218" });
	await expect.element(card).toBeVisible();

	await card.getByRole("link", { name: "Details" }).click();
	await expect.element(page.getByTestId("job-detail")).toBeVisible();
	await expect.poll(() => search.elements().length).toBe(0);
	await page.getByTestId("job-detail-close").click();
	await expect.element(search).toHaveValue("scheduler");
	await expect
		.element(stateChip("Running"))
		.toHaveAttribute("aria-pressed", "true");

	// Browser back and forward move between the board and the details
	// without reloading the page.
	await (() => {
		(window as unknown as { marker: boolean }).marker = true;
	})();
	await goBack();
	await expect.element(page.getByTestId("job-detail")).toBeVisible();
	await goForward();
	await expect
		.poll(() => page.getByTestId("job-detail").elements().length)
		.toBe(0);
	await expect.element(search).toHaveValue("scheduler");
	expect(
		await (() => (window as unknown as { marker?: boolean }).marker)(),
	).toBe(true);
});

test("loading a job details URL shows the project header with that job open", async () => {
	setup();
	await renderApp("/agents/coder/projects/lsegal%2Faviary/jobs/218");
	await expect
		.element(
			page
				.getByTestId("settings-breadcrumb")
				.getByRole("link", { name: "lsegal/aviary", exact: true }),
		)
		.toBeVisible();
	await expect.element(page.getByTestId("prompt-chat-toggle")).toBeVisible();
	await expect.element(page.getByTestId("logs-toggle")).toBeVisible();
	await expect
		.element(page.getByTestId("job-detail-title"))
		.toHaveText(
			/^#218 • (Draft pull request)?PR #233 Cap scheduler retry backoff at 10 minutes$/,
		);
	await expect
		.poll(() => page.getByTestId("job-card").elements().length)
		.toBe(0);
});

test("logs panel shows project_status recent_logs and updates live", async () => {
	// project_status returns recent_logs as an array of dated lines. None have
	// been logged yet; the test logs them one at a time.
	const logged: string[] = [];
	setup(() => ({
		...STATUS,
		recent_logs: [...logged],
		log_offset: 650 + logged.length,
	}));
	await renderApp("/agents/coder/projects/lsegal%2Faviary");
	await page.getByTestId("logs-toggle").click();
	const panel = page.getByTestId("project-logs");
	await expect.element(panel).toMatchTextContent("No log lines yet.");

	// Lines appear without a reload, and the placeholder goes away.
	logged.push("2026-11-21 11:02:00 issue #737 picked up");
	await expect
		.element(panel, {
			timeout: 10_000,
		})
		.toMatchTextContent("issue #737 picked up");
	await expect.element(panel).not.toMatchTextContent("No log lines yet.");
	logged.push("2026-11-21 11:02:01 issue #738 picked up");
	await expect
		.element(panel, {
			timeout: 10_000,
		})
		.toMatchTextContent("issue #738 picked up");
	await expect
		.poll(() => page.getByTestId("project-logs-entry").elements().length)
		.toBe(2);
	await expect
		.element(
			page.getByTestId("project-logs-entry").first().locator("span").first(),
		)
		.toHaveText("2026-11-21 11:02:00");
});

test("logs panel wraps long lines and indents multi-line entries", async () => {
	const url = `https://github.com/lsegal/aviary/issues?q=${"x".repeat(300)}`;
	setup(() => ({
		...STATUS,
		recent_logs: [
			`2026-11-20 12:03:58 handoff poll #2 error: read issue list at ${url} the issue list did not render within 15s`,
			"2026-11-20 12:04:02 prompt agent (claude/opus) filing an issue on lsegal/aviary",
			"## Summary",
			"Body line two",
			"12:04:11 claimed #221",
		].join("\n"),
	}));
	await renderApp("/agents/coder/projects/lsegal%2Faviary");
	await page.getByTestId("logs-toggle").click();
	const lines = page.getByTestId("project-logs-lines");
	await expect.element(lines).toMatchTextContent("claimed #221");

	// Nothing overflows sideways, not even the unbroken URL.
	expect(
		await ((el) => el.scrollWidth - el.clientWidth)(
			lines.element() as HTMLElement,
		),
	).toBeLessThanOrEqual(0);
	const entries = page.getByTestId("project-logs-entry");
	await expect.poll(() => entries.elements().length).toBe(3);
	const first = entries.nth(0);
	expect(
		await ((el) => el.scrollWidth - el.clientWidth)(
			first.element() as HTMLElement,
		),
	).toBe(0);
	await expect.element(first).toMatchTextContent(url);

	// Continuation lines stay in their entry, keep their line breaks, and line
	// up after the timestamp rather than under it.
	const second = entries.nth(1);
	const text = second.locator("span").nth(1);
	// biome-ignore lint/performance/useDomNodeTextContent: the rendered line breaks are the point, as in the e2e innerText check.
	expect((text.element() as HTMLElement).innerText).toBe(
		"prompt agent (claude/opus) filing an issue on lsegal/aviary\n## Summary\nBody line two",
	);
	const timeBox = await boundingBoxOf(second.locator("span").first());
	const textBox = await boundingBoxOf(text);
	if (!timeBox || !textBox) throw new Error("no entry boxes");
	expect(textBox.x).toBeGreaterThan(timeBox.x + timeBox.width);
	const oneLine = (await boundingBoxOf(entries.nth(2)))?.height ?? 0;
	expect(textBox.height).toBeGreaterThan(oneLine * 2.5);
	await expect
		.element(second.locator("span").first())
		.toHaveText("2026-11-20 12:04:02");
	await expect
		.element(entries.nth(2).locator("span").first())
		.toHaveText("12:04:11");
});

test("Summarize sends the job to the prompt chat", async () => {
	const calls: Calls = {};
	Object.assign(calls, setup(undefined, chatFixtures(calls)));
	await renderApp("/agents/coder/projects/lsegal%2Faviary");

	const card = page.getByTestId("job-card").filter({ hasText: "#218" });
	await card.getByTestId("job-summarize").click();
	const chat = page.getByTestId("project-chat");
	await expect.element(chat).toBeVisible();
	await expect.poll(() => calls.agent_run?.length ?? 0).toBe(1);
	expect(calls.agent_run[0]).toMatchObject({
		name: "projects",
		message: "Summarize the current session log of work on lsegal/aviary#218",
		// The board's project goes with every prompt (#516).
		project: "lsegal/aviary",
		session: "projects-chat",
	});
	expect(calls.agent_run[0].session_id).toBeUndefined();
	await expect
		.element(chat)
		.toMatchTextContent(
			"Summarize the current session log of work on lsegal/aviary#218",
		);
	await expect
		.element(chat)
		.toMatchTextContent("#218 is clamping the retry delay.");
});

test("every board shares one prompt chat log with its own project hint", async () => {
	const calls: Calls = {};
	Object.assign(calls, setup(undefined, chatFixtures(calls)));
	await renderApp("/agents/coder/projects/lsegal%2Faviary");
	await page.getByTestId("prompt-chat-toggle").click();
	let chat = page.getByTestId("project-chat");
	await expect.element(chat).toMatchTextContent("about lsegal/aviary");
	const input = chat.getByPlaceholder("Describe a task or ask a question");
	await input.fill("What is stuck here?");
	(input.element() as HTMLElement).focus();
	await commands.keyPress("Enter");
	await expect.poll(() => calls.agent_run?.length ?? 0).toBe(1);
	await expect.element(chat).toMatchTextContent("What is stuck here?");

	// The all-projects board opens the same log, now about every project.
	await goto("/agents/coder/projects");
	chat = page.getByTestId("project-chat");
	await expect.element(chat).toMatchTextContent("about every project");
	await expect.element(chat).toMatchTextContent("Here is the failing run.");
	await expect.element(chat).toMatchTextContent("What is stuck here?");
	await chat
		.getByPlaceholder("Describe a task or ask a question")
		.fill("And everywhere?");
	(
		chat
			.getByPlaceholder("Describe a task or ask a question")
			.element() as HTMLElement
	).focus();
	await commands.keyPress("Enter");
	await expect.poll(() => calls.agent_run?.length ?? 0).toBe(2);

	// Only the project hint changed; both prompts went to the one session.
	expect(calls.agent_run[0]).toMatchObject({
		session: "projects-chat",
		project: "lsegal/aviary",
	});
	expect(calls.agent_run[1].session).toBe("projects-chat");
	expect(calls.agent_run[1].project).toBeUndefined();
	expect(
		new Set((calls.session_messages ?? []).map((a) => a.session_id)),
	).toEqual(new Set(["projects-chat"]));
});

describe("phone width", () => {
	beforeEach(async () => {
		await page.viewport(390, 844);
	});

	for (const path of [
		"/agents/coder/projects/lsegal%2Faviary",
		"/agents/coder/projects/lsegal%2Faviary/jobs/218",
	]) {
		test(`${path} has no horizontal overflow`, async () => {
			setup();
			await renderApp(path);
			await expect.element(page.getByTestId("settings-title")).toBeVisible();
			await new Promise((r) => setTimeout(r, 500));
			const overflow = await (() =>
				[...document.querySelectorAll("main *")].some(
					(el) =>
						el.getBoundingClientRect().right > window.innerWidth + 1 &&
						getComputedStyle(el).position !== "fixed" &&
						!el.closest("[class*='overflow-x-auto']") &&
						!el.closest("pre"),
				))();
			expect(overflow).toBe(false);
		});
	}

	test("prompt chat opens as a bottom drawer", async () => {
		setup(undefined, chatFixtures({}));
		await renderApp("/agents/coder/projects/lsegal%2Faviary");
		// At 390px the chat toggle is in "…" to keep the header on one line
		// (#1576).
		await headerSettled();
		await page.getByTestId("board-more-menu").click();
		await page.getByRole("menuitemcheckbox", { name: "Prompt chat" }).click();
		const chat = page.getByTestId("project-chat");
		await expect.element(chat).toBeVisible();
		const box = await boundingBoxOf(chat);
		if (!box) throw new Error("no prompt chat");
		expect(box.width).toBeCloseTo(390, 0);
		expect(box.y + box.height).toBeCloseTo(844, 0);
		expect(box.y).toBeGreaterThan(100);
	});
});
