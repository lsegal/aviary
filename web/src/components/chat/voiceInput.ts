import { computed, onBeforeUnmount, ref } from "vue";
import {
	analyserSize,
	type ChannelLevel,
	dBFS,
	isSilent,
	measureChannels,
	peakAcross,
	SILENT_LEVEL,
} from "./voiceLevel";

export type VoiceState = "idle" | "recording" | "transcribing";

/** Why voice input can't work on this page, or "" when it can. */
export function voiceUnsupportedReason(): string {
	if (!window.isSecureContext || !navigator.mediaDevices?.getUserMedia) {
		return "Voice input needs a secure page. Open Aviary over https:// or at localhost to use the microphone.";
	}
	if (typeof MediaRecorder === "undefined") {
		return "This browser can't record audio.";
	}
	return "";
}

function recordingMimeType(): string | undefined {
	for (const type of ["audio/webm;codecs=opus", "audio/mp4", "audio/ogg"]) {
		if (MediaRecorder.isTypeSupported?.(type)) return type;
	}
	return undefined;
}

function startError(e: unknown): string {
	const name = e instanceof DOMException ? e.name : "";
	if (name === "NotAllowedError" || name === "SecurityError") {
		return "Microphone access was blocked. Allow the microphone for this site in your browser's address bar or site settings, then try again.";
	}
	if (name === "NotFoundError" || name === "OverconstrainedError") {
		return "No microphone was found. Connect one and try again.";
	}
	return `Couldn't start recording: ${e instanceof Error ? e.message : String(e)}`;
}

/** How often, in seconds, the level meter is read. */
const METER_INTERVAL = 0.1;

/** Where the chosen input device is remembered. */
const DEVICE_KEY = "aviary.voice.deviceId";

/**
 * Capture constraints for transcription. Echo cancellation, noise
 * suppression and auto gain can gate or squash audio-interface input, and
 * the server normalizes the level itself, so the raw signal is requested.
 */
export const CAPTURE_CONSTRAINTS: MediaTrackConstraints = {
	echoCancellation: false,
	noiseSuppression: false,
	autoGainControl: false,
};

/** Capture constraints for an input device, or the browser's default. */
export function captureConstraints(deviceId: string): MediaTrackConstraints {
	return deviceId
		? { ...CAPTURE_CONSTRAINTS, deviceId: { exact: deviceId } }
		: CAPTURE_CONSTRAINTS;
}

/** Shown when the recording had sound but no words were recognized. */
export const NO_WORDS_MESSAGE =
	"Couldn't make out any words. Try speaking closer or longer.";

/**
 * Shown instead when the recording's peak across every channel was near
 * zero, naming the input that was recorded since the browser may not have
 * used the one its settings show.
 */
export function silentMicMessage(label: string): string {
	const input = label ? `"${label}"` : "the microphone";
	return `No speech detected: ${input} picked up no sound. Check that it is unmuted and is the input you meant to use (pick another from the input menu while recording), and that the browser may use it in your system's privacy settings, then try again.`;
}

/** The server's verdict on a recording. */
export interface TranscribeResult {
	text: string;
	/**
	 * Whether the server measured the recording as silent across every
	 * channel, or undefined when it doesn't measure levels.
	 */
	noSignal?: boolean;
}

/**
 * Measures a stream's input level with one AnalyserNode per channel and
 * reports the loudest, so a mic on one input of a stereo audio interface
 * isn't halved by downmixing. Each window covers a whole poll interval at
 * the context's rate, and every channel's peak and RMS are kept for the
 * diagnostics. Returns null when the browser can't analyse the stream;
 * recording still works without it.
 */
