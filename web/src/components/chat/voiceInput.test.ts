import {
	afterEach,
	beforeEach,
	describe,
	expect,
	it,
	type Mock,
	vi,
} from "vitest";

// #1613: voice input runs on browser media APIs, so every one of them is a
// fake here: getUserMedia, MediaRecorder, AudioContext, localStorage, fetch.

const hooks = vi.hoisted(() => ({ unmount: [] as (() => void)[] }));

vi.mock("vue", async (importOriginal) => ({
	...(await importOriginal<typeof import("vue")>()),
	onBeforeUnmount: (fn: () => void) => {
		hooks.unmount.push(fn);
	},
}));

vi.mock("./voiceWav", async (importOriginal) => ({
	...(await importOriginal<typeof import("./voiceWav")>()),
	decodeSpeech: vi.fn(),
}));

import {
	CAPTURE_CONSTRAINTS,
	captureConstraints,
	insertAtCaret,
	NO_WORDS_MESSAGE,
	silentMicMessage,
	type TranscribeResult,
	transcribeRecording,
	useVoiceInput,
	voiceUnsupportedReason,
} from "./voiceInput";
import { decodeSpeech, MAX_RECORDING_MS } from "./voiceWav";

const decode = vi.mocked(decodeSpeech);

class FakeTrack {
	label = "USB Mic";
	readyState = "live";
	muted = false;
	settings: MediaTrackSettings = { channelCount: 1, sampleRate: 48000 };
	stop = vi.fn();
	onmute: (() => void) | null = null;
	onunmute: (() => void) | null = null;
	onended: (() => void) | null = null;
	getSettings() {
		return this.settings;
	}
}

class FakeStream {
	constructor(readonly track: FakeTrack | null = new FakeTrack()) {}
	getTracks() {
		return this.track ? [this.track] : [];
	}
	getAudioTracks() {
		return this.getTracks();
	}
}

/** The bytes the next recording delivers. */
let recordedBytes = 4;
/** The mime types MediaRecorder claims to support. */
let supportedTypes: string[] = [];

class FakeRecorder {
	static instances: FakeRecorder[] = [];
	static isTypeSupported(type: string) {
		return supportedTypes.includes(type);
	}
	mimeType: string;
	ondataavailable: ((e: { data: Blob }) => void) | null = null;
	onstop: ((e: Event) => void) | null = null;
	started = false;
	constructor(
		readonly stream: FakeStream,
		readonly options?: { mimeType: string },
	) {
		this.mimeType = options?.mimeType ?? "";
		FakeRecorder.instances.push(this);
	}
	start() {
		this.started = true;
	}
	stop() {
		this.ondataavailable?.({ data: new Blob([new Uint8Array(recordedBytes)]) });
		this.ondataavailable?.({ data: new Blob([]) });
		this.onstop?.(new Event("stop"));
	}
}

/** The sample value every analyser reads. */
let signal = 0;

class FakeAudioContext {
	static instances: FakeAudioContext[] = [];
	static failOnSource = false;
	sampleRate = 48000;
	splitterChannels = 0;
	analysers: { fftSize: number }[] = [];
	resume = vi.fn(() => Promise.reject(new Error("suspended")));
	close = vi.fn(() => Promise.reject(new Error("closed")));
	constructor() {
		FakeAudioContext.instances.push(this);
	}
	createMediaStreamSource() {
		if (FakeAudioContext.failOnSource) throw new Error("unsupported stream");
		return { connect: vi.fn() };
	}
	createChannelSplitter(n: number) {
		this.splitterChannels = n;
		return { connect: vi.fn() };
	}
	createAnalyser() {
		const analyser = {
			fftSize: 0,
			getFloatTimeDomainData(buf: Float32Array) {
				buf.fill(signal);
			},
		};
		this.analysers.push(analyser);
		return analyser;
	}
}

let storage: Map<string, string>;
let getUserMedia: ReturnType<typeof vi.fn>;
let enumerateDevices: ReturnType<typeof vi.fn> | undefined;
let stream: FakeStream;

/** The stream's audio track, which every test but one has. */
function liveTrack(): FakeTrack {
	if (!stream.track) throw new Error("stream has no track");
	return stream.track;
}

