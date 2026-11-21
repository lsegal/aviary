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
	 * Opens a new task with Target = Project on this watched project, run by
	 * agent. A project board's Schedule button opens the dialog with it set.
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
