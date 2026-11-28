// Ported from web/e2e/projects.spec.ts (#1636): job count stat boxes and the job detail header.
import { expect, test } from "vitest";
import { commands, type Locator, page } from "vitest/browser";
import { currentURL, goto, reload, renderApp } from "../component-test/app";
import { boundingBoxOf } from "./helpers/box";
import { CLONES, JOBS, STATUS, setup } from "./projects-fixtures";
// The pages this file renders (see component-test/routes.ts).
import "../components/projects/ProjectJobDetail.vue";
import "../views/ProjectsView.vue";
import "../views/SettingsView.vue";

/** Playwright's toHaveText with a list: one entry per element, in order. */
async function expectTexts(locator: Locator, expected: (string | RegExp)[]) {
	const texts = () =>
		locator
			.elements()
			.map((el) => (el.textContent ?? "").replace(/\s+/g, " ").trim());
	await expect
		.poll(texts)
		.toEqual(
			expected.map((e) =>
				typeof e === "string" ? e : expect.stringMatching(e),
			),
		);
}

test("job count stat boxes reflow without wrapping mid-value", async () => {
	setup(() => ({
		...STATUS,
		jobs: { ...STATUS.jobs, running: 10, merged_24h: 61 },
		concurrency: 20,
	}));
	// 1280 keeps the sidebar beside a narrow board while the viewport is xl.
	for (const width of [375, 768, 1024, 1280, 1440]) {
		await page.viewport(width, 900);
		await renderApp("/agents/coder/projects/lsegal%2Faviary");
		// Phones open on the compact row; expanding it keeps the cards from
		// then on.
		if (width < 768) {
			await expect.element(page.getByTestId("stat-row")).toBeVisible();
			await page.getByTestId("stats-toggle").click();
		}
		const stats = page.getByRole("region", { name: "Job counts" });
		await expect.element(stats).toMatchTextContent("10of 20 busy");
		await expect.element(stats).toMatchTextContent("Merged (24h)61");
		const layout = await ((section) => {
			const lines = (el: Element) => {
				// Text of mixed sizes on one line overlaps vertically; a wrap
				// starts a rect below every earlier one.
				const range = document.createRange();
				range.selectNodeContents(el);
				let count = 0;
				let bottom = Number.NEGATIVE_INFINITY;
				for (const r of [...range.getClientRects()].sort(
					(a, b) => a.top - b.top,
				)) {
					if (r.top >= bottom - 1) count++;
					bottom = Math.max(bottom, r.bottom);
				}
				return count;
			};
			const overflows = (el: Element) => el.scrollWidth > el.clientWidth;
			const cards = [
				...section.querySelectorAll("[data-testid=stat-card]"),
			].map((card) => {
				const label = card.querySelector("[data-testid=stat-label]");
				const value = card.querySelector("[data-testid=stat-value]");
				if (!label || !value) throw new Error("stat card is missing parts");
				const box = card.getBoundingClientRect();
				return {
					text: card.textContent,
					top: Math.round(box.top),
					height: Math.round(box.height),
					labelTop: Math.round(label.getBoundingClientRect().top),
					valueTop: Math.round(value.getBoundingClientRect().top),
					labelLines: lines(label),
					valueLines: lines(value),
					overflow: overflows(card) || overflows(label) || overflows(value),
				};
			});
			const clis = document.querySelector("[aria-label='Models and quota']");
			return {
				cards,
				pageOverflow:
					document.documentElement.scrollWidth > window.innerWidth ||
					overflows(section) ||
					(clis ? overflows(clis) : false),
				chipOverflow: clis
					? [...clis.children].some(
							(chip) =>
								chip.getBoundingClientRect().right >
								clis.getBoundingClientRect().right + 1,
						)
					: false,
			};
		})(stats.element() as HTMLElement);
		expect(layout.cards).toHaveLength(7);
		expect(layout.pageOverflow, `page overflow at ${width}px`).toBe(false);
		expect(layout.chipOverflow, `CLI chip overflow at ${width}px`).toBe(false);
		for (const card of layout.cards) {
			expect(card.labelLines, `${card.text} label at ${width}px`).toBe(1);
			expect(card.valueLines, `${card.text} value at ${width}px`).toBe(1);
			expect(card.overflow, `${card.text} overflow at ${width}px`).toBe(false);
		}
		// Cards in one row share a height and line up labels and numbers.
		const rows = new Map<number, typeof layout.cards>();
		for (const card of layout.cards) {
			rows.set(card.top, [...(rows.get(card.top) ?? []), card]);
		}
		for (const row of rows.values()) {
			for (const key of ["height", "labelTop", "valueTop"] as const) {
				expect(
					new Set(row.map((card) => card[key])).size,
					`${key} in a row at ${width}px`,
				).toBe(1);
			}
		}
	}
});