function levelMeter(
	stream: MediaStream,
	channelCount: number,
): {
	sampleRate: number;
	read: () => number;
	levels: () => ChannelLevel[];
	close: () => void;
} | null {
	const Ctx =
		window.AudioContext ??
		(window as unknown as { webkitAudioContext?: typeof AudioContext })
			.webkitAudioContext;
	if (!Ctx) return null;
	let ctx: AudioContext | undefined;
	try {
		ctx = new Ctx();
		const source = ctx.createMediaStreamSource(stream);
		const splitter = ctx.createChannelSplitter(channelCount);
		source.connect(splitter);
		const size = analyserSize(ctx.sampleRate, METER_INTERVAL);
		const analysers = Array.from({ length: channelCount }, (_, i) => {
			const analyser = (ctx as AudioContext).createAnalyser();
			analyser.fftSize = size;
			splitter.connect(analyser, i);
			return analyser;
		});
		// Started after an await, the context can begin suspended and read
		// nothing but zeros.
		void ctx.resume().catch(() => {});
		const bufs = analysers.map(() => new Float32Array(size));
		const totals = analysers.map(() => ({ peak: 0, sum: 0, n: 0 }));
		const audio = ctx;
		return {
			sampleRate: audio.sampleRate,
			read() {
				analysers.forEach((analyser, i) => analyser.getFloatTimeDomainData(bufs[i]));
				const now = measureChannels(bufs);
				now.forEach((l, i) => {
					totals[i].peak = Math.max(totals[i].peak, l.peak);
					totals[i].sum += l.rms * l.rms;
					totals[i].n++;
				});
				return peakAcross(now);
			},
			levels() {
				return totals.map((t) => ({
					peak: t.peak,
					rms: t.n ? Math.sqrt(t.sum / t.n) : 0,
				}));
			},
			close() {
				void audio.close().catch(() => {});
			},
		};
	} catch {
		void ctx?.close().catch(() => {});
		return null;
	}
}

/**
 * Records speech with MediaRecorder and hands the recording to transcribe.
 * The transcript goes to onTranscript; it is never sent on its own.
 */
