import { expect, type Page, test } from "@playwright/test";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";

type MicMode = "ok" | "denied" | "no-device";

/** Replaces the microphone and MediaRecorder with fakes. */
async function fakeMicrophone(
	page: Page,
	opts: { mode?: MicMode; insecure?: boolean } = {},
) {
	await page.addInitScript(
		({ mode, insecure }) => {
			if (insecure) {
				Object.defineProperty(window, "isSecureContext", { value: false });
				Object.defineProperty(navigator, "mediaDevices", {
					value: undefined,
				});
				return;
			}
			const w = window as unknown as {
				__micStops: number;
				MediaRecorder: unknown;
			};
			w.__micStops = 0;
			class FakeRecorder {
				mimeType: string;
				state = "inactive";
				ondataavailable: ((e: { data: Blob }) => void) | null = null;
				onstop: (() => void) | null = null;
				constructor(_stream: unknown, options?: { mimeType?: string }) {
					this.mimeType = options?.mimeType ?? "audio/webm";
				}
				static isTypeSupported(type: string) {
					return type.startsWith("audio/webm");
				}
				start() {
					this.state = "recording";
				}
				stop() {
					this.state = "inactive";
					this.ondataavailable?.({
						data: new Blob(["fake-voice"], { type: this.mimeType }),
					});
					this.onstop?.();
				}
			}
			w.MediaRecorder = FakeRecorder;
			navigator.mediaDevices.getUserMedia = async () => {
				if (mode === "denied")
					throw new DOMException("Permission denied", "NotAllowedError");
				if (mode === "no-device")
					throw new DOMException("Requested device not found", "NotFoundError");
				return {
					getTracks: () => [
						{
							stop() {
								w.__micStops += 1;
							},
						},
					],
				} as unknown as MediaStream;
			};
		},
		{ mode: opts.mode ?? "ok", insecure: opts.insecure ?? false },
	);
}

async function openChat(page: Page, runs: unknown[] = []) {
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
			runs.push(args);
			return { stream: ["ok"], result: "" };
		},
	});
	await page.goto("/agents/assistant/chat");
	const composer = page.getByTestId("chat-composer");
	await expect(composer).toBeVisible();
	return {
		composer,
		mic: composer.getByTestId("voice-button"),
		send: composer.getByRole("button", { name: "Send message" }),
		input: composer.getByPlaceholder("Describe a task or ask a question"),
	};
}

test("records, transcribes, and inserts the transcript for review", async ({
	page,
}) => {
	await fakeMicrophone(page);
	let release: () => void = () => {};
	const gate = new Promise<void>((resolve) => {
		release = resolve;
	});
	const uploads: Array<{ type: string; body: string }> = [];
	await page.route("**/api/transcribe", async (route) => {
		const req = route.request();
		uploads.push({
			type: req.headers()["content-type"] ?? "",
			body: req.postData() ?? "",
		});
		await gate;
		await route.fulfill({
			json: { text: "turn on the lights", language: "en", duration_ms: 2000 },
		});
	});
	const runs: unknown[] = [];
	const { composer, mic, send, input } = await openChat(page, runs);

	await expect(mic).toHaveAccessibleName("Record voice message");
	await input.fill("Please");
	await mic.click();

	// Recording: indicator, duration, Cancel, and the mic becomes Stop.
	const recording = composer.getByTestId("voice-recording");
	await expect(recording).toBeVisible();
	await expect(recording.getByTestId("voice-elapsed")).toHaveText(/^0:0\d$/);
	await expect(recording.getByRole("button", { name: "Cancel" })).toBeVisible();
	await expect(mic).toHaveAccessibleName("Stop recording");
	await expect(send).toBeDisabled();

	await mic.click();
	// Transcribing: mic and Send are disabled until the transcript returns.
	await expect(composer.getByTestId("voice-transcribing")).toBeVisible();
	await expect(mic).toBeDisabled();
	await expect(send).toBeDisabled();
	release();

	await expect(input).toHaveValue("Please turn on the lights");
	await expect(composer.getByTestId("voice-transcribing")).toHaveCount(0);
	await expect(mic).toBeEnabled();
	await expect(send).toBeEnabled();
	expect(uploads).toEqual([
		{ type: "audio/webm;codecs=opus", body: "fake-voice" },
	]);
	// Nothing was sent: the user reviews and sends it.
	expect(runs).toHaveLength(0);
	expect(await page.evaluate(() => (window as any).__micStops)).toBe(1);
});

test("inserts the transcript at the caret", async ({ page }) => {
	await fakeMicrophone(page);
	await page.route("**/api/transcribe", (route) =>
		route.fulfill({ json: { text: "blue" } }),
	);
	const { mic, input } = await openChat(page);
	await input.fill("the car is fast");
	await input.evaluate((el: HTMLTextAreaElement) => el.setSelectionRange(8, 8));
	await mic.click();
	await mic.click();
	await expect(input).toHaveValue("the car blue is fast");
});

