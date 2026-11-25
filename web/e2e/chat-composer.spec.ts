/// <reference types="node" />

import { mockMCP, setAuthToken } from "./helpers/mockMCP";
import { expect, type Page, test } from "./helpers/test";

const PIXEL_PNG = Buffer.from(
	"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==",
	"base64",
);

const RED_PNG = Buffer.from(
	"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAADElEQVR4nGP4z8AAAAMBAQDJ/pLvAAAAAElFTkSuQmCC",
	"base64",
);

const dataURL = (png: Buffer) =>
	`data:image/png;base64,${png.toString("base64")}`;

interface RunArgs {
	message?: string;
	media_urls?: string[];
}

/** Mocks one agent with one chat; `run` answers agent_run. */
async function mockChat(
	page: Page,
	run: (args: RunArgs) => unknown = () => ({ stream: ["ok"], result: "" }),
) {
	const runs: RunArgs[] = [];
	const stops: unknown[] = [];
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
			runs.push(args as RunArgs);
			return run(args as RunArgs);
		},
		session_stop: (args?: Record<string, unknown>) => {
			stops.push(args);
			return "stopped";
		},
	});
	await page.goto("/agents/assistant/chat");
	const composer = page.getByTestId("chat-composer");
	await expect(composer).toBeVisible();
	return {
		runs,
		stops,
		composer,
		input: composer.getByPlaceholder("Describe a task or ask a question"),
	};
}

async function pickFiles(
	page: Page,
	files: Array<{ name: string; mimeType: string; buffer: Buffer }>,
) {
	const chooser = page.waitForEvent("filechooser");
	await page
		.getByTestId("chat-composer")
		.getByRole("button", { name: "Attach files" })
		.click();
	await (await chooser).setFiles(files);
}

test("the composer holds +, the text field and Send in one box", async ({
	page,
}) => {
	const { composer, input } = await mockChat(page);
	const plus = composer.getByRole("button", { name: "Attach files" });
	const send = composer.getByRole("button", { name: "Send message" });
	await expect(plus).toBeVisible();
	await expect(send).toBeDisabled();

	// The focus ring belongs to the container, not the text field.
	await input.focus();
	const ring = await composer.evaluate((el) => getComputedStyle(el).boxShadow);
	expect(ring).not.toBe("none");
	expect(await input.evaluate((el) => getComputedStyle(el).outlineStyle)).toBe(
		"none",
	);

	const box = await composer.boundingBox();
	const startHeight = (await input.boundingBox())?.height ?? 0;
	await input.fill("one");
	for (const line of ["two", "three", "four"]) {
		await input.press("Shift+Enter");
		await input.pressSequentially(line);
	}
	await expect(input).toHaveValue("one\ntwo\nthree\nfour");
	const grown = await input.boundingBox();
	expect(grown?.height ?? 0).toBeGreaterThan(startHeight * 2);

	// Both buttons stay inside the box, lined up along its bottom edge.
	const after = await composer.boundingBox();
	const plusBox = await plus.boundingBox();
	const sendBox = await send.boundingBox();
	if (!box || !after || !plusBox || !sendBox) throw new Error("no layout");
	expect(after.height).toBeGreaterThan(box.height);
	expect(
		Math.abs(plusBox.y + plusBox.height - (sendBox.y + sendBox.height)),
	).toBeLessThan(1);
	for (const b of [plusBox, sendBox]) {
		expect(b.x).toBeGreaterThanOrEqual(after.x);
		expect(b.x + b.width).toBeLessThanOrEqual(after.x + after.width);
		expect(b.y).toBeGreaterThanOrEqual(after.y);
		expect(b.y + b.height).toBeLessThanOrEqual(after.y + after.height);
	}
	expect(plusBox.x).toBeLessThan(grown?.x ?? 0);
	expect(sendBox.x).toBeGreaterThan((grown?.x ?? 0) + (grown?.width ?? 0) - 1);
});

