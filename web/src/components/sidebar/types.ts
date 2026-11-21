/** One entry of a sidebar item's "…" menu; danger entries delete or clear. */
export interface SidebarItemAction {
	key: string;
	label: string;
	danger?: boolean;
	disabled?: boolean;
	title?: string;
}

/** Edit and Delete, the "…" menu of chats, tasks and files. */
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
