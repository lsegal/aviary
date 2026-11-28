// Ported from web/e2e/projects.spec.ts (#1636): the job detail pane: transcript, clone, run view, commands and layout.
import { expect, onTestFinished, test } from "vitest";
import { type Locator, page } from "vitest/browser";
import { currentURL, goto, reload, renderApp } from "../component-test/app";
import { declarePages } from "../component-test/routes";
import { boundingBoxOf } from "./helpers/box";
import { at, EVENTS, JOBS, RAW_OUTPUT, setup } from "./projects-fixtures";

// The pages this file renders (see component-test/routes.ts).
declarePages(
	() => import("../components/projects/ProjectJobDetail.vue"),
	() => import("../views/ProjectsView.vue"),
);

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

for (const width of [390, 1600]) {
	test(`job detail's rich transcript wraps instead of scrolling sideways at ${width}px (#835)`, async () => {
		const long = `${"x".repeat(400)} ${"/very/long/path".repeat(30)}`;
		EVENTS[221] = [
			{
				kind: "message",
				time: at(5),
				text: `Long \`${long}\`\n\n\`\`\`\n${long}\n\`\`\``,
			},
			{
				kind: "tool_call",
				time: at(4),
				call_id: "t1",
				name: "Read",
				input: { file_path: long },
			},
			{ kind: "tool_result", time: at(4), call_id: "t1", output: long },
			{
				kind: "file_change",
				time: at(3),
				path: "a.go",
				diff: `@@ -1 +1 @@\n-${long}\n+${long}`,
			},
			{
				kind: "command",
				time: at(2),
				call_id: "b1",
				command: `echo ${long}`,
				status: "running",
			},
			{
				kind: "command",
				time: at(2),
				call_id: "b1",
				output: long,
				exit_code: 0,
			},
		];
		try {
			await page.viewport(width, 900);
			setup();
			await renderApp("/agents/coder/projects/lsegal%2Faviary/jobs/221");
			const timeline = page.getByRole("list", { name: "Activity" });
			await expect
				.poll(
					() => timeline.locator('[data-event="command"]').elements().length,
				)
				.toBe(1);
			for (const summary of timeline.locator("summary").all()) {
				await summary.click();
			}
			await expect
				.poll(() => timeline.locator("details[open]").elements().length)
				.toBe(3);
			// Nothing scrolls sideways: not the pane, the list, or any scroller in it.
			const overflowing = await ((list) =>
				[list.parentElement as Element, list, ...list.querySelectorAll("*")]
					.filter(
						(el) =>
							(el === list ||
								el === list.parentElement ||
								/auto|scroll/.test(getComputedStyle(el).overflowX)) &&
							el.scrollWidth - el.clientWidth > 1,
					)
					.map((el) => el.outerHTML.slice(0, 100)))(
				timeline.element() as HTMLElement,
			);
			expect(overflowing).toEqual([]);
		} finally {
			delete EVENTS[221];
		}
	});
}

test("job detail shows the job's clone path, copyable, or why it has none", async () => {
	// A clipboard that keeps what it is given, in place of granting the
	// page clipboard permissions.
	let clipboard = "";
	Object.defineProperty(navigator, "clipboard", {
		configurable: true,
		value: {
			writeText: async (t: string) => {
				clipboard = t;
			},
			readText: async () => clipboard,
		},
	});
	onTestFinished(() => {
		Reflect.deleteProperty(navigator, "clipboard");
	});
	setup();
	const clone = page.getByTestId("job-clone");

	await renderApp("/agents/coder/projects/lsegal%2Faviary/jobs/218");
	await expect
		.element(page.getByTestId("job-clone-path"))
		.toHaveText("/tmp/aviary-gh-fix-200");
	await clone.getByRole("button", { name: "Copy clone path" }).click();
	await expect
		.element(clone.getByRole("button", { name: "Copied clone path" }))
		.toBeVisible();
	expect(await (() => navigator.clipboard.readText())()).toBe(
		"/tmp/aviary-gh-fix-200",
	);

	await goto("/agents/coder/projects/lsegal%2Faviary/jobs/221");
	await expect.element(clone).toMatchTextContent("No clone yet.");
	await expect.poll(() => clone.getByRole("button").elements().length).toBe(0);

	await goto("/agents/coder/projects/lsegal%2Faviary/jobs/209");
	await expect
		.element(clone)
		.toMatchTextContent("This job's clone was removed: /tmp/aviary-gh-fix-209");
	await expect.poll(() => clone.getByRole("button").elements().length).toBe(0);

	await goto("/agents/coder/projects/lsegal%2Faviary/jobs/200");
	await expect
		.element(clone)
		.toMatchTextContent(
			"This job's clone was handed to #218: /tmp/aviary-gh-fix-200",
		);
});

