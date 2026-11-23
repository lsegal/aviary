import { reactive } from "vue";
import { useSettingsStore } from "../stores/settings";
import { useMCP } from "./useMCP";
import { useToasts } from "./useToasts";

// The tasks whose Pause or Resume is in flight, by "<agent>/<task>".
const pending = reactive(new Set<string>());

/**
 * Pauses and resumes scheduled tasks from anywhere: the sidebar's "…" menu,
 * the Scheduled Tasks page and a task's own page. The settings store flips
 * first, so the sidebar, the page and the task editor all show the new state
 * at once; a failed call flips it back and says why in a toast.
 */
export function useTaskPause() {
	const settings = useSettingsStore();
	const { callTool } = useMCP();
	const { show } = useToasts();

	function isPending(agent: string, name: string): boolean {
		return pending.has(`${agent}/${name}`);
	}

	/** Sets a task's enabled state; resolves false when it failed. */
	async function setTaskEnabled(
		agent: string,
		name: string,
		enabled: boolean,
	): Promise<boolean> {
		const id = `${agent}/${name}`;
		if (pending.has(id)) return false;
		pending.add(id);
		const task = settings.config?.agents
			.find((a) => a.name === agent)
			?.tasks.find((t) => t.name === name);
		const before = task?.enabled;
		if (task) task.enabled = enabled;
		try {
			await callTool("task_update", { name: id, enabled });
			await settings.fetchConfig();
			return true;
		} catch (e) {
			if (task) {
				if (before === undefined) delete task.enabled;
				else task.enabled = before;
			}
			const reason = e instanceof Error ? e.message : String(e);
			show(
				`Couldn't ${enabled ? "resume" : "pause"} ${name}: ${reason}`,
				"error",
			);
			return false;
		} finally {
			pending.delete(id);
		}
	}

	return { setTaskEnabled, isPending };
}
