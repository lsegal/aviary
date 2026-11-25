// Notification sounds (#1215): a small Web Audio synth bundled with the app,
// so no sound files are fetched and nothing needs a license. Each sound is a
// few short tones with a fast attack and an exponential decay.

interface Tone {
	/** Frequency in Hz. */
	freq: number;
	/** Start, after the sound starts, and length, in seconds. */
	at: number;
	length: number;
	type?: OscillatorType;
	/** Relative loudness, 0 to 1. */
	gain?: number;
}

const TONES: Record<string, Tone[]> = {
	chime: [
		{ freq: 880, at: 0, length: 0.5 },
		{ freq: 1318.5, at: 0.12, length: 0.7 },
	],
	bell: [
		{ freq: 659.3, at: 0, length: 1.2 },
		{ freq: 1318.5, at: 0, length: 0.8, gain: 0.35 },
		{ freq: 1977, at: 0, length: 0.5, gain: 0.15 },
	],
	ping: [{ freq: 1568, at: 0, length: 0.3 }],
	pop: [{ freq: 420, at: 0, length: 0.09, type: "triangle" }],
	marimba: [
		{ freq: 523.3, at: 0, length: 0.25, type: "triangle" },
		{ freq: 659.3, at: 0.1, length: 0.25, type: "triangle" },
		{ freq: 784, at: 0.2, length: 0.35, type: "triangle" },
	],
	alert: [
		{ freq: 988, at: 0, length: 0.14, type: "square", gain: 0.5 },
		{ freq: 988, at: 0.2, length: 0.14, type: "square", gain: 0.5 },
		{ freq: 1319, at: 0.4, length: 0.22, type: "square", gain: 0.5 },
	],
};

let ctx: AudioContext | null = null;

/** Every sound's length in seconds, for tests and the settings preview. */
export function soundLength(sound: string): number {
	return Math.max(0, ...(TONES[sound] ?? []).map((t) => t.at + t.length));
}

/**
 * Plays sound at volume (0 to 100). Browsers start audio only after the user
 * has interacted with the page; until then this does nothing.
 */
export function playSound(sound: string, volume: number): boolean {
	const tones = TONES[sound];
	if (!tones || volume <= 0) return false;
	const AudioCtx =
		window.AudioContext ??
		(window as unknown as { webkitAudioContext?: typeof AudioContext })
			.webkitAudioContext;
	if (!AudioCtx) return false;
	try {
		ctx ??= new AudioCtx();
		if (ctx.state === "suspended") void ctx.resume();
		const master = ctx.createGain();
		master.gain.value = (Math.min(volume, 100) / 100) * 0.4;
		master.connect(ctx.destination);
		const start = ctx.currentTime + 0.01;
		for (const tone of tones) {
			const osc = ctx.createOscillator();
			const env = ctx.createGain();
			osc.type = tone.type ?? "sine";
			osc.frequency.value = tone.freq;
			const t0 = start + tone.at;
			const peak = tone.gain ?? 1;
			env.gain.setValueAtTime(0.0001, t0);
			env.gain.exponentialRampToValueAtTime(peak, t0 + 0.01);
			env.gain.exponentialRampToValueAtTime(0.0001, t0 + tone.length);
			osc.connect(env).connect(master);
			osc.start(t0);
			osc.stop(t0 + tone.length + 0.05);
		}
		return true;
	} catch {
		return false;
	}
}