test("run view renders the typed event timeline read-only", async () => {
	setup();
	await renderApp("/agents/coder/projects/lsegal%2Faviary");
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
		.toHaveText(
			/^#218 • (Draft pull request)?PR #233 Cap scheduler retry backoff at 10 minutes$/,
		);

	const steps = page.getByTestId("lifecycle");
	await expect
		.element(steps.locator('[aria-current="step"]'))
		.toMatchTextContent("Implementing");

	const timeline = page.getByRole("list", { name: "Activity" });
	await expect.element(timeline.locator("strong")).toHaveText("without a cap");
	// Each command is one row whose badge carries its result.
	const commands = timeline.locator('[data-event="command"]');
	await expect.poll(() => commands.elements().length).toBe(2);
	await expect
		.element(commands.first().getByTestId("command-status"))
		.toHaveText("failed · exit 1");
	await expect
		.element(commands.last().getByTestId("command-status"))
		.toHaveText("ok");
	await expect.element(timeline).not.toMatchTextContent("running");
	await commands.first().locator("summary").click();
	await expect
		.element(commands.first())
		.toMatchTextContent("--- FAIL: TestBackoffCap");
	const edit = timeline.locator('[data-event="file_change"]');
	await expect
		.element(edit)
		.toMatchTextContent("edit internal/scheduler/queue.go");
	await expect.element(edit).toMatchTextContent("+2");
	await expect.element(edit).toMatchTextContent("-1");
	await expect
		.element(timeline.locator('[data-event="plan"]'))
		.toMatchTextContent("Clamp the delay");
	await timeline.getByText("Read", { exact: true }).click();
	await expect
		.element(timeline.locator('[data-event="tool_call"]'))
		.toMatchTextContent("package scheduler");
	await expect
		.element(timeline.locator('[data-event="comment_relay"]'))
		.toMatchTextContent("lsegal commented");
	await expect
		.element(timeline.locator('[data-event="comment_relay"]'))
		.toMatchTextContent("Keep the jitter, please.");
	await expect
		.element(timeline.locator('[data-event="usage"]'))
		.toMatchTextContent("$1.42");

	// Raw CLI stdout sits beside the timeline once opened.
	await page.getByTestId("raw-output-toggle").click();
	await expect
		.element(page.getByTestId("run-output"))
		.toMatchTextContent('"type":"assistant"');
	await expect
		.element(page.getByRole("link", { name: "Comment on #218 ↗" }))
		.toHaveAttribute(
			"href",
			"https://github.com/lsegal/aviary/issues/218#new_comment_field",
		);

	// The footer names who may steer the job by commenting.
	const commenters = page.getByTestId("allowed-commenters");
	await expect.element(commenters).toHaveText("@lsegal, @octocat");
	await expect
		.element(commenters.getByRole("link", { name: "@octocat" }))
		.toHaveAttribute("href", "https://github.com/octocat");

	// No steering: nothing on the page can send a message to the job.
	await expect.poll(() => page.getByRole("textbox").elements().length).toBe(0);
	await expect
		.poll(() => page.getByRole("button", { name: /send/i }).elements().length)
		.toBe(0);
});

