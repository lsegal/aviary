import { computed, onBeforeUnmount, ref } from "vue";

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

/** The input level, 0..1 of full scale, below which the mic counts as silent. */
const SILENT_LEVEL = 0.01;

/** Shown when the server finds no speech in a recording. */
export const NO_SPEECH_MESSAGE =
	"No speech detected. Check that your microphone is on, unmuted, and selected as this browser's input, then try again.";

/** Shown instead when the level meter never picked up any sound. */
export const SILENT_MIC_MESSAGE =
	"No speech detected: the microphone picked up no sound. Check that it is unmuted and selected as this browser's input (and that the browser may use it in your system's privacy settings), then try again.";

/**
 * Measures a stream's input level with an AnalyserNode. Returns null when
 * the browser can't analyse the stream; recording still works without it.
 */
function levelMeter(stream: MediaStream): {
	read: () => number;
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
		const analyser = ctx.createAnalyser();
		analyser.fftSize = 1024;
		ctx.createMediaStreamSource(stream).connect(analyser);
		// Started after an await, the context can begin suspended and read
		// nothing but zeros.
		void ctx.resume().catch(() => {});
		const buf = new Float32Array(analyser.fftSize);
		const audio = ctx;
		return {
			read() {
				analyser.getFloatTimeDomainData(buf);
				let peak = 0;
				for (const v of buf) peak = Math.max(peak, Math.abs(v));
				return Math.min(1, peak);
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
	transcribe: (audio: Blob) => Promise<string>;
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
			stream = await navigator.mediaDevices.getUserMedia({ audio: true });
		} catch (e) {
			error.value = startError(e);
			return;
		}
		const track = stream.getAudioTracks?.()[0];
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
		meter = levelMeter(stream);
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
			const heardSound = !meter || peakLevel >= SILENT_LEVEL;
			release();
			if (cancelled) {
				state.value = "idle";
				return;
			}
			void finish(new Blob(chunks, { type }), durationMs, heardSound);
		};
		active.start();
		const startedAt = Date.now();
		elapsedMs.value = 0;
		timer = setInterval(() => {
			elapsedMs.value = Date.now() - startedAt;
			if (meter) {
				level.value = meter.read();
				peakLevel = Math.max(peakLevel, level.value);
			}
		}, 100);
		state.value = "recording";
	}

	async function finish(audio: Blob, durationMs: number, heardSound: boolean) {
		console.debug("voice: recorded", {
			type: audio.type,
			bytes: audio.size,
			durationMs,
			peakLevel,
			heardSound,
		});
		if (audio.size === 0) {
			state.value = "idle";
			error.value =
				"Nothing was recorded. Check your microphone and try again.";
			return;
		}
		state.value = "transcribing";
		try {
			const text = (await options.transcribe(audio)).trim();
			if (text) options.onTranscript(text);
			else error.value = heardSound ? NO_SPEECH_MESSAGE : SILENT_MIC_MESSAGE;
		} catch (e) {
			error.value = e instanceof Error ? e.message : String(e);
		} finally {
			state.value = "idle";
		}
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
	error?: string;
	missing?: string[];
	install?: string;
}

/** Uploads a recording to the server and returns its transcript. */
export async function transcribeRecording(
	audio: Blob,
	token: string | null,
): Promise<string> {
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
			text: data.text ?? "",
		});
		return data.no_speech ? "" : (data.text ?? "");
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
