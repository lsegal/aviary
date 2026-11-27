import { afterEach, expect, it, vi } from "vitest";
import { useCopyText } from "./useCopyText";

// #1576: the instance ID copies from its popover and from the "…" menu.

afterEach(() => vi.unstubAllGlobals());

it("reports Copied, or the fallback when the clipboard refuses", async () => {
	const writeText = vi.fn().mockResolvedValue(undefined);
	vi.stubGlobal("navigator", { clipboard: { writeText } });
	const { status, copy } = useCopyText("Couldn't copy");
	expect(status.value).toBe("");
	await copy("7F3CA91B");
	expect(writeText).toHaveBeenCalledWith("7F3CA91B");
	expect(status.value).toBe("Copied");

	writeText.mockRejectedValue(new Error("denied"));
	await copy("7F3CA91B");
	expect(status.value).toBe("Couldn't copy");
});
