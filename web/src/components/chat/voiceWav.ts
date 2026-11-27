import { measureChannels, peakAcross } from "./voiceLevel";

/** The sample rate whisper reads, and the rate recordings are uploaded at. */
export const SPEECH_RATE = 16000;

/**
 * The longest recording kept, in milliseconds. Recording stops on its own
 * at this length. At 16 kHz 16-bit mono that is about 19 MB, well under
 * the server's 50 MB upload limit.
 */
export const MAX_RECORDING_MS = 10 * 60 * 1000;

/** The level quiet speech is amplified to before 16-bit encoding. */
const SPEECH_PEAK = 0.7;
/** The most quiet speech is amplified (60 dB). */
const MAX_GAIN = 1000;

/** Averages channels into one, so a mic on one input of an interface isn't lost. */
export function downmix(channels: Float32Array[]): Float32Array {
	const length = channels.reduce((n, ch) => Math.max(n, ch.length), 0);
	const out = new Float32Array(length);
	if (!channels.length) return out;
	for (const ch of channels) {
		for (let i = 0; i < ch.length; i++) out[i] += ch[i];
	}
	for (let i = 0; i < length; i++) out[i] /= channels.length;
	return out;
}

/**
 * The gain that brings quiet speech peaking at peak up to a usable level,
 * so it keeps its precision in 16 bits. Loud audio is left alone.
 */
export function speechGain(peak: number): number {
	if (peak <= 0 || peak >= SPEECH_PEAK) return 1;
	return Math.min(SPEECH_PEAK / peak, MAX_GAIN);
}

/** Encodes mono samples, scaled by gain, as 16-bit PCM WAV at rate. */
export function encodeWAV(
	samples: Float32Array,
	rate: number,
	gain = 1,
): ArrayBuffer {
	const size = samples.length * 2;
	const buf = new ArrayBuffer(44 + size);
	const view = new DataView(buf);
	const text = (offset: number, s: string) => {
		for (let i = 0; i < s.length; i++)
			view.setUint8(offset + i, s.charCodeAt(i));
	};
	text(0, "RIFF");
	view.setUint32(4, 36 + size, true);
	text(8, "WAVEfmt ");
	view.setUint32(16, 16, true);
	view.setUint16(20, 1, true); // PCM
	view.setUint16(22, 1, true); // mono
	view.setUint32(24, rate, true);
	view.setUint32(28, rate * 2, true);
	view.setUint16(32, 2, true);
	view.setUint16(34, 16, true);
	text(36, "data");
	view.setUint32(40, size, true);
	for (let i = 0; i < samples.length; i++) {
		const v = Math.max(-1, Math.min(1, samples[i] * gain));
		view.setInt16(44 + i * 2, Math.round(v * 32767), true);
	}
	return buf;
}

/** A recording converted for upload. */
export interface SpeechAudio {
	/** 16 kHz mono 16-bit WAV. */
	wav: Blob;
	/** The loudest sample across every recorded channel, 0..1. */
	peak: number;
	durationMs: number;
}

/**
 * Converts recorded channels, already at SPEECH_RATE, to speech WAV: the
 * peak is measured on every channel before they are averaged, so a quiet
 * input on one channel isn't mistaken for silence.
 */
export function speechFromChannels(channels: Float32Array[]): SpeechAudio {
	const peak = peakAcross(measureChannels(channels));
	const mono = downmix(channels);
	const monoPeak = peakAcross(measureChannels([mono]));
	return {
		wav: new Blob([encodeWAV(mono, SPEECH_RATE, speechGain(monoPeak))], {
			type: "audio/wav",
		}),
		peak,
		durationMs: Math.round((mono.length / SPEECH_RATE) * 1000),
	};
}

/**
 * Decodes a MediaRecorder recording (WebM/Opus, MP4/AAC, Ogg) with the
 * browser's own decoders, resampled to 16 kHz by decoding into a 16 kHz
 * context, and converts it to speech WAV. Returns null when the browser
 * can't decode it, so the original recording can be uploaded instead.
 */
export async function decodeSpeech(audio: Blob): Promise<SpeechAudio | null> {
	const Ctx =
		window.OfflineAudioContext ??
		(
			window as unknown as {
				webkitOfflineAudioContext?: typeof OfflineAudioContext;
			}
		).webkitOfflineAudioContext;
	if (!Ctx) return null;
	try {
		const ctx = new Ctx(1, 1, SPEECH_RATE);
		const decoded = await ctx.decodeAudioData(await audio.arrayBuffer());
		let channels = Array.from({ length: decoded.numberOfChannels }, (_, i) =>
			decoded.getChannelData(i),
		);
		if (decoded.sampleRate !== SPEECH_RATE) {
			channels = await resample(Ctx, decoded);
		}
		return speechFromChannels(channels);
	} catch (e) {
		console.debug("voice: couldn't decode the recording in the browser", e);
		return null;
	}
}

/** Resamples a buffer to SPEECH_RATE, for browsers that decode at its own rate. */
async function resample(
	Ctx: typeof OfflineAudioContext,
	buffer: AudioBuffer,
): Promise<Float32Array[]> {
	const length = Math.max(1, Math.ceil(buffer.duration * SPEECH_RATE));
	const ctx = new Ctx(buffer.numberOfChannels, length, SPEECH_RATE);
	const source = ctx.createBufferSource();
	source.buffer = buffer;
	source.connect(ctx.destination);
	source.start();
	const out = await ctx.startRendering();
	return Array.from({ length: out.numberOfChannels }, (_, i) =>
		out.getChannelData(i),
	);
}
