import { parseToolJSON } from "../lib/projects";
import { type TriggerFields, triggerArgs } from "../lib/trigger";
import { useMCP } from "./useMCP";

/** Returns a function giving the next 3 fires of a trigger as edited, saved or not. */
export function useTriggerPreview() {
	const { callTool } = useMCP();
	return async (s: TriggerFields): Promise<string[]> => {
		if (!s.trigger) return [];
		const args = triggerArgs(s);
		const result = parseToolJSON(
			await callTool(
				"task_preview",
				Object.fromEntries(Object.entries(args).filter(([, v]) => v)),
			),
		);
		const next = (result as { next?: unknown } | null)?.next;
		return Array.isArray(next) ? next.map(String) : [];
	};
}
