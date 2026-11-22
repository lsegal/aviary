/** The verdicts task_analyze gives a task as it is edited. */
export type TaskVerdict =
	| "deterministic"
	| "partial"
	| "nondeterministic"
	| "script"
	| "project";

/** One run's estimated token use, and cost when the model's pricing is known. */
export interface TaskRunEstimate {
	available: boolean;
	reason?: string;
	min_tokens: number;
	max_tokens: number;
	min_cost_usd?: number;
	max_cost_usd?: number;
}

/** task_analyze's preview of what precompiling a task would do. */
export interface TaskAnalysis {
	verdict: TaskVerdict;
	will_precompile: boolean;
	reason?: string;
	model_steps?: string[];
	model?: string;
	estimate: TaskRunEstimate;
}

/** Parses task_analyze's result, or returns null when it is not one. */
export function parseTaskAnalysis(text: string): TaskAnalysis | null {
	try {
		const v = JSON.parse(text) as Partial<TaskAnalysis> | null;
		if (!v || typeof v !== "object" || typeof v.verdict !== "string")
			return null;
		if (!v.estimate || typeof v.estimate !== "object") return null;
		return v as TaskAnalysis;
	} catch {
		return null;
	}
}

/** A token count, shortened past a thousand: 950, 2.3k, 12k, 1.4M. */
export function formatTokens(n: number): string {
	if (n < 1000) return String(Math.round(n));
	if (n < 999_500) return shorten(n / 1000, "k");
	return shorten(n / 1_000_000, "M");
}

function shorten(v: number, unit: string): string {
	return `${v < 10 ? v.toFixed(1).replace(/\.0$/, "") : Math.round(v)}${unit}`;
}

/** A USD amount with enough precision for a fraction of a cent. */
export function formatUSD(n: number): string {
	if (n >= 1) return `$${n.toFixed(2)}`;
	if (n >= 0.01) return `$${n.toFixed(3)}`;
	return `$${n.toPrecision(2)}`;
}

/** The estimate as shown after the verdict, such as "≈ 2.3k–12k tokens/run". */
export function formatEstimate(e: TaskRunEstimate): string {
	if (!e.available)
		return `Estimate unavailable: ${e.reason || "depends on the task's input"}`;
	if (e.max_tokens <= 0) return "≈ 0 tokens/run";
	return formatRange(e, 1, "run");
}

/**
 * The estimate over runs runs in 30 days, such as "≈ 1.4M–3.7M tokens/month",
 * or "" when there is nothing to project: no recurring runs, an unavailable
 * estimate, or runs that need no tokens.
 */
export function formatMonthlyEstimate(
	e: TaskRunEstimate,
	runs: number,
): string {
	if (runs <= 0 || !e.available || e.max_tokens <= 0) return "";
	return formatRange(e, runs, "month");
}

function formatRange(e: TaskRunEstimate, times: number, per: string): string {
	let text = `≈ ${span(formatTokens, e.min_tokens * times, e.max_tokens * times)} tokens/${per}`;
	if (e.min_cost_usd != null && e.max_cost_usd != null)
		text += ` (${span(formatUSD, e.min_cost_usd * times, e.max_cost_usd * times)})`;
	return text;
}

/** "min–max", or one value when both format the same, such as "10k". */
function span(format: (n: number) => string, min: number, max: number): string {
	const lo = format(min);
	const hi = format(max);
	return lo === hi ? hi : `${lo}–${hi}`;
}