test("files attached through + are appended, removable and all sent", async ({
	page,
}) => {
	// The reply never finishes, so no reload replaces the sent bubble.
	const { runs, composer, input } = await mockChat(
		page,
		() => new Promise(() => {}),
	);
	await pickFiles(page, [
		{ name: "first.png", mimeType: "image/png", buffer: PIXEL_PNG },
		{ name: "second.png", mimeType: "image/png", buffer: PIXEL_PNG },
	]);
	const chips = composer.getByRole("listitem");
	await expect(chips).toHaveCount(2);

	// Picking again appends instead of replacing.
	await pickFiles(page, [
		{
			name: "notes.md",
			mimeType: "text/markdown",
			buffer: Buffer.from("# Notes\nremember this"),
		},
	]);
	await expect(chips).toHaveCount(3);
	await expect(composer.getByText("notes.md")).toBeVisible();

	await composer.getByRole("button", { name: "Remove first.png" }).click();
	await expect(chips).toHaveCount(2);
	await expect(composer.getByRole("img", { name: "second.png" })).toBeVisible();
	await expect(composer.getByRole("img", { name: "first.png" })).toHaveCount(0);

	await input.fill("look at these");
	await input.press("Enter");

	await expect.poll(() => runs.length).toBe(1);
	const media = runs[0].media_urls ?? [];
	expect(media).toHaveLength(2);
	expect(media[0]).toBe(
		`data:image/png;base64,${PIXEL_PNG.toString("base64")}`,
	);
	expect(media[1]).toBe(
		`data:text/plain;name=notes.md;base64,${Buffer.from("# Notes\nremember this").toString("base64")}`,
	);
	await expect(chips).toHaveCount(0);

	const sent = page
		.getByTestId("chat-user-message")
		.filter({ hasText: "look at these" });
	await expect(sent.locator("img")).toHaveCount(1);
	await expect(sent.getByRole("link", { name: "notes.md" })).toBeVisible();
});

test("clicking a pending image opens the preview; × only removes it", async ({
	page,
}) => {
	const { composer } = await mockChat(page);
	await pickFiles(page, [
		{ name: "white.png", mimeType: "image/png", buffer: PIXEL_PNG },
		{
			name: "notes.md",
			mimeType: "text/markdown",
			buffer: Buffer.from("# Notes"),
		},
		{ name: "red.png", mimeType: "image/png", buffer: RED_PNG },
	]);
	await expect(composer.getByRole("listitem")).toHaveCount(3);
	const preview = page.getByTestId("image-preview");
	const shown = preview.getByTestId("image-preview-img");

	// Thumbnails read as clickable; text attachments aren't previewable.
	const thumb = composer.getByRole("button", { name: "Preview red.png" });
	await expect(thumb).toHaveCSS("cursor", "pointer");
	await expect(composer.getByTestId("attachment-preview")).toHaveCount(2);
	await expect(
		composer.getByRole("button", { name: "Preview notes.md" }),
	).toHaveCount(0);

	// The clicked image opens at full size, and the preview steps through
	// just the images.
	await thumb.click();
	await expect(preview).toBeVisible();
	await expect(preview).toHaveAttribute("role", "dialog");
	await expect(shown).toHaveAttribute("src", dataURL(RED_PNG));
	await expect(shown).toHaveAttribute("alt", "red.png");
	await expect(preview.getByTestId("image-preview-position")).toHaveText(
		"2 / 2",
	);
	await expect(
		preview.getByRole("button", { name: "Close image" }),
	).toBeFocused();
	await page.keyboard.press("ArrowRight");
	await expect(shown).toHaveAttribute("src", dataURL(PIXEL_PNG));
	await preview.getByRole("button", { name: "Next image" }).click();
	await expect(shown).toHaveAttribute("src", dataURL(RED_PNG));
	await page.keyboard.press("ArrowLeft");
	await expect(shown).toHaveAttribute("alt", "white.png");

	// Esc closes it and returns focus to the thumbnail; nothing is removed.
	await page.keyboard.press("Escape");
	await expect(preview).toHaveCount(0);
	await expect(thumb).toBeFocused();
	await expect(composer.getByRole("listitem")).toHaveCount(3);

	// The keyboard opens it too, and a click outside the image closes it.
	await page.keyboard.press("Enter");
	await expect(shown).toHaveAttribute("src", dataURL(RED_PNG));
	await preview.click({ position: { x: 5, y: 5 } });
	await expect(preview).toHaveCount(0);
	await expect(composer.getByRole("listitem")).toHaveCount(3);

	// × removes without opening the preview.
	await composer.getByRole("button", { name: "Remove red.png" }).click();
	await expect(composer.getByRole("listitem")).toHaveCount(2);
	await expect(preview).toHaveCount(0);
	await composer.getByRole("button", { name: "Preview white.png" }).click();
	await expect(shown).toHaveAttribute("src", dataURL(PIXEL_PNG));
	await expect(preview.getByTestId("image-preview-position")).toHaveCount(0);
	await expect(preview.getByRole("button", { name: "Next image" })).toHaveCount(
		0,
	);
});