test("expanded job count cards fit one row at desktop widths (#923)", async () => {
	setup(() => ({
		...STATUS,
		jobs: { ...STATUS.jobs, running: 10, merged_24h: 61 },
		concurrency: 20,
	}));
	for (const width of [1280, 1440]) {
		await page.viewport(width, 900);
		await renderApp("/agents/coder/projects/lsegal%2Faviary");
		const cards = page.getByTestId("stat-card");
		await expect.poll(() => cards.elements().length).toBe(7);
		const boxes = ((els: Element[]) =>
			els.map((el) => {
				const box = el.getBoundingClientRect();
				return { top: Math.round(box.top), height: box.height };
			}))(cards.elements());
		expect(new Set(boxes.map((b) => b.top)).size, `rows at ${width}px`).toBe(1);
		for (const box of boxes) {
			expect(box.height, `card height at ${width}px`).toBeLessThanOrEqual(60);
		}
	}
});

test("job counts fold into one compact row on phones (#910)", async () => {
	setup(() => ({
		...STATUS,
		jobs: { ...STATUS.jobs, merged_24h: 65 },
		concurrency: 20,
	}));
	const colors = (testid: string) =>
		((els: Element[]) =>
			els.map((el) => {
				const css = getComputedStyle(el);
				return [css.color, css.backgroundColor, css.borderColor].join(" ");
			}))(page.getByTestId(testid).elements());
	for (const scheme of ["light", "dark"] as const) {
		await commands.emulateMedia({ colorScheme: scheme });
		await page.viewport(390, 844);
		await renderApp("/agents/coder/projects/lsegal%2Faviary");
		await (() => localStorage.removeItem("aviary.projects.statsCompact"))();
		await reload();
		const row = page.getByTestId("stat-row");
		await expect.element(row).toBeVisible();
		await expect
			.poll(() => page.getByTestId("stat-card").elements().length)
			.toBe(0);
		const chips = page.getByTestId("stat-chip");
		await expectTexts(chips, [
			/^\d+\/20slots$/,
			/^\d+starting$/,
			/^\d+paused$/,
			/^\d+running$/,
			/^\d+done$/,
			/^\d+failed$/,
			"65merged 24h",
		]);
		const layout = await ((section) => {
			const tops = [...section.children].map((chip) =>
				Math.round(chip.getBoundingClientRect().top),
			);
			const chip = section.children[0].getBoundingClientRect();
			return {
				tops: new Set(tops).size,
				height: section.getBoundingClientRect().height,
				chipHeight: chip.height,
				pageOverflow: document.documentElement.scrollWidth > window.innerWidth,
				scrolls: getComputedStyle(section).overflowX,
			};
		})(row.element() as HTMLElement);
		expect(layout.tops, "chips on one line").toBe(1);
		expect(layout.height).toBeLessThanOrEqual(layout.chipHeight + 20);
		expect(layout.pageOverflow, "page overflow").toBe(false);
		expect(layout.scrolls).toBe("auto");
		// Each chip keeps its card's colors.
		const chipColors = await colors("stat-chip");
		const toggle = page.getByTestId("stats-toggle");
		await expect.element(toggle).toHaveAttribute("aria-expanded", "false");
		await toggle.click();
		await expect.poll(() => row.elements().length).toBe(0);
		await expect
			.poll(() => page.getByTestId("stat-card").elements().length)
			.toBe(7);
		await expect.element(toggle).toHaveAttribute("aria-expanded", "true");
		expect(await colors("stat-card"), `${scheme} colors`).toEqual(chipColors);
		// The choice survives a reload, both ways.
		await reload();
		await expect
			.poll(() => page.getByTestId("stat-card").elements().length)
			.toBe(7);
		await toggle.click();
		await expect.element(row).toBeVisible();
		await reload();
		await expect.element(row).toBeVisible();
	}
	// Wide boards keep the cards until the user picks the row.
	await (() => localStorage.removeItem("aviary.projects.statsCompact"))();
	await page.viewport(1280, 900);
	await reload();
	await expect
		.poll(() => page.getByTestId("stat-card").elements().length)
		.toBe(7);
	await expect
		.poll(() => page.getByTestId("stat-row").elements().length)
		.toBe(0);
});

