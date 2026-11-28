// Ported from web/e2e/projects.spec.ts (#1636): job cards' rich transcript view.
import { beforeEach, expect, test } from "vitest";
import { commands, type Locator, page } from "vitest/browser";
import { reload, renderApp } from "../component-test/app";
import { declarePages } from "../component-test/routes";
import { useJobOutputMode } from "../composables/useJobOutput";
import { at, jobGetWithEvents, STATUS, setup } from "./projects-fixtures";

// The pages this file renders (see component-test/routes.ts).
declarePages(
	() => import("../components/projects/ProjectJobDetail.vue"),
	() => import("../views/ProjectsView.vue"),
);

// The rich/raw choice is module state that a fresh page would read from the
// (cleared) storage, so each test starts on the default rich view.
beforeEach(() => {
	useJobOutputMode().value = "rich";
});

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

test("job cards show the transcript in a rich view by default, falling back to raw (#769)", async () => {
	const calls = setup();
	await renderApp("/agents/coder/projects/lsegal%2Faviary");
	const card = page.getByTestId("job-card").filter({ hasText: "#218" });
	const rich = card.getByTestId("job-output-rich");
	await expect.element(rich).toBeVisible();
	await expect
		.poll(() => card.getByTestId("job-output").elements().length)
		.toBe(0);
	// Cards read enough events to fill their output area, with the job list
	// itself rather than one project_job_get per card (#1657).
	expect(calls.project_job_list?.at(-1)?.output_events).toBe(40);
	expect(calls.project_job_get).toBeUndefined();

	// Agent messages render as markdown.
	await expect
		.element(rich.locator('[data-row="message"] strong'))
		.toHaveText("without a cap");
	// Each tool call is one row with its summary and a status badge, its
	// full text in a tooltip.
	const read = rich.locator('[data-call-id="c1"]');
	await expect.poll(() => read.elements().length).toBe(1);
	await expect
		.element(read)
		.toMatchTextContent("Read internal/scheduler/queue.go");
	await expect.element(read.getByTestId("tool-status")).toHaveText("✓ ok");
	await expect
		.element(read.locator("span.font-mono"))
		.toHaveAttribute("title", "Read internal/scheduler/queue.go");
	const commands = rich.locator('[data-row="tool"]').filter({
		hasText: "go test ./internal/scheduler/...",
	});
	await expect
		.element(commands.first().getByTestId("tool-status"))
		.toHaveText("✕ exit 1");
	await expect
		.element(commands.last().getByTestId("tool-status"))
		.toHaveText("✓ ok");
	await expect
		.element(
			rich
				.locator('[data-row="tool"]')
				.filter({ hasText: "edit internal/scheduler" }),
		)
		.toMatchTextContent("+2 −1");

	// The toggle sits in the action row, rich selected.
	await expect
		.element(card.getByRole("button", { name: "Rich output" }))
		.toHaveAttribute("aria-pressed", "true");
	await expect
		.element(card.getByRole("button", { name: "Raw output" }))
		.toHaveAttribute("title", expect.stringMatching(/raw terminal scrollback/));

	// A job with no events shows its scrollback, with no error.
	const fallback = page.getByTestId("job-card").filter({ hasText: "#221" });
	await expect
		.element(fallback.getByTestId("job-output"))
		.toMatchTextContent("read internal/scheduler/queue.go (#221)");
	await expect
		.poll(() => fallback.getByTestId("job-output-rich").elements().length)
		.toBe(0);
});

test("the rich/raw toggle applies to every card and survives a reload (#769)", async () => {
	setup();
	await renderApp("/agents/coder/projects/lsegal%2Faviary");
	const cards = page.getByTestId("job-card");
	await expect.poll(() => cards.elements().length).toBe(5);
	await expect
		.poll(() => page.getByTestId("job-output-rich").elements().length)
		.toBe(1);

	await cards
		.filter({ hasText: "#209" })
		.getByRole("button", { name: "Raw output" })
		.click();
	await expect
		.poll(() => page.getByTestId("job-output-rich").elements().length)
		.toBe(0);
	await expect
		.poll(() => page.getByTestId("job-output").elements().length)
		.toBe(5);
	const card = cards.filter({ hasText: "#218" });
	await expect
		.element(card.getByTestId("job-output"))
		.toMatchTextContent("read internal/scheduler/queue.go (#218)");
	await expect
		.element(card.getByRole("button", { name: "Raw output" }))
		.toHaveAttribute("aria-pressed", "true");

	await reload();
	await expect.poll(() => cards.elements().length).toBe(5);
	await expect
		.poll(() => page.getByTestId("job-output").elements().length)
		.toBe(5);
	await expect
		.poll(() => page.getByTestId("job-output-rich").elements().length)
		.toBe(0);
	// Raw output keeps streaming.
	const output = card.getByTestId("job-output");
	const first = output.element().textContent;
	await expect.element(output, { timeout: 5_000 }).not.toHaveText(first ?? "");

	// Switching back shows the rich view again.
	await card.getByRole("button", { name: "Rich output" }).click();
	await expect
		.poll(() => page.getByTestId("job-output-rich").elements().length)
		.toBe(1);
});