function stubBrowser(
	win: Record<string, unknown> = {
		isSecureContext: true,
		AudioContext: FakeAudioContext,
	},
) {
	vi.stubGlobal("window", win);
	vi.stubGlobal("navigator", {
		mediaDevices: { getUserMedia, enumerateDevices },
	});
	vi.stubGlobal("MediaRecorder", FakeRecorder);
	vi.stubGlobal("localStorage", {
		getItem: (k: string) => storage.get(k) ?? null,
		setItem: (k: string, v: string) => storage.set(k, v),
		removeItem: (k: string) => storage.delete(k),
	});
}

async function flush() {
	for (let i = 0; i < 20; i++) await Promise.resolve();
}

type Transcribe = Mock<(audio: Blob) => Promise<TranscribeResult>>;

function setup(
	transcribe: Transcribe = vi.fn().mockResolvedValue({ text: " hello " }),
) {
	const onTranscript = vi.fn();
	const voice = useVoiceInput({ transcribe, onTranscript });
	return { voice, transcribe, onTranscript };
}

/** Starts a recording and returns once it is underway. */
async function recording(transcribe?: Transcribe) {
	const s = setup(transcribe);
	s.voice.toggle();
	await flush();
	expect(s.voice.state.value).toBe("recording");
	return s;
}

beforeEach(() => {
	vi.useFakeTimers();
	vi.spyOn(console, "debug").mockImplementation(() => {});
	storage = new Map();
	stream = new FakeStream();
	getUserMedia = vi.fn(async () => stream);
	enumerateDevices = vi.fn(async () => [
		{ kind: "audioinput", deviceId: "mic-a", label: "Desk Mic" },
		{ kind: "audioinput", deviceId: "mic-b", label: "" },
		{ kind: "audioinput", deviceId: "", label: "Hidden" },
		{ kind: "videoinput", deviceId: "cam", label: "Camera" },
	]);
	recordedBytes = 4;
	supportedTypes = ["audio/webm;codecs=opus"];
	signal = 0;
	FakeRecorder.instances = [];
	FakeAudioContext.instances = [];
	FakeAudioContext.failOnSource = false;
	hooks.unmount = [];
	decode.mockReset();
	decode.mockResolvedValue(null);
	stubBrowser();
});

afterEach(() => {
	vi.useRealTimers();
	vi.unstubAllGlobals();
	vi.restoreAllMocks();
});

describe("voiceUnsupportedReason", () => {
	it("needs a secure page with getUserMedia", () => {
		stubBrowser({ isSecureContext: false });
		expect(voiceUnsupportedReason()).toMatch(/needs a secure page/);
		stubBrowser({ isSecureContext: true });
		vi.stubGlobal("navigator", {});
		expect(voiceUnsupportedReason()).toMatch(/needs a secure page/);
	});

	it("needs MediaRecorder", () => {
		vi.stubGlobal("MediaRecorder", undefined);
		expect(voiceUnsupportedReason()).toBe("This browser can't record audio.");
	});

	it("is empty when voice input can work", () => {
		expect(voiceUnsupportedReason()).toBe("");
	});
});

describe("captureConstraints", () => {
	it("requests raw audio from the default input", () => {
		expect(captureConstraints("")).toBe(CAPTURE_CONSTRAINTS);
		expect(CAPTURE_CONSTRAINTS).toEqual({
			echoCancellation: false,
			noiseSuppression: false,
			autoGainControl: false,
		});
	});

	it("pins a chosen input exactly", () => {
		expect(captureConstraints("mic-a")).toEqual({
			...CAPTURE_CONSTRAINTS,
			deviceId: { exact: "mic-a" },
		});
	});
});

describe("silentMicMessage", () => {
	it("names the recorded input, or the microphone", () => {
		expect(silentMicMessage("USB Mic")).toMatch(
			/^No speech detected: "USB Mic" picked up no sound\./,
		);
		expect(silentMicMessage("")).toMatch(
			/^No speech detected: the microphone picked up no sound\./,
		);
	});
});

