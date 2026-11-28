import type {} from "@vitest/browser-playwright";
import type { BrowserCommand } from "vitest/node";

// Playwright's page input and emulation, for component tests (#1636) that
// need real keyboard and pointer events or media emulation. Coordinates are
// client coordinates inside the test iframe; these offset them by the
// iframe's position on the runner page.

type MouseButton = "left" | "right" | "middle";
type Context = Parameters<BrowserCommand>[0];

async function frameOffset(context: Context) {
	const frame = await context.frame();
	const box = await (await frame.frameElement()).boundingBox();
	return { x: box?.x ?? 0, y: box?.y ?? 0 };
}

export const mouseMove: BrowserCommand<
	[x: number, y: number, steps?: number]
> = async (context, x, y, steps = 1) => {
	const offset = await frameOffset(context);
	await context.page.mouse.move(offset.x + x, offset.y + y, { steps });
};

export const mouseDown: BrowserCommand<
	[button?: MouseButton, clickCount?: number]
> = async (context, button = "left", clickCount = 1) => {
	await context.page.mouse.down({ button, clickCount });
};

export const mouseUp: BrowserCommand<
	[button?: MouseButton, clickCount?: number]
> = async (context, button = "left", clickCount = 1) => {
	await context.page.mouse.up({ button, clickCount });
};

export const mouseWheel: BrowserCommand<[dx: number, dy: number]> = async (
	context,
	dx,
	dy,
) => {
	await context.page.mouse.wheel(dx, dy);
};

/** page.keyboard.press: Playwright key names such as "Escape",
 * "Control+k" or "ArrowDown". */
export const keyPress: BrowserCommand<[key: string]> = async (context, key) => {
	await context.page.keyboard.press(key);
};

/** page.keyboard.type into the focused element. */
export const keyType: BrowserCommand<[text: string]> = async (
	context,
	text,
) => {
	await context.page.keyboard.type(text);
};

/** page.emulateMedia, e.g. { colorScheme: "dark" } or
 * { reducedMotion: "reduce" }. */
export const emulateMedia: BrowserCommand<
	[options: Record<string, string | null>]
> = async (context, options) => {
	await context.page.emulateMedia(options as never);
};

declare module "vitest/browser" {
	interface BrowserCommands {
		mouseMove(x: number, y: number, steps?: number): Promise<void>;
		mouseDown(button?: MouseButton, clickCount?: number): Promise<void>;
		mouseUp(button?: MouseButton, clickCount?: number): Promise<void>;
		mouseWheel(dx: number, dy: number): Promise<void>;
		keyPress(key: string): Promise<void>;
		keyType(text: string): Promise<void>;
		emulateMedia(options: Record<string, string | null>): Promise<void>;
	}
}