test("the run view stepper follows the PR out of draft, like the board icon", async () => {
	// #218's events never mention review or CI, so only its PR state can move
	// the stepper past Implementing.
	const job218 = JOBS.find((j) => j.number === 218) as Record<string, unknown>;
	try {
		setup();
		const steps = page.getByTestId("lifecycle");
		await renderApp("/agents/coder/projects/lsegal%2Faviary/jobs/218");
		await expect
			.element(steps.locator('[aria-current="step"]'))
			.toMatchTextContent("Implementing");

		job218.pull_request_state = "open";
		await goto("/agents/coder/projects/lsegal%2Faviary");
		await expect
			.element(
				page
					.getByTestId("job-card")
					.filter({ hasText: "#218" })
					.getByTestId("pr-state-icon"),
			)
			.toHaveAttribute("aria-label", "Open pull request");

		await goto("/agents/coder/projects/lsegal%2Faviary/jobs/218");
		await expect
			.element(steps.locator('[aria-current="step"]'))
			.toMatchTextContent("CI");
		await expect.element(steps.locator("li").nth(3)).toMatchTextContent("✓");
		await expect
			.element(steps.locator("li").nth(5))
			.not.toMatchTextContent("✓");
	} finally {
		job218.pull_request_state = "draft";
	}
});

test("the run view stepper shows a merged PR as done", async () => {
	setup();
	await renderApp("/agents/coder/projects/lsegal%2Faviary/jobs/200");
	const steps = page.getByTestId("lifecycle");
	await expect
		.poll(() => steps.locator('[aria-current="step"]').elements().length)
		.toBe(0);
	await expect.element(steps.locator("li").nth(5)).toMatchTextContent("✓");
});

test("job detail commands are single rows whose badge resolves in place (#774)", async () => {
	const events: object[] = [
		{
			kind: "command",
			time: at(3),
			call_id: "r1",
			command: "pnpm lint",
			status: "running",
		},
	];
	EVENTS[221] = events;
	try {
		setup();
		await renderApp("/agents/coder/projects/lsegal%2Faviary/jobs/221");
		const rows = page
			.getByRole("list", { name: "Activity" })
			.locator('[data-event="command"]');
		await expect.poll(() => rows.elements().length).toBe(1);
		const badge = rows.first().getByTestId("command-status");
		await expect.element(badge).toHaveText("running");

		events.push({
			kind: "command",
			time: at(2),
			call_id: "r1",
			output: "Checked 107 files",
			exit_code: 0,
			status: "done",
		});
		await expect.element(badge).toHaveText("ok");
		await expect.element(badge).toHaveAttribute("title", "Succeeded");
		await expect.poll(() => rows.elements().length).toBe(1);

		events.push(
			{
				kind: "command",
				time: at(1),
				call_id: "r2",
				command: "go test ./...",
				status: "running",
			},
			{
				kind: "command",
				time: at(1),
				call_id: "r2",
				output: "FAIL",
				exit_code: 2,
				is_error: true,
				status: "done",
			},
		);
		await expect.poll(() => rows.elements().length).toBe(2);
		const failed = rows.last().getByTestId("command-status");
		await expect.element(failed).toHaveText("failed · exit 2");
		await expect
			.element(failed)
			.toHaveAttribute("title", "Failed with exit code 2");
		await expect
			.poll(() => page.getByText(/^exit \d/).elements().length)
			.toBe(0);
	} finally {
		delete EVENTS[221];
	}
});

test("job detail's raw Output starts collapsed, toggles, remembers, and hides heartbeats (#774)", async () => {
	RAW_OUTPUT[221] = [
		'{"type":"assistant","message":{"content":[{"type":"text","text":"Working"}]}}',
		'{"type":"tool_progress","tool_use_id":"t1","heartbeat":true,"elapsed_time_seconds":30}',
	].join("\n");
	try {
		setup();
		await renderApp("/agents/coder/projects/lsegal%2Faviary/jobs/221");
		const toggle = page.getByTestId("raw-output-toggle");
		const panel = page.getByTestId("raw-output");
		await expect
			.element(page.getByRole("list", { name: "Activity" }))
			.toBeVisible();
		await expect.poll(() => panel.elements().length).toBe(0);
		await expect.element(toggle).toHaveAttribute("aria-pressed", "false");

		await toggle.click();
		await expect.element(panel).toBeVisible();
		const output = page.getByTestId("run-output");
		await expect.element(output).toMatchTextContent('"text":"Working"');
		await expect.element(output).not.toMatchTextContent("tool_progress");
		await page.getByTestId("raw-output-heartbeats").click();
		await expect
			.element(page.getByTestId("raw-output-heartbeats"))
			.toBeChecked();
		await expect.element(output).toMatchTextContent('"heartbeat":true');

		await reload();
		await expect.element(panel).toBeVisible();
		await expect.element(toggle).toHaveAttribute("aria-pressed", "true");

		await toggle.click();
		await expect.poll(() => panel.elements().length).toBe(0);
		await reload();
		await expect
			.element(page.getByRole("list", { name: "Activity" }))
			.toBeVisible();
		await expect.poll(() => panel.elements().length).toBe(0);
	} finally {
		delete RAW_OUTPUT[221];
	}
});