for (const colorScheme of ["light", "dark"] as const) {
	test(`the rich view uses the raw view's colors and stays readable in the ${colorScheme} theme (#873)`, async () => {
		await commands.emulateMedia({ colorScheme });
		setup();
		await renderApp("/agents/coder/projects/lsegal%2Faviary");
		const card = page.getByTestId("job-card").filter({ hasText: "#218" });
		const rich = card.getByTestId("job-output-rich");
		await expect
			.element(rich.locator('[data-row="tool"]').first())
			.toBeVisible();
		const colors = (el: Element) => {
			const s = getComputedStyle(el);
			return { background: s.backgroundColor, color: s.color };
		};
		const richColors = await colors(rich.element() as HTMLElement);

		// Every text element in the rich view contrasts with the surface.
		const contrasts = await ((root) => {
			// Computed colors may be oklch(); a canvas pixel normalizes them.
			const ctx = document
				.createElement("canvas")
				.getContext("2d", { willReadFrequently: true });
			if (!ctx) throw new Error("no canvas");
			const rgb = (c: string) => {
				ctx.clearRect(0, 0, 1, 1);
				ctx.fillStyle = c;
				ctx.fillRect(0, 0, 1, 1);
				const [r, g, b, a] = ctx.getImageData(0, 0, 1, 1).data;
				return [r, g, b, a / 255];
			};
			// Composite a translucent color over the opaque one under it.
			const over = (top: number[], under: number[]) => {
				const a = top[3] ?? 1;
				return [0, 1, 2].map((i) => top[i] * a + under[i] * (1 - a));
			};
			const lum = (c: number[]) => {
				const [r, g, b] = c.map((v) => {
					const s = v / 255;
					return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4;
				});
				return 0.2126 * r + 0.7152 * g + 0.0722 * b;
			};
			const surface = (el: Element): number[] => {
				const chain: Element[] = [];
				for (let e: Element | null = el; e; e = e.parentElement) chain.push(e);
				let bg = [255, 255, 255];
				for (const e of chain.reverse()) {
					const c = rgb(getComputedStyle(e).backgroundColor);
					if ((c[3] ?? 1) > 0) bg = over(c, bg);
				}
				return bg;
			};
			const out: { text: string; ratio: number }[] = [];
			for (const el of root.querySelectorAll("li, li *")) {
				if (el.closest("pre")) continue;
				const own = [...el.childNodes].some(
					(n) => n.nodeType === Node.TEXT_NODE && n.textContent?.trim(),
				);
				if (!own) continue;
				const bg = surface(el);
				const fg = over(rgb(getComputedStyle(el).color), bg);
				const [hi, lo] = [lum(fg), lum(bg)].sort((a, b) => b - a);
				out.push({
					text: (el.textContent ?? "").trim().slice(0, 40),
					ratio: (hi + 0.05) / (lo + 0.05),
				});
			}
			return out;
		})(rich.element() as HTMLElement);
		expect(contrasts.length).toBeGreaterThan(5);
		expect(contrasts.filter((c) => c.ratio < 4.5)).toEqual([]);

		await card.getByRole("button", { name: "Raw output" }).click();
		const raw = card.getByTestId("job-output");
		await expect.element(raw).toBeVisible();
		expect(await colors(raw.element() as HTMLElement)).toEqual(richColors);
	});
}

