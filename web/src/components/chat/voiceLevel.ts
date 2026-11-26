/**
 * The input level, 0..1 of full scale, below which the mic counts as silent
 * (-60 dBFS, the server's floor too). Quiet speech from a low-gain audio
 * interface can peak around -50 dBFS.
 */
export const SILENT_LEVEL = 0.001;

/** One channel's level, each 0..1 of full scale. */
export interface ChannelLevel {
	peak: number;
	rms: number;
}

/** Measures the peak and RMS of each channel's float samples. */
export function measureChannels(channels: ArrayLike<number>[]): ChannelLevel[] {
	return channels.map((samples) => {
		let peak = 0;
		let sum = 0;
		for (let i = 0; i < samples.length; i++) {
			const v = samples[i];
			peak = Math.max(peak, Math.abs(v));
			sum += v * v;
		}
		return {
			peak: Math.min(1, peak),
			rms: samples.length ? Math.sqrt(sum / samples.length) : 0,
		};
	});
}

/** The loudest peak across every channel, so one live input is enough. */
export function peakAcross(levels: ChannelLevel[]): number {
	return levels.reduce((peak, l) => Math.max(peak, l.peak), 0);
}

/** Whether a peak, measured across all channels, is effectively silence. */
export function isSilent(peak: number): boolean {
	return peak < SILENT_LEVEL;
}

/**
 * The AnalyserNode window, a power of two, that covers interval seconds of
 * audio at sampleRate, so reading it that often misses no samples. At
 * 192 kHz a 100 ms poll needs the 32768-sample maximum.
 */
export function analyserSize(sampleRate: number, interval: number): number {
	let size = 32;
	while (size < sampleRate * interval && size < 32768) size *= 2;
	return size;
}

/** Renders a 0..1 level in decibels relative to full scale, floored at -120. */
export function dBFS(level: number): number {
	if (level <= 0) return -120;
	return Math.max(-120, Math.round(200 * Math.log10(level)) / 10);
}
