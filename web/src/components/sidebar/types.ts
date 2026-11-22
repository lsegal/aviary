import type { Component } from "vue";

/** One entry of a sidebar item's "…" menu; danger entries delete or clear. */
export interface SidebarItemAction {
	key: string;
	label: string;
	/** Overrides the icon picked from the action's key and danger flag. */
	icon?: Component;
	danger?: boolean;
	disabled?: boolean;
	title?: string;
}

/** Edit and Delete, the "…" menu of files. */
export function editDeleteActions(
	editLabel: string,
	options: { locked?: boolean; lockedTitle?: string } = {},
): SidebarItemAction[] {
	const { locked = false, lockedTitle } = options;
	return [
		{
			key: "edit",
			label: editLabel,
			disabled: locked,
			title: locked ? lockedTitle : undefined,
		},
		{
			key: "delete",
			label: "Delete",
			danger: true,
			disabled: locked,
			title: locked ? lockedTitle : undefined,
		},
	];
}