describe("insertAtCaret", () => {
	it("appends at the end when there is no caret", () => {
		expect(insertAtCaret("hi", "there", null)).toEqual({
			value: "hi there",
			caret: 8,
		});
	});

	it("spaces the text from both neighbors", () => {
		expect(insertAtCaret("ab", "X", 1)).toEqual({ value: "a X b", caret: 3 });
	});

	it("reuses existing whitespace", () => {
		expect(insertAtCaret("a  b", "X", 2)).toEqual({ value: "a X b", caret: 3 });
	});

	it("inserts into an empty value without spaces", () => {
		expect(insertAtCaret("", "X", 0)).toEqual({ value: "X", caret: 1 });
	});

	it("clamps a caret past the end", () => {
		expect(insertAtCaret("a", "X", 9)).toEqual({ value: "a X", caret: 3 });
	});
});

describe("useVoiceInput start", () => {
	it("reports why voice input is unsupported instead of recording", async () => {
		stubBrowser({ isSecureContext: false });
		const { voice } = setup();
		voice.toggle();
		await flush();
		expect(voice.error.value).toMatch(/needs a secure page/);
		expect(voice.unsupportedReason.value).toBe(voice.error.value);
		expect(getUserMedia).not.toHaveBeenCalled();
		expect(voice.state.value).toBe("idle");
	});

	it("records raw audio from the remembered input", async () => {
		storage.set("aviary.voice.deviceId", "mic-a");
		const { voice } = await recording();
		expect(getUserMedia).toHaveBeenCalledWith({
			audio: captureConstraints("mic-a"),
		});
		expect(voice.deviceId.value).toBe("mic-a");
		expect(voice.deviceLabel.value).toBe("USB Mic");
		expect(voice.busy.value).toBe(true);
		expect(voice.devices.value).toEqual([
			{ id: "mic-a", label: "Desk Mic" },
			{ id: "mic-b", label: "Microphone 2" },
		]);
		const [rec] = FakeRecorder.instances;
		expect(rec.started).toBe(true);
		expect(rec.options).toEqual({ mimeType: "audio/webm;codecs=opus" });
		expect(voice.level.value).toBe(0);
		expect(voice.micWarning.value).toBe("");
	});

	it("picks the first supported recording format, or the default", async () => {
		supportedTypes = ["audio/ogg", "audio/mp4"];
		await recording();
		expect(FakeRecorder.instances[0].options).toEqual({
			mimeType: "audio/mp4",
		});

		supportedTypes = [];
		const s = setup();
		s.voice.toggle();
		await flush();
		expect(FakeRecorder.instances[1].options).toBeUndefined();
	});

	it.each([
		["NotAllowedError", /Microphone access was blocked/],
		["SecurityError", /Microphone access was blocked/],
		["NotFoundError", /No microphone was found/],
		["OverconstrainedError", /No microphone was found/],
		["AbortError", /^Couldn't start recording: busy$/],
	])("explains a %s from getUserMedia", async (name, message) => {
		getUserMedia.mockRejectedValue(new DOMException("busy", name));
		const { voice } = setup();
		voice.toggle();
		await flush();
		expect(voice.error.value).toMatch(message);
		expect(voice.state.value).toBe("idle");
	});

	it("explains a non-DOM failure", async () => {
		getUserMedia.mockRejectedValue("no way");
		const { voice } = setup();
		voice.toggle();
		await flush();
		expect(voice.error.value).toBe("Couldn't start recording: no way");
	});

	it("falls back to the default input when the remembered one is gone", async () => {
		storage.set("aviary.voice.deviceId", "gone");
		getUserMedia.mockRejectedValueOnce(
			new DOMException("gone", "OverconstrainedError"),
		);
		const { voice } = await recording();
		expect(getUserMedia).toHaveBeenNthCalledWith(1, {
			audio: captureConstraints("gone"),
		});
		expect(getUserMedia).toHaveBeenNthCalledWith(2, {
			audio: CAPTURE_CONSTRAINTS,
		});
		expect(voice.deviceId.value).toBe("");
		expect(storage.has("aviary.voice.deviceId")).toBe(false);
	});

	it("lists no inputs when the browser can't enumerate them", async () => {
		enumerateDevices = vi.fn().mockRejectedValue(new Error("denied"));
		stubBrowser();
		const failing = await recording();
		expect(failing.voice.devices.value).toEqual([]);

		enumerateDevices = undefined;
		stubBrowser();
		const missing = await recording();
		expect(missing.voice.devices.value).toEqual([]);
	});

	it("records a stream without an audio track", async () => {
		stream = new FakeStream(null);
		const { voice } = await recording();
		expect(voice.deviceLabel.value).toBe("");
		expect(voice.micWarning.value).toBe("");
		// No channel count reported: the meter splits two channels.
		expect(FakeAudioContext.instances[0].splitterChannels).toBe(2);
	});
});

describe("useVoiceInput while recording", () => {
	it("counts elapsed time and meters the loudest channel", async () => {
		const { voice } = await recording();
		const ctx = FakeAudioContext.instances[0];
		expect(ctx.splitterChannels).toBe(1);
		expect(ctx.analysers[0].fftSize).toBe(8192);
		expect(ctx.resume).toHaveBeenCalled();

		signal = 0.5;
		vi.advanceTimersByTime(1000);
		expect(voice.elapsed.value).toBe("0:01");
		expect(voice.level.value).toBe(0.5);

		// Peaks decay instead of dropping to nothing.
		signal = 0;
		vi.advanceTimersByTime(100);
		expect(voice.level.value).toBeCloseTo(0.4);

		vi.advanceTimersByTime(64_000);
		expect(voice.elapsed.value).toBe("1:05");
		expect(voice.micWarning.value).toBe("");
	});

	it("warns when the input sends only silence", async () => {
		const { voice } = await recording();
		vi.advanceTimersByTime(1400);
		expect(voice.micWarning.value).toBe("");
		vi.advanceTimersByTime(200);
		expect(voice.micWarning.value).toBe(
			`"USB Mic" is sending silence. Pick another input if it's the wrong one.`,
		);
	});

	it("names an unlabeled silent input generically", async () => {
		liveTrack().label = "";
		const { voice } = await recording();
		vi.advanceTimersByTime(1600);
		expect(voice.micWarning.value).toMatch(/^This input is sending silence/);
	});

	it("follows the track's mute and end events", async () => {
		const track = liveTrack();
		track.muted = true;
		const { voice } = await recording();
		expect(voice.micWarning.value).toMatch(/microphone is muted/);

		track.muted = false;
		track.onunmute?.();
		expect(voice.micWarning.value).toBe("");

		track.readyState = "ended";
		track.onended?.();
		expect(voice.micWarning.value).toMatch(/microphone stopped/);
	});

	it("stops at the recording length limit", async () => {
		const { voice, onTranscript } = await recording();
		vi.advanceTimersByTime(MAX_RECORDING_MS);
		await flush();
		expect(onTranscript).toHaveBeenCalledWith("hello");
		expect(voice.state.value).toBe("idle");
	});

	it("records without a meter when the browser has no AudioContext", async () => {
		stubBrowser({ isSecureContext: true });
		const { voice } = await recording();
		expect(voice.level.value).toBeNull();
		vi.advanceTimersByTime(2000);
		expect(voice.level.value).toBeNull();
		expect(voice.micWarning.value).toBe("");
	});

	it("uses the prefixed webkitAudioContext", async () => {
		stubBrowser({
			isSecureContext: true,
			webkitAudioContext: FakeAudioContext,
		});
		const { voice } = await recording();
		expect(FakeAudioContext.instances).toHaveLength(1);
		expect(voice.level.value).toBe(0);
	});

	it("records without a meter when the stream can't be analysed", async () => {
		FakeAudioContext.failOnSource = true;
		const { voice } = await recording();
		expect(voice.level.value).toBeNull();
		expect(FakeAudioContext.instances[0].close).toHaveBeenCalled();
	});
});

describe("useVoiceInput stop and transcription", () => {
	it("sends the recording and hands over the trimmed transcript", async () => {
		const { voice, transcribe, onTranscript } = await recording();
		const track = liveTrack();
		voice.stop();
		expect(voice.state.value).toBe("transcribing");
		await flush();
		const [audio] = transcribe.mock.calls[0];
		expect(audio).toBeInstanceOf(Blob);
		expect((audio as Blob).size).toBe(4);
		expect((audio as Blob).type).toBe("audio/webm;codecs=opus");
		expect(onTranscript).toHaveBeenCalledWith("hello");
		expect(voice.state.value).toBe("idle");
		expect(voice.error.value).toBe("");
		expect(voice.level.value).toBeNull();
		expect(track.stop).toHaveBeenCalled();
		expect(FakeAudioContext.instances[0].close).toHaveBeenCalled();
	});

	it("labels a recording with no format as webm", async () => {
		supportedTypes = [];
		const { voice, transcribe } = await recording();
		voice.stop();
		await flush();
		expect((transcribe.mock.calls[0][0] as Blob).type).toBe("audio/webm");
	});

	it("uploads the browser's decoded WAV when it has one", async () => {
		const wav = new Blob(["wav"], { type: "audio/wav" });
		decode.mockResolvedValue({ wav, durationMs: 1000, peak: 0.5 });
		const { voice, transcribe } = await recording();
		voice.stop();
		await flush();
		expect(transcribe).toHaveBeenCalledWith(wav);
	});

	it("reports a silent decoded recording without uploading it", async () => {
		decode.mockResolvedValue({
			wav: new Blob(["wav"]),
			durationMs: 1000,
			peak: 0,
		});
		const { voice, transcribe } = await recording();
		voice.stop();
		await flush();
		expect(transcribe).not.toHaveBeenCalled();
		expect(voice.error.value).toBe(silentMicMessage("USB Mic"));
		expect(voice.state.value).toBe("idle");
	});

	it("reports an empty recording", async () => {
		recordedBytes = 0;
		const { voice, transcribe } = await recording();
		voice.stop();
		await flush();
		expect(transcribe).not.toHaveBeenCalled();
		expect(voice.error.value).toBe(
			"Nothing was recorded. Check your microphone and try again.",
		);
		expect(voice.state.value).toBe("idle");
	});

	it.each([
		["the server measured silence", { noSignal: true }, true],
		["the server measured sound", { noSignal: false }, false],
		["the meter heard nothing", {}, true],
	])("explains no words when %s", async (_, verdict, silent) => {
		const { voice, onTranscript } = await recording(
			vi.fn().mockResolvedValue({ text: "  ", ...verdict }),
		);
		voice.stop();
		await flush();
		expect(onTranscript).not.toHaveBeenCalled();
		expect(voice.error.value).toBe(
			silent ? silentMicMessage("USB Mic") : NO_WORDS_MESSAGE,
		);
	});

	it("never claims silence without a meter or server measurement", async () => {
		stubBrowser({ isSecureContext: true });
		const { voice } = await recording(vi.fn().mockResolvedValue({ text: "" }));
		voice.stop();
		await flush();
		expect(voice.error.value).toBe(NO_WORDS_MESSAGE);
	});

	it("trusts the decoded audio over the server's silence verdict", async () => {
		decode.mockResolvedValue({
			wav: new Blob(["wav"]),
			durationMs: 1000,
			peak: 0.5,
		});
		const { voice } = await recording(
			vi.fn().mockResolvedValue({ text: "", noSignal: true }),
		);
		voice.stop();
		await flush();
		expect(voice.error.value).toBe(NO_WORDS_MESSAGE);
	});

	it("shows a transcription failure", async () => {
		const { voice } = await recording(
			vi.fn().mockRejectedValue(new Error("Transcription failed: 500")),
		);
		voice.stop();
		await flush();
		expect(voice.error.value).toBe("Transcription failed: 500");
		expect(voice.state.value).toBe("idle");

		voice.dismiss();
		expect(voice.error.value).toBe("");
	});

	it("shows a non-Error failure", async () => {
		const { voice } = await recording(vi.fn().mockRejectedValue("offline"));
		voice.stop();
		await flush();
		expect(voice.error.value).toBe("offline");
	});
});

describe("useVoiceInput cancel, switch and unmount", () => {
	it("discards a cancelled recording", async () => {
		const { voice, transcribe } = await recording();
		voice.cancel();
		await flush();
		expect(voice.state.value).toBe("idle");
		expect(transcribe).not.toHaveBeenCalled();
		expect(liveTrack().stop).toHaveBeenCalled();
	});

	it("ignores stop and cancel when not recording", () => {
		const { voice } = setup();
		voice.stop();
		voice.cancel();
		expect(voice.state.value).toBe("idle");
		expect(FakeRecorder.instances).toHaveLength(0);
	});

	it("remembers a chosen input without recording", async () => {
		const { voice } = setup();
		await voice.switchDevice("mic-b");
		expect(voice.deviceId.value).toBe("mic-b");
		expect(storage.get("aviary.voice.deviceId")).toBe("mic-b");
		expect(getUserMedia).not.toHaveBeenCalled();
	});

	it("restarts a recording on the chosen input", async () => {
		const { voice, transcribe } = await recording();
		await voice.switchDevice("mic-b");
		await flush();
		expect(transcribe).not.toHaveBeenCalled();
		expect(FakeRecorder.instances).toHaveLength(2);
		expect(getUserMedia).toHaveBeenLastCalledWith({
			audio: captureConstraints("mic-b"),
		});
		expect(voice.state.value).toBe("recording");
	});

	it("stops recording without transcribing on unmount", async () => {
		const { voice, transcribe } = await recording();
		expect(hooks.unmount).toHaveLength(1);
		hooks.unmount[0]();
		await flush();
		expect(transcribe).not.toHaveBeenCalled();
		expect(liveTrack().stop).toHaveBeenCalled();
		expect(voice.state.value).toBe("idle");
		expect(voice.level.value).toBeNull();
	});
});

describe("transcribeRecording", () => {
	function respond(status: number, body: unknown, statusText = "") {
		const fetchMock = vi.fn().mockResolvedValue({
			ok: status >= 200 && status < 300,
			status,
			statusText,
			json:
				body instanceof Error
					? () => Promise.reject(body)
					: () => Promise.resolve(body),
		});
		vi.stubGlobal("fetch", fetchMock);
		return fetchMock;
	}

	it("posts the audio with its type and the bearer token", async () => {
		const fetchMock = respond(200, { text: "hi", peak_dbfs: -20 });
		const audio = new Blob(["a"], { type: "audio/wav" });
		await expect(transcribeRecording(audio, "tok")).resolves.toEqual({
			text: "hi",
			noSignal: false,
		});
		expect(fetchMock).toHaveBeenCalledWith("/api/transcribe", {
			method: "POST",
			headers: { "Content-Type": "audio/wav", Authorization: "Bearer tok" },
			body: audio,
		});
	});

	it("defaults to webm without a token or level", async () => {
		const fetchMock = respond(200, {});
		await expect(transcribeRecording(new Blob(["a"]), null)).resolves.toEqual({
			text: "",
			noSignal: undefined,
		});
		expect(fetchMock.mock.calls[0][1].headers).toEqual({
			"Content-Type": "audio/webm",
		});
	});

	it("drops text the server marked as no speech", async () => {
		respond(200, {
			text: "[noise]",
			no_speech: true,
			no_signal: true,
			peak_dbfs: -90,
		});
		await expect(transcribeRecording(new Blob(["a"]), null)).resolves.toEqual({
			text: "",
			noSignal: true,
		});
	});

	it("wraps a network failure", async () => {
		vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new Error("offline")));
		await expect(transcribeRecording(new Blob(["a"]), null)).rejects.toThrow(
			"Transcription failed: offline",
		);
		vi.stubGlobal("fetch", vi.fn().mockRejectedValue("down"));
		await expect(transcribeRecording(new Blob(["a"]), null)).rejects.toThrow(
			"Transcription failed: down",
		);
	});

	it("explains missing server dependencies", async () => {
		respond(503, { missing: ["whisper", "model"], install: "run setup" });
		await expect(transcribeRecording(new Blob(["a"]), null)).rejects.toThrow(
			"Voice transcription isn't installed on the server (missing whisper, model). To install it, run setup.",
		);
		respond(503, { missing: ["whisper"] });
		await expect(transcribeRecording(new Blob(["a"]), null)).rejects.toThrow(
			"To install it, run `aviary deps install`.",
		);
	});

	it("reports the server's error, or the status", async () => {
		respond(400, { error: "too long" });
		await expect(transcribeRecording(new Blob(["a"]), null)).rejects.toThrow(
			"Transcription failed: too long",
		);
		respond(502, new Error("not json"), "Bad Gateway");
		await expect(transcribeRecording(new Blob(["a"]), null)).rejects.toThrow(
			"Transcription failed: 502 Bad Gateway",
		);
	});
});
