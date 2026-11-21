import { ref } from "vue";

/** The agent items the sidebar adds, edits and deletes through dialogs. */
export type AgentItemKind = "task" | "file";

export interface AgentItemDialog {
	kind: AgentItemKind;
	agent: string;
	mode: "create" | "edit" | "delete";
	/** The task or file being edited or deleted. */
	name?: string;
}

// One dialog at a time, opened from the sidebar's "+" and "…" buttons and
// shown by AgentItemDialogs.
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