test("an overflowing compact row keeps its scrollbar off the chips and fades its cut edge (#917)", async () => {
	setup(() => ({
		...STATUS,
		jobs: { ...STATUS.jobs, merged_24h: 65 },
		concurrency: 20,
	}));
	const row = page.getByTestId("stat-row");
	const mask = () =>
		((el) => getComputedStyle(el).maskImage)(row.element() as HTMLElement);
	for (const width of [320, 1280]) {
		await page.viewport(width, 844);
		await renderApp("/agents/coder/projects/lsegal%2Faviary");
		await (() => localStorage.setItem("aviary.projects.statsCompact", "1"))();
		await reload();
		await expect.element(row).toBeVisible();
		const layout = await ((section: HTMLElement) => {
			const box = section.getBoundingClientRect();
			const chips = [...section.children].map((chip) =>
				chip.getBoundingClientRect(),
			);
			const scrollbar = section.offsetHeight - section.clientHeight;
			return {
				overflows: section.scrollWidth > section.clientWidth,
				tops: new Set(chips.map((chip) => Math.round(chip.top))).size,
				// Nothing is clipped vertically.
				clipped: chips.some(
					(chip) => chip.top < box.top || chip.bottom > box.bottom,
				),
				// Space between the chips' bottom borders and where the
				// scrollbar starts, overlay or not.
				gap:
					box.bottom -
					scrollbar -
					Math.max(...chips.map((chip) => chip.bottom)),
			};
		})(row.element() as HTMLElement);
		expect(layout.tops, `one line at ${width}px`).toBe(1);
		expect(layout.clipped, `clipped at ${width}px`).toBe(false);
		expect(layout.gap, `scrollbar gap at ${width}px`).toBeGreaterThanOrEqual(4);
		if (!layout.overflows) {
			expect(await mask(), `no fade at ${width}px`).toBe("none");
			continue;
		}
		// Only the right edge fades at the start, both edges mid-way, and only
		// the left once scrolled to the end.
		const fades = async () => {
			const m = await mask();
			return {
				left: /^linear-gradient\(to right, rgba\(0, 0, 0, 0\)/.test(m),
				right: /rgba\(0, 0, 0, 0\)\)$/.test(m),
			};
		};
		expect(await fades()).toEqual({ left: false, right: true });
		await ((el) => {
			el.scrollLeft = 20;
		})(row.element() as HTMLElement);
		await expect.poll(fades).toEqual({ left: true, right: true });
		await ((el) => {
			el.scrollLeft = el.scrollWidth;
		})(row.element() as HTMLElement);
		await expect.poll(fades).toEqual({ left: true, right: false });
	}
	// The narrow phone has to overflow for this test to mean anything.
	await page.viewport(320, 844);
	await expect
		.poll(() =>
			((el) => el.scrollWidth > el.clientWidth)(row.element() as HTMLElement),
		)
		.toBe(true);
});

for (const { name, width } of [
	{ name: "desktop", width: 1440 },
	{ name: "narrow desktop", width: 1024 },
	{ name: "mobile", width: 390 },
]) {
	test(`job detail header keeps its actions on one row at ${name} width (#797)`, async () => {
		await page.viewport(width, 900);
		const title =
			"Job detail view: rich single-row transcript, collapsible raw Output, prominent Clone, and a stepper that never scrolls sideways";
		setup(() => STATUS, {
			project_job_get: (args?: Record<string, unknown>) => {
				const found = JOBS.find(
					(j) => j.target === args?.target && j.number === args?.number,
				);
				if (!found) throw new Error("no such job");
				return {
					...found,
					title,
					...CLONES[found.number],
					scrollback: "",
					events: [],
					raw_output: "",
				};
			},
		});
		await renderApp("/agents/coder/projects/lsegal%2Faviary/jobs/218");
		// The job's actions sit in the shared header's title row (#1147).
		const header = page.getByTestId("settings-title-row");
		const actions = page.getByTestId("job-detail-actions");
		await expect
			.element(page.getByTestId("job-detail-title"))
			.toMatchTextContent(title);
		await expect
			.element(actions.getByRole("button", { name: "Stop" }))
			.toBeVisible();

		const buttons = actions.locator(":scope > a, :scope > button");
		// Raw output, Retry, Stop and close; the Issue/PR links live in the title (#835).
		await expect.poll(() => buttons.elements().length).toBe(4);
		const boxes = ((els: Element[]) =>
			els.map((el) => {
				const r = el.getBoundingClientRect();
				return { top: r.top, height: r.height };
			}))(buttons.elements());
		// Every action, the close button included, sits on one row at one height.
		expect(new Set(boxes.map((b) => b.top)).size).toBe(1);
		expect(new Set(boxes.map((b) => b.height)).size).toBe(1);
		await expect
			.element(page.getByTestId("job-detail-close"))
			.toHaveAttribute("aria-label", "Close details");

		const layout = await ((el) => {
			const t = el
				.querySelector("[data-testid=job-detail-title]")
				?.getBoundingClientRect();
			const a = el
				.querySelector("[data-testid=settings-actions]")
				?.getBoundingClientRect();
			return {
				overflow: el.scrollWidth - el.clientWidth,
				titleHeight: t?.height ?? 0,
				titleRight: t?.right ?? 0,
				titleBottom: t?.bottom ?? 0,
				actionsLeft: a?.left ?? 0,
				actionsTop: a?.top ?? 0,
			};
		})(header.element() as HTMLElement);
		expect(layout.overflow).toBeLessThanOrEqual(0);
		// The long title wraps instead of squeezing or overlapping the actions.
		expect(layout.titleHeight).toBeGreaterThan(15);
		// The actions sit beside the title, or wrap below it when they don't fit.
		expect(
			layout.titleRight <= layout.actionsLeft ||
				layout.titleBottom <= layout.actionsTop,
		).toBe(true);
		if (width < 1024)
			expect(layout.titleBottom).toBeLessThanOrEqual(layout.actionsTop);
	});
}

