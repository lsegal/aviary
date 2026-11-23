// Task and workflow delivery targets, mirroring config.ParseTaskTarget: a
// "codeloop:<project>#<number>" target hands the output to a Code Loop job.

export const CODELOOP_TARGET_PREFIX = "codeloop:";

/** The Code Loop job a target names. */
export interface CodeLoopTarget {
	project: string;
	number: number;
}

/** The target that picks project's job number. */
export function codeLoopTarget(project: string, number: number): string {
	return `${CODELOOP_TARGET_PREFIX}${project.trim()}#${number}`;
}

/** Whether target is a Code Loop target, well-formed or not. */
export function isCodeLoopTarget(target?: string): boolean {
	return (target ?? "").trim().startsWith(CODELOOP_TARGET_PREFIX);
}

/** The job a "codeloop:<project>#<number>" target picks, or null. */
export function parseCodeLoopTarget(target?: string): CodeLoopTarget | null {
	const t = (target ?? "").trim();
	if (!t.startsWith(CODELOOP_TARGET_PREFIX)) return null;
	const ref = t.slice(CODELOOP_TARGET_PREFIX.length);
	const hash = ref.lastIndexOf("#");
	if (hash < 0) return null;
	const project = ref.slice(0, hash).trim();
	const number = Number(ref.slice(hash + 1).trim());
	if (!project || !Number.isInteger(number) || number <= 0) return null;
	return { project, number };
}

/** How a Code Loop target is named in prose, such as "Code Loop owner/repo#12". */
export function codeLoopTargetLabel(t: CodeLoopTarget): string {
	return `Code Loop ${t.project}#${t.number}`;
}