export function useVoiceInput(options: {
	transcribe: (audio: Blob) => Promise<TranscribeResult>;
	onTranscript: (text: string) => void;
}) {
	const state = ref<VoiceState>("idle");
	const elapsedMs = ref(0);
	const error = ref("");
	const unsupportedReason = ref(voiceUnsupportedReason());
	/** The live input level, 0..1, or null when it can't be measured. */
	const level = ref<number | null>(null);
	/** Why the microphone may not be capturing sound, shown while recording. */
	const micWarning = ref("");
	/** The audio inputs to choose from, once the browser lists them. */
	const devices = ref<{ id: string; label: string }[]>([]);
	/** The chosen input's deviceId, or "" for the browser's default. */
	const deviceId = ref(localStorage.getItem(DEVICE_KEY) ?? "");
	/** The input being recorded, as the browser names it. */
	const deviceLabel = ref("");

	let recorder: MediaRecorder | null = null;
	let stream: MediaStream | null = null;
	let chunks: Blob[] = [];
	let timer: ReturnType<typeof setInterval> | undefined;
	let cancelled = false;
	let meter: ReturnType<typeof levelMeter> = null;
	let peakLevel = 0;

	const busy = computed(() => state.value !== "idle");
	const elapsed = computed(() => {
		const secs = Math.floor(elapsedMs.value / 1000);
		return `${Math.floor(secs / 60)}:${String(secs % 60).padStart(2, "0")}`;
	});

	function release() {
		clearInterval(timer);
		timer = undefined;
		meter?.close();
		meter = null;
		level.value = null;
		for (const track of stream?.getTracks() ?? []) track.stop();
		stream = null;
		recorder = null;
	}

	async function start() {
		error.value = "";
		unsupportedReason.value = voiceUnsupportedReason();
		if (unsupportedReason.value) {
			error.value = unsupportedReason.value;
			return;
		}
		if (state.value !== "idle") return;
		try {
			stream = await navigator.mediaDevices.getUserMedia({
				audio: captureConstraints(deviceId.value),
			});
		} catch (e) {
			// A remembered input that is gone falls back to the default.
			if (
				deviceId.value &&
				e instanceof DOMException &&
				(e.name === "OverconstrainedError" || e.name === "NotFoundError")
			) {
				chooseDevice("");
				return start();
			}
			error.value = startError(e);
			return;
		}
		const track = stream.getAudioTracks?.()[0];
		const settings = track?.getSettings?.() ?? {};
		deviceLabel.value = track?.label ?? "";
		void listDevices();
		console.debug("voice: capture settings", {
			label: track?.label,
			deviceId: settings.deviceId,
			sampleRate: settings.sampleRate,
			channelCount: settings.channelCount,
			echoCancellation: settings.echoCancellation,
			noiseSuppression: settings.noiseSuppression,
			autoGainControl: settings.autoGainControl,
		});
		micWarning.value = "";
		if (track) {
			const update = () => {
				if (track.readyState === "ended") {
					micWarning.value =
						"The microphone stopped. Check that it is still connected.";
				} else if (track.muted) {
					micWarning.value =
						"The microphone is muted, so nothing is being recorded. Unmute it or pick another input.";
				} else {
					micWarning.value = "";
				}
			};
			track.onmute = update;
			track.onunmute = update;
			track.onended = update;
			update();
		}
		meter = levelMeter(stream, Math.max(1, settings.channelCount ?? 2));
		console.debug("voice: level meter", {
			contextSampleRate: meter?.sampleRate,
			trackSampleRate: settings.sampleRate,
			channels: Math.max(1, settings.channelCount ?? 2),
		});
		peakLevel = 0;
		level.value = meter ? 0 : null;
		const mimeType = recordingMimeType();
		const active = new MediaRecorder(
			stream,
			mimeType ? { mimeType } : undefined,
		);
		recorder = active;
		chunks = [];
		cancelled = false;
		active.ondataavailable = (e) => {
			if (e.data.size > 0) chunks.push(e.data);
		};
		active.onstop = () => {
			const type = active.mimeType || mimeType || "audio/webm";
			const durationMs = Date.now() - startedAt;
			const levels = meter?.levels() ?? null;
			const label = deviceLabel.value;
			release();
			if (cancelled) {
				state.value = "idle";
				return;
			}
			void finish(new Blob(chunks, { type }), durationMs, levels, label);
		};
		active.start();
		const startedAt = Date.now();
		elapsedMs.value = 0;
		timer = setInterval(() => {
			elapsedMs.value = Date.now() - startedAt;
			if (meter) {
				const now = meter.read();
				peakLevel = Math.max(peakLevel, now);
				// A track that has delivered nothing but zeros for a while is
				// almost always the wrong input, not a quiet one.
				if (
					!micWarning.value &&
					peakLevel === 0 &&
					Date.now() - startedAt >= 1500
				) {
					micWarning.value = `${deviceLabel.value ? `"${deviceLabel.value}"` : "This input"} is sending silence. Pick another input if it's the wrong one.`;
				}
				// Hold peaks briefly so the meter doesn't drop to nothing
				// between syllables.
				level.value = Math.max(now, (level.value ?? 0) * 0.8);
			}
		}, 100);
		state.value = "recording";
	}

	async function finish(
		audio: Blob,
		durationMs: number,
		levels: ChannelLevel[] | null,
		label: string,
	) {
		const peak = levels ? peakAcross(levels) : null;
		console.debug("voice: recorded", {
			type: audio.type,
			bytes: audio.size,
			durationMs,
			device: label,
			channels: levels?.map((l) => ({
				peakDBFS: dBFS(l.peak),
				rmsDBFS: dBFS(l.rms),
			})),
			peakDBFS: peak === null ? null : dBFS(peak),
			silentDBFS: dBFS(SILENT_LEVEL),
		});
		if (audio.size === 0) {
			state.value = "idle";
			error.value =
				"Nothing was recorded. Check your microphone and try again.";
			return;
		}
		state.value = "transcribing";
		try {
			const result = await options.transcribe(audio);
			const text = result.text.trim();
			if (text) {
				options.onTranscript(text);
				return;
			}
			// The server's measurement wins; the meter only decides when the
			// server doesn't measure, and a missing meter never claims silence.
			const silent =
				result.noSignal ?? (peak !== null && isSilent(peak));
			error.value = silent ? silentMicMessage(label) : NO_WORDS_MESSAGE;
		} catch (e) {
			error.value = e instanceof Error ? e.message : String(e);
		} finally {
			state.value = "idle";
		}
	}

	async function listDevices() {
		try {
			const all = await navigator.mediaDevices.enumerateDevices?.();
			devices.value = (all ?? [])
				.filter((d) => d.kind === "audioinput" && d.deviceId)
				.map((d, i) => ({
					id: d.deviceId,
					label: d.label || `Microphone ${i + 1}`,
				}));
		} catch {
			devices.value = [];
		}
	}

	function chooseDevice(id: string) {
		deviceId.value = id;
		if (id) localStorage.setItem(DEVICE_KEY, id);
		else localStorage.removeItem(DEVICE_KEY);
	}

	/** Records from another input, restarting a recording in progress. */
	async function switchDevice(id: string) {
		chooseDevice(id);
		if (state.value !== "recording") return;
		const done = new Promise<void>((resolve) => {
			const active = recorder;
			if (!active) return resolve();
			const onstop = active.onstop;
			active.onstop = (e) => {
				onstop?.call(active, e);
				resolve();
			};
		});
		cancel();
		await done;
		await start();
	}

	function stop() {
		if (state.value === "recording") recorder?.stop();
	}

	function cancel() {
		if (state.value !== "recording") return;
		cancelled = true;
		recorder?.stop();
	}

	function toggle() {
		if (state.value === "recording") stop();
		else void start();
	}

	function dismiss() {
		error.value = "";
	}

	onBeforeUnmount(() => {
		cancelled = true;
		recorder?.stop();
		release();
	});

	return {
		state,
		busy,
		elapsed,
		error,
		unsupportedReason,
		level,
		micWarning,
		devices,
		deviceId,
		deviceLabel,
		switchDevice,
		toggle,
		stop,
		cancel,
		dismiss,
	};
}