test("Cancel discards the recording", async ({ page }) => {
	await fakeMicrophone(page);
	let uploads = 0;
	await page.route("**/api/transcribe", (route) => {
		uploads += 1;
		return route.fulfill({ json: { text: "should not appear" } });
	});
	const { composer, mic, input } = await openChat(page);
	await mic.click();
	await composer.getByRole("button", { name: "Cancel" }).click();
	await expect(composer.getByTestId("voice-recording")).toHaveCount(0);
	await expect(mic).toHaveAccessibleName("Record voice message");
	await expect(input).toHaveValue("");
	expect(uploads).toBe(0);
	expect(await page.evaluate(() => (window as any).__micStops)).toBe(1);
});

test("explains a denied microphone permission", async ({ page }) => {
	await fakeMicrophone(page, { mode: "denied" });
	const { composer, mic, input } = await openChat(page);
	await mic.click();
	const error = composer.getByTestId("voice-error");
	await expect(error).toContainText("Microphone access was blocked");
	await expect(error).toContainText("site settings");
	// The composer stays usable, and dismissing returns to idle.
	await input.fill("typed instead");
	await error.getByRole("button", { name: "Dismiss" }).click();
	await expect(error).toHaveCount(0);
});

test("explains a missing microphone", async ({ page }) => {
	await fakeMicrophone(page, { mode: "no-device" });
	const { composer, mic } = await openChat(page);
	await mic.click();
	await expect(composer.getByTestId("voice-error")).toContainText(
		"No microphone was found",
	);
});

test("explains that voice input needs a secure page", async ({ page }) => {
	await fakeMicrophone(page, { insecure: true });
	const { composer, mic } = await openChat(page);
	await expect(mic).toHaveAttribute("aria-disabled", "true");
	await expect(mic).toHaveAttribute("title", /https:\/\/ or at localhost/);
	// aria-disabled keeps the button clickable so it can explain itself.
	await mic.click({ force: true });
	await expect(composer.getByTestId("voice-error")).toContainText(
		"Voice input needs a secure page",
	);
	await expect(composer.getByTestId("voice-recording")).toHaveCount(0);
});

test("shows install instructions when the engine is missing", async ({
	page,
}) => {
	await fakeMicrophone(page);
	await page.route("**/api/transcribe", (route) =>
		route.fulfill({
			status: 503,
			json: {
				error: "transcription unavailable: whisper-cli missing",
				missing: ["whisper-cli", "whisper model base.en"],
				install:
					"run `aviary deps install`, or Install on the System > Tools page",
			},
		}),
	);
	const { composer, mic, input } = await openChat(page);
	await mic.click();
	await mic.click();
	const error = composer.getByTestId("voice-error");
	await expect(error).toContainText(
		"missing whisper-cli, whisper model base.en",
	);
	await expect(error).toContainText("aviary deps install");
	await expect(input).toHaveValue("");
	await expect(mic).toBeEnabled();
});

test("shows a failed transcription", async ({ page }) => {
	await fakeMicrophone(page);
	await page.route("**/api/transcribe", (route) =>
		route.fulfill({
			status: 500,
			json: { error: "running whisper-cli: exit status 1" },
		}),
	);
	const { composer, mic } = await openChat(page);
	await mic.click();
	await mic.click();
	await expect(composer.getByTestId("voice-error")).toContainText(
		"Transcription failed: running whisper-cli: exit status 1",
	);
});

test("chat history shows the full voice transcript with an audio player", async ({
	page,
}) => {
	const transcript =
		"So here is a long voice note. ".repeat(20).trim() + " The end.";
	const path = "/data/media/incoming/signal/1_voice.ogg";
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
		config_validate: [],
		session_messages: [
			{
				id: "m1",
				session_id: "s1",
				role: "user",
				content: `[voice message 0:12, transcribed]: ${transcript}\n[file attached: voice.ogg (audio/ogg, 12.0 KB) saved at ${path}]`,
				media_urls: [`file://${path}`],
				timestamp: "2026-03-12T12:00:00Z",
			},
		],
	});
	await page.goto("/agents/assistant/chat");
	const message = page.getByTestId("chat-user-message");
	await expect(message).toContainText(
		`[voice message 0:12, transcribed]: ${transcript}`,
	);
	const audio = message.locator("audio");
	await expect(audio).toHaveCount(1);
	const src = await audio.getAttribute("src");
	expect(src).toContain("/api/media?");
	expect(new URL(src ?? "", "http://x").searchParams.get("path")).toBe(path);
});