test("job detail's title PR link shows the PR state (#834)", async () => {
	setup();
	await renderApp("/agents/coder/projects/lsegal%2Faviary/jobs/218");
	const pr = page.getByTestId("job-detail-pr-link");
	await expect.element(pr).toHaveAttribute("target", "_blank");
	await expect.element(pr).toHaveAttribute("data-pr-state", "draft");
	await expect
		.element(pr.getByTestId("pr-state-icon"))
		.toHaveAttribute("aria-label", "Draft pull request");
	await goto("/agents/coder/projects/lsegal%2Faviary/jobs/200");
	await expect.element(pr).toHaveAttribute("data-pr-state", "merged");
	await expect
		.element(page.getByTestId("job-detail-issue-link"))
		.toHaveAttribute("target", "_blank");
});

test("job detail's Raw output is an icon button that toggles its panel (#834)", async () => {
	setup();
	await renderApp("/agents/coder/projects/lsegal%2Faviary/jobs/218");
	const actions = page.getByTestId("job-detail-actions");
	const toggle = actions.getByRole("button", {
		name: "Raw output",
		exact: true,
	});
	await expect.element(toggle).toHaveAttribute("aria-pressed", "false");
	await expect.element(toggle).toHaveText("");
	await expect
		.element(toggle)
		.toHaveAttribute("title", expect.stringMatching(/raw CLI output/));
	const size = async (name: string) => {
		const box = await boundingBoxOf(
			actions.getByRole("button", { name, exact: true }),
		);
		return box && { width: box.width, height: box.height };
	};
	expect(await size("Raw output")).toEqual(await size("Retry"));

	await toggle.click();
	await expect.element(page.getByTestId("raw-output")).toBeVisible();
	await expect.element(toggle).toHaveAttribute("aria-pressed", "true");
	await toggle.click();
	await expect
		.poll(() => page.getByTestId("raw-output").elements().length)
		.toBe(0);
	await expect.element(toggle).toHaveAttribute("aria-pressed", "false");
});

test("job detail's close button is a borderless ghost, and it and Escape close the pane (#834)", async () => {
	setup();
	await renderApp("/agents/coder/projects/lsegal%2Faviary/jobs/218");
	const close = page.getByRole("link", { name: "Close details" });
	await expect.element(close).toHaveAttribute("title", "Close details");
	const border = (el: Element) =>
		Number.parseFloat(getComputedStyle(el).borderLeftWidth);
	expect(border(close.element())).toBe(0);
	const retry = page.getByRole("button", { name: "Retry" });
	await expect.element(retry).toBeInTheDocument();
	expect(border(retry.element())).toBeGreaterThan(0);
	await close.click();
	await expect.poll(currentURL).toBe("/agents/coder/projects/lsegal%2Faviary");
	await expect
		.poll(() => page.getByTestId("job-detail").elements().length)
		.toBe(0);

	await goto("/agents/coder/projects/lsegal%2Faviary/jobs/218");
	await expect.element(page.getByTestId("job-detail")).toBeVisible();
	await commands.keyPress("Escape");
	await expect.poll(currentURL).toBe("/agents/coder/projects/lsegal%2Faviary");
	await expect
		.poll(() => page.getByTestId("job-detail").elements().length)
		.toBe(0);
	await expect.element(page.getByTestId("job-card").first()).toBeVisible();
});
