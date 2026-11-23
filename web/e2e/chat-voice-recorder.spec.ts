import { expect, test } from "@playwright/test";
import { openChat } from "./helpers/voice";

// Chromium's fake capture device feeds a beeping test signal through the
// real getUserMedia and MediaRecorder.
test.use({
	permissions: ["microphone"],
	launchOptions: {
		args: [
			"--use-fake-ui-for-media-stream",
			"--use-fake-device-for-media-stream",
		],
	},
});

test("uploads a complete WebM recording and shows the input level", async ({
	page,
}) => {
	const uploads: Array<{ type: string; body: Buffer }> = [];
	await page.route("**/api/transcribe", (route) => {
		const req = route.request();
		uploads.push({
			type: req.headers()["content-type"] ?? "",
			body: req.postDataBuffer() ?? Buffer.alloc(0),
		});
		return route.fulfill({ json: { text: "test one two three" } });
	});
	const { composer, mic, input } = await openChat(page);
	await mic.click();
	const meter = composer.getByTestId("voice-level");
	await expect(meter).toBeVisible();
	await expect
		.poll(async () => Number(await meter.getAttribute("aria-valuenow")), {
			timeout: 5000,
		})
		.toBeGreaterThan(0);
	await expect(composer.getByTestId("voice-elapsed")).toHaveText("0:02");
	await mic.click();
	await expect(input).toHaveValue("test one two three");

	expect(uploads).toHaveLength(1);
	const [upload] = uploads;
	expect(upload.type).toMatch(/^audio\/webm/);
	// The whole file, header included: EBML magic, and Opus at roughly
	// 1 KB or more per second rather than a few hundred bytes.
	expect([...upload.body.subarray(0, 4)]).toEqual([0x1a, 0x45, 0xdf, 0xa3]);
	expect(upload.body.length).toBeGreaterThan(2000);
});
