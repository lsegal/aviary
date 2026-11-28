import { expect, type Page, test } from "@playwright/test";
import { openChat } from "./helpers/voice";

// The mocked streams play through an AudioContext, which needs no gesture.
test.use({
	launchOptions: { args: ["--autoplay-policy=no-user-gesture-required"] },
});

/**
 * Replaces getUserMedia with a 192 kHz stereo stream, like a 2-channel USB
 * audio interface, carrying a tone at amplitude on channel 1 only (0 for
 * digital silence). The real MediaRecorder records it.
 */
async function stereoMicrophone(page: Page, amplitude: number) {
	await page.addInitScript((amp) => {
		navigator.mediaDevices.getUserMedia = async () => {
			const ctx = new AudioContext({ sampleRate: 192000 });
			await ctx.resume();
			const osc = ctx.createOscillator();
			osc.frequency.value = 440;
			const gain = ctx.createGain();
			gain.gain.value = amp;
			const merger = ctx.createChannelMerger(2);
			osc.connect(gain).connect(merger, 0, 0);
			const dest = ctx.createMediaStreamDestination();
			dest.channelCount = 2;
			merger.connect(dest);
			osc.start();
			return dest.stream;
		};
	}, amplitude);
}

test("transcribes a 192 kHz stereo stream with signal on channel 1 only", async ({
	page,
}) => {
	// About -50 dBFS: a mic on input 1 of an interface at low gain.
	await stereoMicrophone(page, 0.003);
	const uploads: Array<{ type: string; body: Buffer }> = [];
	await page.route("**/api/transcribe", (route) => {
		uploads.push({
			type: route.request().headers()["content-type"] ?? "",
			body: route.request().postDataBuffer() ?? Buffer.alloc(0),
		});
		return route.fulfill({ json: { text: "hello there", peak_dbfs: -50 } });
	});
	const { composer, mic, input } = await openChat(page);
	await mic.click();
	const meter = composer.getByTestId("voice-level");
	await expect(meter).toBeVisible();
	await expect
		.poll(async () => Number(await meter.getAttribute("aria-valuenow")), {
			timeout: 5000,
		})
		.toBeGreaterThan(10);
	await expect(composer.getByTestId("voice-elapsed")).toHaveText("0:02");
	await expect(composer.getByTestId("voice-mic-warning")).toHaveCount(0);
	await mic.click();
	await expect(input).toHaveValue("hello there");
	expect(uploads).toHaveLength(1);
	// The browser decoded the WebM/Opus recording and uploads 16 kHz mono
	// 16-bit WAV, so the server needs no ffmpeg.
	const { type, body } = uploads[0];
	expect(type).toBe("audio/wav");
	expect(body.toString("ascii", 0, 4)).toBe("RIFF");
	expect(body.toString("ascii", 8, 12)).toBe("WAVE");
	expect(body.readUInt16LE(20)).toBe(1);
	expect(body.readUInt16LE(22)).toBe(1);
	expect(body.readUInt32LE(24)).toBe(16000);
	expect(body.readUInt16LE(34)).toBe(16);
	// About two seconds of audio, with channel 1's tone kept and amplified.
	const samples = (body.length - 44) / 2;
	expect(samples).toBeGreaterThan(16000);
	let peak = 0;
	for (let i = 44; i + 1 < body.length; i += 2) {
		peak = Math.max(peak, Math.abs(body.readInt16LE(i)));
	}
	expect(peak / 32767).toBeGreaterThan(0.3);
	await expect(composer.getByTestId("voice-error")).toHaveCount(0);
});

test("says no words, not no sound, when a stereo recording with signal has no speech", async ({
	page,
}) => {
	await stereoMicrophone(page, 0.003);
	let attempted = false;
	// An engine that doesn't measure levels: the browser's meter decides.
	await page.route("**/api/transcribe", (route) => {
		attempted = true;
		return route.fulfill({ json: { no_speech: true } });
	});
	const { composer, mic } = await openChat(page);
	await mic.click();
	await expect
		.poll(async () =>
			Number(
				await composer.getByTestId("voice-level").getAttribute("aria-valuenow"),
			),
		)
		.toBeGreaterThan(10);
	await mic.click();
	const error = composer.getByTestId("voice-error");
	await expect(error).toContainText(
		"Couldn't make out any words. Try speaking closer or longer.",
	);
	expect(attempted).toBe(true);
});

test("warns live and reports no sound for a digitally silent input without uploading it", async ({
	page,
}) => {
	await stereoMicrophone(page, 0);
	let uploads = 0;
	await page.route("**/api/transcribe", (route) => {
		uploads += 1;
		return route.fulfill({ json: { text: "should not appear" } });
	});
	const { composer, mic, input } = await openChat(page);
	await mic.click();
	await expect(composer.getByTestId("voice-mic-warning")).toContainText(
		"is sending silence",
	);
	await mic.click();
	await expect(composer.getByTestId("voice-error")).toContainText(
		"picked up no sound",
	);
	// The decoded recording is silent, so the browser says so at once.
	expect(uploads).toBe(0);
	await expect(input).toHaveValue("");
});
