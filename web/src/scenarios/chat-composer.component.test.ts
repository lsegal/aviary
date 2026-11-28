import { beforeEach, describe, expect, test, vi } from "vitest";
import { commands, type Locator, page } from "vitest/browser";
import { renderApp } from "../component-test/app";
import { declarePages } from "../component-test/routes";
import { mockMCP } from "../component-test/server";
import { chatDraftKey, setChatDraft } from "../components/chat/drafts";

// The pages this file renders (see component-test/routes.ts).
declarePages(
	() => import("../views/ChatView.vue"),
	() => import("../views/SettingsView.vue"),
);

// Drafts live in module state, which outlasts each test in this file; each
// spec test started from a fresh page with no draft.
beforeEach(() => {
	setChatDraft(chatDraftKey("assistant", "s1"), { text: "", attachments: [] });
});

const count = (loc: Locator) => loc.elements().length;

const PIXEL_B64 =
	"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==";
const RED_B64 =
	"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAADElEQVR4nGP4z8AAAAMBAQDJ/pLvAAAAAElFTkSuQmCC";

const bytes = (b64: string) =>
	Uint8Array.from(atob(b64), (c) => c.charCodeAt(0));
const PIXEL_PNG = bytes(PIXEL_B64);
const RED_PNG = bytes(RED_B64);
const dataURL = (b64: string) => `data:image/png;base64,${b64}`;
const png = (name: string, data: Uint8Array<ArrayBuffer>) =>
	new File([data], name, { type: "image/png" });

interface RunArgs {
	message?: string;
	media_urls?: string[];
}

