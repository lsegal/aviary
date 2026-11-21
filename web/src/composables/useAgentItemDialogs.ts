import { ref } from "vue";

/** The agent items the sidebar adds, edits and deletes through dialogs. */
export type AgentItemKind = "task" | "file";

export interface AgentItemDialog {
	kind: AgentItemKind;
	agent: string;
	mode: "create" | "edit" | "delete";
	/** The task or file being edited or deleted. */
	name?: string;
	/**
	 * Creates a project scheduled task: a "project" task that runs against
	 * this watched project, under agent. A project's Schedules section opens
	 * the dialog with it set and agent set to the project's owning agent.
	 */
	project?: string;
	/**
	 * Called with the new task's agent and name in place of opening it on
	 * the Scheduled Tasks page.
	 */
	onCreated?: (agent: string, name: string) => void;
}

// One dialog at a time, opened from the sidebar's "+" and "…" buttons, or a
// project's "New schedule", and shown by AgentItemDialogs.
const dialog = ref<AgentItemDialog | null>(null);

export function useAgentItemDialogs() {
	return {
		dialog,
		open(next: AgentItemDialog) {
			dialog.value = next;
		},
		close() {
			dialog.value = null;
		},
	};
}