test("a tool call's status updates in place without duplicate rows (#769)", async () => {
	let finished = false;
	let failed = false;
	const events = () => [
		{ kind: "message", time: at(2), text: "Running the tests." },
		{
			kind: "tool_call",
			time: at(2),
			call_id: "t1",
			name: "Bash",
			input: { command: "go test ./internal/scheduler/... -run TestBackoff" },
		},
		...(finished
			? [
					{
						kind: "tool_result",
						time: at(1),
						call_id: "t1",
						output: "ok",
					},
				]
			: []),
		{
			kind: "tool_call",
			time: at(1),
			call_id: "t2",
			name: "Read",
			input: { file_path: "missing.go" },
		},
		...(failed
			? [
					{
						kind: "tool_result",
						time: at(1),
						call_id: "t2",
						output: "no such file",
						is_error: true,
					},
				]
			: []),
	];
	setup(() => STATUS, {
		project_job_get: jobGetWithEvents(events),
	});
	await renderApp("/agents/coder/projects/lsegal%2Faviary");
	const rich = page
		.getByTestId("job-card")
		.filter({ hasText: "#218" })
		.getByTestId("job-output-rich");
	const bash = rich.locator('[data-call-id="t1"]');
	const read = rich.locator('[data-call-id="t2"]');
	await expect.element(bash.getByTestId("tool-status")).toHaveText("running");
	await expect.element(read.getByTestId("tool-status")).toHaveText("running");

	finished = true;
	failed = true;
	await expect.element(bash.getByTestId("tool-status")).toHaveText("✓ ok");
	await expect.element(read.getByTestId("tool-status")).toHaveText("✕ error");
	await expect
		.element(read.getByTestId("tool-status"))
		.toHaveAttribute("title", "no such file");
	await expect
		.poll(() => rich.locator('[data-row="tool"]').elements().length)
		.toBe(2);
	await expect.poll(() => bash.elements().length).toBe(1);
	await expect.poll(() => read.elements().length).toBe(1);
});

test("rich card output stays pinned to the bottom unless scrolled up (#769)", async () => {
	let count = 30;
	const events = () =>
		Array.from({ length: count }, (_, i) => ({
			kind: "message",
			time: at(1),
			text: `Step ${i + 1} of the plan is done.`,
		}));
	setup(() => STATUS, {
		project_job_get: jobGetWithEvents(events),
	});
	await renderApp("/agents/coder/projects/lsegal%2Faviary");
	const rich = page
		.getByTestId("job-card")
		.filter({ hasText: "#218" })
		.getByTestId("job-output-rich");
	const gap = () =>
		((el) => el.scrollHeight - el.scrollTop - el.clientHeight)(
			rich.element() as HTMLElement,
		);
	await expect.element(rich).toMatchTextContent("Step 30 of the plan");
	await expect.poll(gap).toBeLessThanOrEqual(1);

	// New output keeps the view at the bottom.
	count = 34;
	await expect.element(rich).toMatchTextContent("Step 34 of the plan");
	await expect.poll(gap).toBeLessThanOrEqual(1);

	// Scrolled up, it stays put as output arrives.
	await ((el) => {
		el.scrollTop = 0;
		el.dispatchEvent(new Event("scroll"));
	})(rich.element() as HTMLElement);
	count = 38;
	await expect.element(rich).toMatchTextContent("Step 38 of the plan");
	expect(await ((el) => el.scrollTop)(rich.element() as HTMLElement)).toBe(0);

	// The shared "More below" pill counts the new rows and follows again
	// (#1072).
	const card = page.getByTestId("job-card").filter({ hasText: "#218" });
	const pill = card.getByTestId("job-output-more-below");
	await expect.element(pill).toHaveText("4 new · More below ↓");
	await pill.click();
	await expect.poll(gap).toBeLessThanOrEqual(1);
	await expectHidden(pill);
	count = 40;
	await expect.element(rich).toMatchTextContent("Step 40 of the plan");
	await expect.poll(gap).toBeLessThanOrEqual(1);
});

test("job detail's transcript opens at the bottom and pauses with the pill (#1072)", async () => {
	await page.viewport(1600, 900);
	let count = 40;
	const events = () =>
		Array.from({ length: count }, (_, i) => ({
			kind: "message",
			time: at(1),
			text: `Step ${i + 1} of the plan is done.`,
		}));
	setup(() => STATUS, {
		project_job_get: jobGetWithEvents(events),
	});
	await renderApp("/agents/coder/projects/lsegal%2Faviary/jobs/218");
	const transcript = page.getByTestId("job-transcript");
	const pill = page.getByTestId("job-transcript-more-below");
	const gap = () =>
		((el) => el.scrollHeight - el.scrollTop - el.clientHeight)(
			transcript.element() as HTMLElement,
		);
	await expect.element(transcript).toMatchTextContent("Step 40 of the plan");
	await expect.poll(gap).toBeLessThanOrEqual(8);

	await ((el) => {
		el.scrollTop = 0;
	})(transcript.element() as HTMLElement);
	await expect.element(pill).toBeVisible();
	count = 42;
	await expect.element(transcript).toMatchTextContent("Step 42 of the plan");
	expect(
		await ((el) => el.scrollTop)(transcript.element() as HTMLElement),
	).toBe(0);
	await expect.element(pill).toHaveText("2 new · More below ↓");

	await pill.click();
	await expect.poll(gap).toBeLessThanOrEqual(8);
	await expectHidden(pill);
	count = 44;
	await expect.element(transcript).toMatchTextContent("Step 44 of the plan");
	await expect.poll(gap).toBeLessThanOrEqual(8);
});