interface TranscribeResponse {
	text?: string;
	duration_ms?: number;
	no_speech?: boolean;
	no_signal?: boolean;
	peak_dbfs?: number;
	error?: string;
	missing?: string[];
	install?: string;
}

/** Uploads a recording to the server and returns its transcript. */
export async function transcribeRecording(
	audio: Blob,
	token: string | null,
): Promise<TranscribeResult> {
	const headers: Record<string, string> = {
		"Content-Type": audio.type || "audio/webm",
	};
	if (token) headers.Authorization = `Bearer ${token}`;
	let res: Response;
	try {
		res = await fetch("/api/transcribe", {
			method: "POST",
			headers,
			body: audio,
		});
	} catch (e) {
		throw new Error(
			`Transcription failed: ${e instanceof Error ? e.message : String(e)}`,
		);
	}
	let data: TranscribeResponse = {};
	try {
		data = (await res.json()) as TranscribeResponse;
	} catch {
		// Fall through to the status-based message.
	}
	if (res.ok) {
		console.debug("voice: transcribed", {
			durationMs: data.duration_ms,
			noSpeech: data.no_speech ?? false,
			noSignal: data.no_signal ?? false,
			peakDBFS: data.peak_dbfs,
			text: data.text ?? "",
		});
		return {
			text: data.no_speech ? "" : (data.text ?? ""),
			noSignal:
				data.peak_dbfs === undefined ? undefined : (data.no_signal ?? false),
		};
	}
	if (data.missing?.length) {
		throw new Error(
			`Voice transcription isn't installed on the server (missing ${data.missing.join(", ")}). To install it, ${data.install ?? "run `aviary deps install`"}.`,
		);
	}
	throw new Error(
		`Transcription failed: ${data.error || `${res.status} ${res.statusText}`}`,
	);
}

/** Inserts text at the caret, separated from its neighbors by spaces. */
export function insertAtCaret(
	value: string,
	text: string,
	caret: number | null,
): { value: string; caret: number } {
	const at = caret === null ? value.length : Math.min(caret, value.length);
	const before = value.slice(0, at);
	const after = value.slice(at);
	const lead = before && !/\s$/.test(before) ? " " : "";
	const trail = after && !/^\s/.test(after) ? " " : "";
	const inserted = lead + text + trail;
	return {
		value: before + inserted + after,
		caret: before.length + lead.length + text.length,
	};
}