/** Mocks one agent with one chat; `run` answers agent_run. */
async function mockChat(
	run: (args: RunArgs) => unknown = () => ({ stream: ["ok"], result: "" }),
) {
	const runs: RunArgs[] = [];
	const stops: unknown[] = [];
	mockMCP({
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
	await renderApp("/agents/assistant/chat");
	const composer = page.getByTestId("chat-composer");
	await expect.element(composer).toBeVisible();
	// The chat's (empty) draft is restored once its session loads; anything
	// attached before then would be replaced. A spec's round trips left
	// the session loaded by the first action.
	await expect.element(page.getByText("No messages yet")).toBeVisible();
	return {
		runs,
		stops,
		composer,
		input: composer.getByPlaceholder("Describe a task or ask a question"),
	};
}

/** Picks files through +: the button opens the file input's chooser, and
 * the files land on that input, as a user picking them would. */
async function pickFiles(files: File[]) {
	const opened = vi
		.spyOn(HTMLInputElement.prototype, "click")
		.mockImplementation(() => {});
	await page
		.getByTestId("chat-composer")
		.getByRole("button", { name: "Attach files" })
		.click();
	expect(opened).toHaveBeenCalled();
	opened.mockRestore();
	await page.getByTestId("chat-file-input").upload(files);
}

/** Focuses `loc` and presses `key`, as Playwright's locator.press. */
async function press(loc: Locator, key: string) {
	(loc.element() as HTMLElement).focus();
	await commands.keyPress(key);
}

const rect = (loc: Locator) => loc.element().getBoundingClientRect();

test("the composer holds +, the text field and Send in one box", async () => {
	const { composer, input } = await mockChat();
	const plus = composer.getByRole("button", { name: "Attach files" });
	const send = composer.getByRole("button", { name: "Send message" });
	await expect.element(plus).toBeVisible();
	await expect.element(send).toBeDisabled();

	// The focus ring belongs to the container, not the text field.
	await input.click();
	await expect
		.poll(() => getComputedStyle(composer.element()).boxShadow)
		.not.toBe("none");
	expect(getComputedStyle(input.element()).outlineStyle).toBe("none");

	const box = rect(composer);
	const startHeight = rect(input).height;
	await input.fill("one");
	for (const line of ["two", "three", "four"]) {
		await press(input, "Shift+Enter");
		await commands.keyType(line);
	}
	await expect.element(input).toHaveValue("one\ntwo\nthree\nfour");
	await expect.poll(() => rect(input).height).toBeGreaterThan(startHeight * 2);
	const grown = rect(input);

	// Both buttons stay inside the box, lined up along its bottom edge.
	const after = rect(composer);
	const plusBox = rect(plus);
	const sendBox = rect(send);
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
	expect(plusBox.x).toBeLessThan(grown.x);
	expect(sendBox.x).toBeGreaterThan(grown.x + grown.width - 1);
});

test("files attached through + are appended, removable and all sent", async () => {
	// The reply never finishes, so no reload replaces the sent bubble.
	const { runs, composer, input } = await mockChat(() => new Promise(() => {}));
	await pickFiles([png("first.png", PIXEL_PNG), png("second.png", PIXEL_PNG)]);
	const chips = composer.getByRole("listitem");
	await expect.poll(() => count(chips)).toBe(2);

	// Picking again appends instead of replacing.
	const notes = "# Notes\nremember this";
	await pickFiles([new File([notes], "notes.md", { type: "text/markdown" })]);
	await expect.poll(() => count(chips)).toBe(3);
	await expect.element(composer.getByText("notes.md")).toBeVisible();

	await composer.getByRole("button", { name: "Remove first.png" }).click();
	await expect.poll(() => count(chips)).toBe(2);
	await expect
		.element(composer.getByRole("img", { name: "second.png" }))
		.toBeVisible();
	expect(count(composer.getByRole("img", { name: "first.png" }))).toBe(0);

	await input.fill("look at these");
	await press(input, "Enter");

	await expect.poll(() => runs.length).toBe(1);
	const media = runs[0].media_urls ?? [];
	expect(media).toHaveLength(2);
	expect(media[0]).toBe(`data:image/png;base64,${PIXEL_B64}`);
	expect(media[1]).toBe(`data:text/plain;name=notes.md;base64,${btoa(notes)}`);
	await expect.poll(() => count(chips)).toBe(0);

	const sent = page
		.getByTestId("chat-user-message")
		.filter({ hasText: "look at these" });
	await expect.poll(() => count(sent.locator("img"))).toBe(1);
	await expect
		.element(sent.getByRole("link", { name: "notes.md" }))
		.toBeVisible();
});

test("clicking a pending image opens the preview; × only removes it", async () => {
	const { composer } = await mockChat();
	await pickFiles([
		png("white.png", PIXEL_PNG),
		new File(["# Notes"], "notes.md", { type: "text/markdown" }),
		png("red.png", RED_PNG),
	]);
	const chips = composer.getByRole("listitem");
	await expect.poll(() => count(chips)).toBe(3);
	const preview = page.getByTestId("image-preview");
	const shown = preview.getByTestId("image-preview-img");

	// Thumbnails read as clickable; text attachments aren't previewable.
	const thumb = composer.getByRole("button", { name: "Preview red.png" });
	await expect.element(thumb).toHaveCSS("cursor", "pointer");
	expect(count(composer.getByTestId("attachment-preview"))).toBe(2);
	expect(
		count(composer.getByRole("button", { name: "Preview notes.md" })),
	).toBe(0);

	// The clicked image opens at full size, and the preview steps through
	// just the images.
	await thumb.click();
	await expect.element(preview).toBeVisible();
	await expect.element(preview).toHaveAttribute("role", "dialog");
	await expect.element(shown).toHaveAttribute("src", dataURL(RED_B64));
	await expect.element(shown).toHaveAttribute("alt", "red.png");
	await expect
		.element(preview.getByTestId("image-preview-position"))
		.toHaveText("2 / 2");
	await expect
		.element(preview.getByRole("button", { name: "Close image" }))
		.toHaveFocus();
	await commands.keyPress("ArrowRight");
	await expect.element(shown).toHaveAttribute("src", dataURL(PIXEL_B64));
	await preview.getByRole("button", { name: "Next image" }).click();
	await expect.element(shown).toHaveAttribute("src", dataURL(RED_B64));
	await commands.keyPress("ArrowLeft");
	await expect.element(shown).toHaveAttribute("alt", "white.png");

	// Esc closes it and returns focus to the thumbnail; nothing is removed.
	await commands.keyPress("Escape");
	await expect.element(preview).not.toBeInTheDocument();
	await expect.element(thumb).toHaveFocus();
	expect(count(chips)).toBe(3);

	// The keyboard opens it too, and a click outside the image closes it.
	await commands.keyPress("Enter");
	await expect.element(shown).toHaveAttribute("src", dataURL(RED_B64));
	await preview.click({ position: { x: 5, y: 5 } });
	await expect.element(preview).not.toBeInTheDocument();
	expect(count(chips)).toBe(3);

	// × removes without opening the preview.
	await composer.getByRole("button", { name: "Remove red.png" }).click();
	await expect.poll(() => count(chips)).toBe(2);
	expect(count(preview)).toBe(0);
	await composer.getByRole("button", { name: "Preview white.png" }).click();
	await expect.element(shown).toHaveAttribute("src", dataURL(PIXEL_B64));
	expect(count(preview.getByTestId("image-preview-position"))).toBe(0);
	expect(count(preview.getByRole("button", { name: "Next image" }))).toBe(0);
});

/** Dispatches a paste of `files` on `el`. */
function paste(el: Element, files: File[]) {
	const data = new DataTransfer();
	for (const f of files) data.items.add(f);
	el.dispatchEvent(
		new ClipboardEvent("paste", {
			clipboardData: data,
			bubbles: true,
			cancelable: true,
		}),
	);
}

test("pasted and dropped images open the preview too", async () => {
	const { composer, input } = await mockChat();
	(input.element() as HTMLElement).focus();
	paste(input.element(), [png("pasted.png", RED_PNG)]);
	const data = new DataTransfer();
	data.items.add(png("dropped.png", PIXEL_PNG));
	composer.element().dispatchEvent(
		new DragEvent("drop", {
			dataTransfer: data,
			bubbles: true,
			cancelable: true,
		}),
	);
	await expect.poll(() => count(composer.getByRole("listitem"))).toBe(2);
	const shown = page.getByTestId("image-preview-img");
	await composer.getByRole("button", { name: "Preview pasted.png" }).click();
	await expect.element(shown).toHaveAttribute("src", dataURL(RED_B64));
	await commands.keyPress("Escape");
	await composer.getByRole("button", { name: "Preview dropped.png" }).click();
	await expect.element(shown).toHaveAttribute("src", dataURL(PIXEL_B64));
});

test("sent images open in the same preview", async () => {
	// The reply never finishes, so no reload replaces the sent bubble.
	const { input } = await mockChat(() => new Promise(() => {}));
	await pickFiles([png("white.png", PIXEL_PNG), png("red.png", RED_PNG)]);
	await input.fill("two images");
	await press(input, "Enter");
	const sent = page
		.getByTestId("chat-user-message")
		.filter({ hasText: "two images" });
	await expect.poll(() => count(sent.locator("img"))).toBe(2);

	await sent.locator("img").nth(1).click();
	const preview = page.getByTestId("image-preview");
	const shown = preview.getByTestId("image-preview-img");
	await expect.element(shown).toHaveAttribute("src", dataURL(RED_B64));
	await expect
		.element(preview.getByTestId("image-preview-position"))
		.toHaveText("2 / 2");
	await commands.keyPress("ArrowRight");
	await expect.element(shown).toHaveAttribute("src", dataURL(PIXEL_B64));
	await preview.getByRole("button", { name: "Close image" }).click();
	await expect.element(preview).not.toBeInTheDocument();
});

test("pasting several images adds every one", async () => {
	const { composer, input } = await mockChat();
	(input.element() as HTMLElement).focus();
	paste(input.element(), [png("a.png", PIXEL_PNG), png("b.png", PIXEL_PNG)]);
	await expect.poll(() => count(composer.getByRole("listitem"))).toBe(2);
});

test("an unsupported file is refused with a notice", async () => {
	const { composer } = await mockChat();
	await pickFiles([
		new File(
			[Uint8Array.of(0x50, 0x4b, 0x03, 0x04, 0x00, 0x00)],
			"archive.zip",
			{
				type: "application/zip",
			},
		),
	]);
	await expect
		.element(composer.getByRole("alert"))
		.toMatchTextContent("archive.zip can't be attached");
	expect(count(composer.getByRole("listitem"))).toBe(0);
});

test("Send turns into Stop inside the box while the agent runs", async () => {
	let release = () => {};
	const held = new Promise<void>((resolve) => {
		release = resolve;
	});
	const { stops, composer, input } = await mockChat(async () => {
		await held;
		return { stream: ["done"], result: "" };
	});
	const send = composer.getByRole("button", { name: "Send message" });
	const stop = composer.getByRole("button", { name: "Stop response" });

	await input.fill("long task");
	const sendBox = rect(send);
	await send.click();

	await expect.element(stop).toBeVisible();
	expect(count(send)).toBe(0);
	const stopBox = rect(stop);
	expect(Math.abs(stopBox.x - sendBox.x)).toBeLessThan(1);
	expect(Math.abs(stopBox.y - sendBox.y)).toBeLessThan(1);

	// Typing a follow-up during the run offers Send again.
	await input.fill("follow-up");
	await expect.element(send).toBeEnabled();
	expect(count(stop)).toBe(0);
	await input.fill("");
	await expect.element(stop).toBeVisible();

	await stop.click();
	await expect.poll(() => stops.length).toBe(1);
	await expect.element(send).toBeVisible();
	expect(count(stop)).toBe(0);
	release();
});

test("Enter sends and Shift+Enter inserts a newline", async () => {
	const { runs, input } = await mockChat();
	await input.fill("first line");
	await press(input, "Shift+Enter");
	await commands.keyType("second line");
	await expect.element(input).toHaveValue("first line\nsecond line");
	expect(runs).toHaveLength(0);

	await press(input, "Enter");
	await expect.poll(() => runs.length).toBe(1);
	expect(runs[0].message).toBe("first line\nsecond line");
	await expect.element(input).toHaveValue("");
});

describe("phone width", () => {
	// The spec emulated a touch screen and tapped; a component test can't turn
	// on touch, so this clicks at phone width.
	test("tapping a pending image opens the preview", async () => {
		await page.viewport(390, 844);
		const { composer } = await mockChat();
		await pickFiles([png("red.png", RED_PNG)]);
		await composer.getByRole("button", { name: "Preview red.png" }).click();
		const preview = page.getByTestId("image-preview");
		await expect
			.element(preview.getByTestId("image-preview-img"))
			.toHaveAttribute("src", dataURL(RED_B64));
		await preview.getByRole("button", { name: "Close image" }).click();
		await expect.element(preview).not.toBeInTheDocument();
		expect(count(composer.getByRole("listitem"))).toBe(1);
	});
});