test("pasted and dropped images open the preview too", async ({ page }) => {
	const { composer, input } = await mockChat(page);
	await input.focus();
	await input.evaluate((el, b64) => {
		const bytes = Uint8Array.from(atob(b64), (c) => c.charCodeAt(0));
		const data = new DataTransfer();
		data.items.add(new File([bytes], "pasted.png", { type: "image/png" }));
		el.dispatchEvent(
			new ClipboardEvent("paste", {
				clipboardData: data,
				bubbles: true,
				cancelable: true,
			}),
		);
	}, RED_PNG.toString("base64"));
	await composer.evaluate((el, b64) => {
		const bytes = Uint8Array.from(atob(b64), (c) => c.charCodeAt(0));
		const data = new DataTransfer();
		data.items.add(new File([bytes], "dropped.png", { type: "image/png" }));
		el.dispatchEvent(
			new DragEvent("drop", {
				dataTransfer: data,
				bubbles: true,
				cancelable: true,
			}),
		);
	}, PIXEL_PNG.toString("base64"));
	await expect(composer.getByRole("listitem")).toHaveCount(2);
	const shown = page.getByTestId("image-preview-img");
	await composer.getByRole("button", { name: "Preview pasted.png" }).click();
	await expect(shown).toHaveAttribute("src", dataURL(RED_PNG));
	await page.keyboard.press("Escape");
	await composer.getByRole("button", { name: "Preview dropped.png" }).click();
	await expect(shown).toHaveAttribute("src", dataURL(PIXEL_PNG));
});

test("sent images open in the same preview", async ({ page }) => {
	// The reply never finishes, so no reload replaces the sent bubble.
	const { input } = await mockChat(page, () => new Promise(() => {}));
	await pickFiles(page, [
		{ name: "white.png", mimeType: "image/png", buffer: PIXEL_PNG },
		{ name: "red.png", mimeType: "image/png", buffer: RED_PNG },
	]);
	await input.fill("two images");
	await input.press("Enter");
	const sent = page
		.getByTestId("chat-user-message")
		.filter({ hasText: "two images" });
	await expect(sent.locator("img")).toHaveCount(2);

	await sent.locator("img").nth(1).click();
	const preview = page.getByTestId("image-preview");
	const shown = preview.getByTestId("image-preview-img");
	await expect(shown).toHaveAttribute("src", dataURL(RED_PNG));
	await expect(preview.getByTestId("image-preview-position")).toHaveText(
		"2 / 2",
	);
	await page.keyboard.press("ArrowRight");
	await expect(shown).toHaveAttribute("src", dataURL(PIXEL_PNG));
	await preview.getByRole("button", { name: "Close image" }).click();
	await expect(preview).toHaveCount(0);
});

