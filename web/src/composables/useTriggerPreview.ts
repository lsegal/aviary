import { parseToolJSON } from "../lib/projects";
import { type TriggerFields, triggerArgs } from "../lib/trigger";
import { useMCP } from "./useMCP";

/** Returns a function giving the next 3 fires of a trigger as edited, saved or not. */
export function useTriggerPreview() {
	const preview = useTaskPreview();
	return async (s: TriggerFields): Promise<string[]> => {
		if (!s.trigger) return [];
		const next = (await preview(s))?.next;
		return Array.isArray(next) ? next.map(String) : [];
	};
}

/**
 * Returns a function giving how often a recurring trigger fires in 30 days,
 * or 0 for no trigger, a one-time trigger, or one task_preview rejects.
 */
export function useTriggerRuns() {
	const preview = useTaskPreview();
	return async (s: TriggerFields): Promise<number> => {
		if (!s.trigger || s.trigger === "once") return 0;
		try {
			const runs = (await preview(s))?.runs_per_30_days;
			return typeof runs === "number" && runs > 0 ? runs : 0;
		} catch {
			return 0;
		}
	};
}

interface TaskPreview {
	next?: unknown;
	runs_per_30_days?: unknown;
}

function useTaskPreview() {
	const { callTool } = useMCP();
	return async (s: TriggerFields): Promise<TaskPreview | null> => {
		const args = triggerArgs(s);
		return parseToolJSON(
			await callTool(
				"task_preview",
				Object.fromEntries(Object.entries(args).filter(([, v]) => v)),
			),
		) as TaskPreview | null;
	};
}
