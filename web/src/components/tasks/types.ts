import type { Job } from "../../stores/jobs";
import type { AgentTask } from "../../stores/settings";

/** A scheduled task as the Scheduled Tasks page shows it. */
export interface TaskRow {
	/** The scheduler's id: <agent>/<name>. */
	id: string;
	task: AgentTask;
	enabled: boolean;
	/** "agent <name>", or "project <owner/repo>" for a project task. */
	owner: string;
	/** The schedule in words, with its next fire or "paused". */
	schedule: string;
	running: boolean;
	/** The latest run that started, newest first in the range. */
	last?: Job;
}

/** A task card's or row's quick actions. */
export type TaskAction = "run" | "toggle" | "edit" | "runs";
