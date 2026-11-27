import { describe, expect, it } from "vitest";
import {
	downmix,
	encodeWAV,
	SPEECH_RATE,
	speechFromChannels,
	speechGain,
} from "./voiceWav";

function tone(seconds: number, amplitude: number): Float32Array {
	const out = new Float32Array(seconds * SPEECH_RATE);
	for (let i = 0; i < out.length; i++) {
		out[i] = amplitude * Math.sin((2 * Math.PI * 440 * i) / SPEECH_RATE);
	}
	return out;
}

/** Reads a WAV header and its 16-bit samples as 0..1 floats. */
async function readWAV(blob: Blob) {
	const view = new DataView(await blob.arrayBuffer());
	const text = (o: number, n: number) =>
		String.fromCharCode(
			...Array.from({ length: n }, (_, i) => view.getUint8(o + i)),
		);
	const size = view.getUint32(40, true);
	const samples = Array.from(
		{ length: size / 2 },
		(_, i) => view.getInt16(44 + i * 2, true) / 32767,
	);
	return {
		riff: text(0, 4),
		wave: text(8, 4),
		format: view.getUint16(20, true),
		channels: view.getUint16(22, true),
		rate: view.getUint32(24, true),
		bits: view.getUint16(34, true),
		samples,
		peak: samples.reduce((p, v) => Math.max(p, Math.abs(v)), 0),
	};
}

describe("downmix", () => {
	it("averages every channel", () => {
		const out = downmix([
			Float32Array.from([1, 0.5]),
			Float32Array.from([0, 0.5]),
		]);
		expect(Array.from(out)).toEqual([0.5, 0.5]);
	});

	it("keeps a mono recording as is", () => {
		expect(Array.from(downmix([Float32Array.from([0.25, -0.25])]))).toEqual([
			0.25, -0.25,
		]);
	});
});

describe("speechGain", () => {
	it("amplifies quiet speech, capped at 60 dB", () => {
		expect(speechGain(0.07)).toBeCloseTo(10);
		expect(speechGain(0.0001)).toBe(1000);
	});

	it("leaves loud audio and silence alone", () => {
		expect(speechGain(0.9)).toBe(1);
		expect(speechGain(0)).toBe(1);
	});
});

describe("encodeWAV", () => {
	it("writes 16 kHz mono 16-bit PCM", async () => {
		const wav = await readWAV(
			new Blob([encodeWAV(Float32Array.from([0, 1, -1, 2]), SPEECH_RATE)]),
		);
		expect(wav).toMatchObject({
			riff: "RIFF",
			wave: "WAVE",
			format: 1,
			channels: 1,
			rate: 16000,
			bits: 16,
		});
		// Out-of-range samples are clipped.
		expect(wav.samples).toEqual([0, 1, -1, 1]);
	});
});

describe("speechFromChannels", () => {
	it("keeps signal that is only on the left channel", async () => {
		const left = tone(1, 0.003);
		const speech = speechFromChannels([left, new Float32Array(left.length)]);
		expect(speech.peak).toBeCloseTo(0.003, 4);
		expect(speech.durationMs).toBe(1000);
		const wav = await readWAV(speech.wav);
		expect(speech.wav.type).toBe("audio/wav");
		expect(wav.channels).toBe(1);
		expect(wav.rate).toBe(16000);
		expect(wav.samples).toHaveLength(16000);
		// Halved by the average, then amplified for whisper.
		expect(wav.peak).toBeGreaterThan(0.6);
	});

	it("measures digital silence as silent", () => {
		const speech = speechFromChannels([new Float32Array(16000)]);
		expect(speech.peak).toBe(0);
	});
});