test("job detail shows the clone with session and agent above the stepper (#774)", async () => {
	setup();
	await renderApp("/agents/coder/projects/lsegal%2Faviary/jobs/218");
	const meta = page.getByTestId("job-meta");
	await expect.element(meta.getByTestId("job-session")).toHaveText("7f3c…a9");
	await expect
		.element(meta.getByTestId("job-agent"))
		.toMatchTextContent("claude");
	await expect.element(meta.getByTestId("job-clone-path")).toBeVisible();
	const clone = await boundingBoxOf(meta.getByTestId("job-clone"));
	const steps = await boundingBoxOf(page.getByTestId("lifecycle"));
	expect(clone && steps && clone.y + clone.height <= steps.y).toBe(true);
});

for (const { name, width, height, compact } of [
	{ name: "mobile", width: 390, height: 844, compact: true },
	{ name: "narrow desktop", width: 820, height: 800, compact: true },
	{ name: "wide desktop", width: 1600, height: 900, compact: false },
]) {
	test(`the job detail stepper never scrolls sideways at ${name} width (#774)`, async () => {
		await page.viewport(width, height);
		setup();
		await renderApp("/agents/coder/projects/lsegal%2Faviary/jobs/218");
		const steps = page.getByTestId("lifecycle");
		await expect
			.element(steps.locator('[aria-current="step"]'))
			.toMatchTextContent("Implementing");
		const overflow = await ((el) => el.scrollWidth - el.clientWidth)(
			steps.element() as HTMLElement,
		);
		expect(overflow).toBeLessThanOrEqual(0);
		const summary = page.getByTestId("lifecycle-compact");
		if (compact) {
			await expect.element(summary).toBeVisible();
			await expect.element(summary).toHaveText("3/6 · Implementing");
		} else {
			await expectHidden(summary);
		}
		await expect
			.element(steps.locator("li").nth(2))
			.toHaveAttribute("title", "Step 3 of 6: Implementing (current)");
		await expect
			.element(steps.locator("li").nth(0))
			.toHaveAttribute("data-step", "done");
	});
}

for (const { name, width, height } of [
	{ name: "mobile", width: 390, height: 844 },
	{ name: "wide desktop", width: 1600, height: 900 },
]) {
	test(`the job detail page never scrolls past its content at ${name} width (#854)`, async () => {
		// Each tool call carries a visually hidden, absolutely positioned
		// status label, which must stay inside the transcript's scroller.
		EVENTS[221] = Array.from({ length: 80 }, (_, i) => [
			{
				kind: "tool_call",
				time: at(3),
				call_id: `t${i}`,
				name: "Read",
				input: { file_path: `file${i}.go` },
			},
			{ kind: "tool_result", time: at(3), call_id: `t${i}`, output: "ok" },
		]).flat();
		try {
			await page.viewport(width, height);
			setup();
			await renderApp("/agents/coder/projects/lsegal%2Faviary/jobs/221");
			const calls = page
				.getByRole("list", { name: "Activity" })
				.locator('[data-event="tool_call"]');
			await expect.poll(() => calls.elements().length).toBe(80);
			const overscroll = await (() => {
				const doc = document.scrollingElement ?? document.documentElement;
				return doc.scrollHeight - doc.clientHeight;
			})();
			expect(overscroll).toBeLessThanOrEqual(0);

			// The transcript itself still scrolls to its last entry and footer.
			calls.last().element().scrollIntoView({ block: "nearest" });
			await expect.element(calls.last()).toBeInViewport();
			const footer = page.getByText("Jobs only take instructions");
			footer.element().scrollIntoView({ block: "nearest" });
			await expect.element(footer).toBeInViewport();
		} finally {
			delete EVENTS[221];
		}
	});
}