test("pasting several images adds every one", async ({ page }) => {
	const { composer, input } = await mockChat(page);
	await input.focus();
	await input.evaluate((el, b64) => {
		const bytes = Uint8Array.from(atob(b64), (c) => c.charCodeAt(0));
		const data = new DataTransfer();
		data.items.add(new File([bytes], "a.png", { type: "image/png" }));
		data.items.add(new File([bytes], "b.png", { type: "image/png" }));
		el.dispatchEvent(
			new ClipboardEvent("paste", {
				clipboardData: data,
				bubbles: true,
				cancelable: true,
			}),
		);
	}, PIXEL_PNG.toString("base64"));
	await expect(composer.getByRole("listitem")).toHaveCount(2);
});

test("an unsupported file is refused with a notice", async ({ page }) => {
	const { composer } = await mockChat(page);
	await pickFiles(page, [
		{
			name: "archive.zip",
			mimeType: "application/zip",
			buffer: Buffer.from([0x50, 0x4b, 0x03, 0x04, 0x00, 0x00]),
		},
	]);
	await expect(composer.getByRole("alert")).toContainText(
		"archive.zip can't be attached",
	);
	await expect(composer.getByRole("listitem")).toHaveCount(0);
});

test("Send turns into Stop inside the box while the agent runs", async ({
	page,
}) => {
	let release = () => {};
	const held = new Promise<void>((resolve) => {
		release = resolve;
	});
	const { stops, composer, input } = await mockChat(page, async () => {
		await held;
		return { stream: ["done"], result: "" };
	});
	const send = composer.getByRole("button", { name: "Send message" });
	const stop = composer.getByRole("button", { name: "Stop response" });

	await input.fill("long task");
	const sendBox = await send.boundingBox();
	await send.click();

	await expect(stop).toBeVisible();
	await expect(send).toHaveCount(0);
	const stopBox = await stop.boundingBox();
	expect(Math.abs((stopBox?.x ?? 0) - (sendBox?.x ?? -100))).toBeLessThan(1);
	expect(Math.abs((stopBox?.y ?? 0) - (sendBox?.y ?? -100))).toBeLessThan(1);

	// Typing a follow-up during the run offers Send again.
	await input.fill("follow-up");
	await expect(send).toBeEnabled();
	await expect(stop).toHaveCount(0);
	await input.fill("");
	await expect(stop).toBeVisible();

	await stop.click();
	await expect.poll(() => stops.length).toBe(1);
	await expect(send).toBeVisible();
	await expect(stop).toHaveCount(0);
	release();
});

test("Enter sends and Shift+Enter inserts a newline", async ({ page }) => {
	const { runs, input } = await mockChat(page);
	await input.fill("first line");
	await input.press("Shift+Enter");
	await input.pressSequentially("second line");
	await expect(input).toHaveValue("first line\nsecond line");
	expect(runs).toHaveLength(0);

	await input.press("Enter");
	await expect.poll(() => runs.length).toBe(1);
	expect(runs[0].message).toBe("first line\nsecond line");
	await expect(input).toHaveValue("");
});

test.describe("phone width", () => {
	test.use({ viewport: { width: 390, height: 844 }, hasTouch: true });

	test("tapping a pending image opens the preview", async ({ page }) => {
		const { composer } = await mockChat(page);
		await pickFiles(page, [
			{ name: "red.png", mimeType: "image/png", buffer: RED_PNG },
		]);
		await composer.getByRole("button", { name: "Preview red.png" }).tap();
		const preview = page.getByTestId("image-preview");
		await expect(preview.getByTestId("image-preview-img")).toHaveAttribute(
			"src",
			dataURL(RED_PNG),
		);
		await preview.getByRole("button", { name: "Close image" }).tap();
		await expect(preview).toHaveCount(0);
		await expect(composer.getByRole("listitem")).toHaveCount(1);
	});
});
