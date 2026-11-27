import { beforeEach, describe, expect, it, vi } from "vitest";

const callTool = vi.fn();
vi.mock("./useMCP", () => ({ useMCP: () => ({ callTool }) }));

const { useProjectMode } = await import("./useProjectMode");

// The issue discovery choice shared by the eye menu and the board's "…"
// menu (#1488).
describe("useProjectMode", () => {
	beforeEach(() => callTool.mockReset());

	it("checks the effective mode and labels it", () => {
		const watch = useProjectMode(
			{ target: "o/r", manual: false, defaultMode: "watch" },
			() => {},
		);
		expect(watch.choice.value).toBe("watch");
		expect(watch.label.value).toBe("Issue discovery: Watch");
		const manual = useProjectMode(
			{ target: "o/r", manual: true, defaultMode: "watch" },
			() => {},
		);
		expect(manual.choice.value).toBe("manual");
	});

	it("stores an override only for the mode that differs from the default", async () => {
		const changed = vi.fn();
		const mode = useProjectMode(
			{ target: "o/r", manual: false, defaultMode: "watch" },
			changed,
		);
		await mode.pick("watch");
		expect(callTool).not.toHaveBeenCalled();
		await mode.pick("manual");
		expect(callTool).toHaveBeenCalledWith("project_update", {
			target: "o/r",
			mode: "manual",
		});
		expect(changed).toHaveBeenCalledWith("manual");
		expect(mode.saving.value).toBe(false);
	});

	it("keeps the error when the update fails", async () => {
		callTool.mockRejectedValueOnce(new Error("nope"));
		const changed = vi.fn();
		const mode = useProjectMode(
			{ target: "o/r", manual: true, defaultMode: "watch" },
			changed,
		);
		await mode.pick("watch");
		expect(mode.error.value).toBe("nope");
		expect(changed).not.toHaveBeenCalled();
		expect(mode.saving.value).toBe(false);
	});
});
