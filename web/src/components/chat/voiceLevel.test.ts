import { expect, test } from "vitest";
import {
	analyserSize,
	dBFS,
	isSilent,
	measureChannels,
	peakAcross,
} from "./voiceLevel";

/** A second of a 440 Hz sine at amplitude, as float32 samples. */
function sine(rate: number, amplitude: number): Float32Array {
	const out = new Float32Array(rate);
	for (let i = 0; i < rate; i++) {
		out[i] = amplitude * Math.sin((2 * Math.PI * 440 * i) / rate);
	}
	return out;
}

test("stereo input with signal only on the left channel isn't silent", () => {
	// A mic on input 1 of a stereo audio interface at low gain.
	const levels = measureChannels([sine(48000, 0.003), new Float32Array(48000)]);
	expect(levels[0].peak).toBeCloseTo(0.003, 4);
	expect(levels[0].rms).toBeCloseTo(0.003 / Math.SQRT2, 4);
	expect(levels[1]).toEqual({ peak: 0, rms: 0 });
	expect(peakAcross(levels)).toBeCloseTo(0.003, 4);
	expect(isSilent(peakAcross(levels))).toBe(false);
});

test("192 kHz input isn't silent", () => {
	const levels = measureChannels([
		sine(192000, 0.003),
		new Float32Array(192000),
	]);
	expect(isSilent(peakAcross(levels))).toBe(false);
	// A 100 ms poll at 192 kHz reads a window covering all of it.
	expect(analyserSize(192000, 0.1)).toBe(32768);
	expect(analyserSize(48000, 0.1)).toBe(8192);
	expect(analyserSize(16000, 0.1)).toBe(2048);
});

test("float32 samples keep levels under -60 dBFS 16-bit would lose", () => {
	// Full-scale float32 and a -55 dBFS signal both count.
	expect(isSilent(peakAcross(measureChannels([Float32Array.of(1, -1)])))).toBe(
		false,
	);
	const quiet = peakAcross(measureChannels([sine(48000, 0.0018)]));
	expect(dBFS(quiet)).toBeCloseTo(-54.9, 0);
	expect(isSilent(quiet)).toBe(false);
});

test("only a near-zero peak across every channel is silent", () => {
	expect(
		isSilent(
			peakAcross(
				measureChannels([new Float32Array(48000), new Float32Array(48000)]),
			),
		),
	).toBe(true);
	expect(isSilent(peakAcross(measureChannels([sine(48000, 0.0008)])))).toBe(
		true,
	);
	expect(isSilent(peakAcross(measureChannels([])))).toBe(true);
	expect(dBFS(0)).toBe(-120);
});
