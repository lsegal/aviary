// What deleting an object's data removes, as delete_preview counts it.

export interface DataCounts {
	sessions: number;
	messages: number;
	runs: number;
	files: number;
}

/** The kinds of object whose delete offers to delete their data. */
export type DataOwnerKind = "task" | "channel" | "agent";

/** A task or agent whose data config_save deletes with it. */
export interface DataOwner {
	kind: "task" | "agent";
	agent?: string;
	name: string;
}

function count(n: number, one: string, many: string): string {
	return `${n} ${n === 1 ? one : many}`;
}

/**
 * The counts as one phrase, e.g. "1 session, 34 messages, 12 runs, 3 files",
 * leaving out what is zero; "" when there is nothing.
 */
export function formatDataCounts(c: DataCounts): string {
	const parts: string[] = [];
	if (c.sessions) parts.push(count(c.sessions, "session", "sessions"));
	if (c.messages) parts.push(count(c.messages, "message", "messages"));
	if (c.runs) parts.push(count(c.runs, "run", "runs"));
	if (c.files) parts.push(count(c.files, "file", "files"));
	return parts.join(", ");
}
