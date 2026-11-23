// Task and workflow delivery targets, mirroring config.ParseTaskTarget: a
// "codeloop:<project>#<number>" target hands the output to a Code Loop job,
// and an optional "?resume,reopen" suffix resumes a paused job or reopens a
// completed one when output arrives.

export const CODELOOP_TARGET_PREFIX = "codeloop:";

/** A Code Loop target's delivery options. */
export interface CodeLoopTargetOptions {
	/** Resume a paused job when output arrives. */
	resume?: boolean;
	/** Retry a completed job with the output instead of rejecting it. */
	reopen?: boolean;
}

/** The Code Loop job a target names, and its delivery options. */
export interface CodeLoopTarget extends CodeLoopTargetOptions {
	project: string;
	number: number;
}

/** The target that picks project's job number with options. */
export function codeLoopTarget(
	project: string,
	number: number,
	options: CodeLoopTargetOptions = {},
): string {
	const flags = [
		options.resume ? "resume" : "",
		options.reopen ? "reopen" : "",
	].filter(Boolean);
	const suffix = flags.length ? `?${flags.join(",")}` : "";
	return `${CODELOOP_TARGET_PREFIX}${project.trim()}#${number}${suffix}`;
}

/** Whether target is a Code Loop target, well-formed or not. */
export function isCodeLoopTarget(target?: string): boolean {
	return (target ?? "").trim().startsWith(CODELOOP_TARGET_PREFIX);
}

/** The job a "codeloop:<project>#<number>[?options]" target picks, or null. */
export function parseCodeLoopTarget(target?: string): CodeLoopTarget | null {
	const t = (target ?? "").trim();
	if (!t.startsWith(CODELOOP_TARGET_PREFIX)) return null;
	const ref = t.slice(CODELOOP_TARGET_PREFIX.length);
	const hash = ref.lastIndexOf("#");
	if (hash < 0) return null;
	const project = ref.slice(0, hash).trim();
	const [rawNumber, ...rest] = ref.slice(hash + 1).split("?");
	const number = Number(rawNumber.trim());
	if (!project || !Number.isInteger(number) || number <= 0) return null;
	const picked: CodeLoopTarget = { project, number };
	if (rest.length === 0) return picked;
	for (const flag of rest.join("?").split(",")) {
		const f = flag.trim().toLowerCase();
		if (f === "resume") picked.resume = true;
		else if (f === "reopen") picked.reopen = true;
		else return null;
	}
	return picked;
}

/** How a Code Loop target is named in prose, such as "Code Loop owner/repo#12". */
export function codeLoopTargetLabel(t: CodeLoopTarget): string {
	return `Code Loop ${t.project}#${t.number}`;
}
