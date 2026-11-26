import { writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { expect, test } from "@playwright/test";
import { openChat } from "./helpers/voice";

/**
 * Writes a WAV that stands in for a stereo USB audio interface with a mic
 * on input 1 at low gain: a -50 dBFS tone on the left channel, silence on
 * the right.
 */
function quietLeftChannelWAV(): string {
	const rate = 48000;
	const frames = rate * 2;
	const buf = Buffer.alloc(44 + frames * 4);
	buf.write("RIFF", 0);
	buf.writeUInt32LE(36 + frames * 4, 4);
	buf.write("WAVEfmt ", 8);
	buf.writeUInt32LE(16, 16);
	buf.writeUInt16LE(1, 20);
	buf.writeUInt16LE(2, 22);
	buf.writeUInt32LE(rate, 24);
	buf.writeUInt32LE(rate * 4, 28);
	buf.writeUInt16LE(4, 32);
	buf.writeUInt16LE(16, 34);
	buf.write("data", 36);
	buf.writeUInt32LE(frames * 4, 40);
	for (let i = 0; i < frames; i++) {
		const v = Math.round(
			0.003 * 32767 * Math.sin((2 * Math.PI * 440 * i) / rate),
		);
		buf.writeInt16LE(v, 44 + i * 4);
	}
	const path = join(tmpdir(), `aviary-quiet-left-${process.pid}.wav`);
	writeFileSync(path, buf);
	return path;
}

// Chromium's fake capture device feeds the WAV, looped, through the real
// getUserMedia and MediaRecorder.
test.use({
	permissions: ["microphone"],
	launchOptions: {
		args: [
			"--use-fake-ui-for-media-stream",
			"--use-fake-device-for-media-stream",
			`--use-file-for-fake-audio-capture=${quietLeftChannelWAV()}`,
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
		// -50 dBFS on one channel of two still reads clearly on the meter.
		.toBeGreaterThan(10);
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

test("requests raw capture so audio-interface input isn't processed", async ({
	page,
}) => {
	await page.addInitScript(() => {
		const w = window as unknown as { __constraints: unknown[] };
		w.__constraints = [];
		const getUserMedia = navigator.mediaDevices.getUserMedia.bind(
			navigator.mediaDevices,
		);
		navigator.mediaDevices.getUserMedia = (constraints) => {
			w.__constraints.push(constraints);
			return getUserMedia(constraints);
		};
	});
	await page.route("**/api/transcribe", (route) =>
		route.fulfill({ json: { text: "quiet words" } }),
	);
	const { composer, mic, input } = await openChat(page);
	await mic.click();
	await expect(composer.getByTestId("voice-level")).toBeVisible();
	await mic.click();
	await expect(input).toHaveValue("quiet words");
	expect(
		await page.evaluate(
			() => (window as unknown as { __constraints: unknown[] }).__constraints,
		),
	).toEqual([
		{
			audio: {
				echoCancellation: false,
				noiseSuppression: false,
				autoGainControl: false,
			},
		},
	]);
});

test("records from the input chosen in the composer's input menu", async ({
	page,
}) => {
	await page.addInitScript(() => {
		const w = window as unknown as { __constraints: unknown[] };
		w.__constraints = [];
		const getUserMedia = navigator.mediaDevices.getUserMedia.bind(
			navigator.mediaDevices,
		);
		navigator.mediaDevices.getUserMedia = (constraints) => {
			w.__constraints.push(constraints);
			return getUserMedia(constraints);
		};
	});
	await page.route("**/api/transcribe", (route) =>
		route.fulfill({ json: { text: "switched" } }),
	);
	const { composer, mic, input } = await openChat(page);
	await mic.click();
	const menu = composer.getByTestId("voice-device");
	await expect(menu).toBeVisible();
	const options = await menu
		.locator("option")
		.evaluateAll((els) => els.map((el) => (el as HTMLOptionElement).value));
	expect(options.length).toBeGreaterThan(1);
	const current = await menu.inputValue();
	const other =
		options.find((id) => id !== "default" && id !== current) ?? options[1];
	await menu.selectOption(other);
	// The recording restarts on the chosen input and still transcribes.
	await expect(menu).toHaveValue(other);
	await expect(composer.getByTestId("voice-recording")).toBeVisible();
	await mic.click();
	await expect(input).toHaveValue("switched");
	const constraints = await page.evaluate(
		() => (window as unknown as { __constraints: unknown[] }).__constraints,
	);
	expect(constraints.at(-1)).toEqual({
		audio: {
			echoCancellation: false,
			noiseSuppression: false,
			autoGainControl: false,
			deviceId: { exact: other },
		},
	});
	expect(
		await page.evaluate(() => localStorage.getItem("aviary.voice.deviceId")),
	).toBe(other);
});
