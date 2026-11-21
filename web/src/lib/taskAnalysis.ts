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

/** A token count, shortened past a thousand: 950, 2.3k, 12k. */
export function formatTokens(n: number): string {
	if (n < 1000) return String(Math.round(n));
	const k = n / 1000;
	return `${k < 10 ? k.toFixed(1).replace(/\.0$/, "") : Math.round(k)}k`;
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
	const tokens =
		e.min_tokens === e.max_tokens
			? formatTokens(e.max_tokens)
			: `${formatTokens(e.min_tokens)}–${formatTokens(e.max_tokens)}`;
	let text = `≈ ${tokens} tokens/run`;
	if (e.min_cost_usd != null && e.max_cost_usd != null)
		text += ` (${formatUSD(e.min_cost_usd)}–${formatUSD(e.max_cost_usd)})`;
	return text;
}
