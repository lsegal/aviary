import type { AgentTask } from "../stores/settings";

export type ScheduleTrigger = "once" | "interval" | "daily" | "weekly" | "cron";

/**
 * When a task fires, as the trigger editor edits it. An empty trigger means
 * the task has no time trigger (a watch-only or manually run task).
 */
export interface TriggerFields {
	trigger: ScheduleTrigger | "";
	schedule: string;
	every: string;
	at: string;
	days: string;
	startAt: string;
	timezone: string;
	overlap: "queue" | "skip";
	missed: "skip" | "run_once";
}

/**
 * The task_schedule/task_update arguments for fields: only the inputs of the
 * selected trigger are set, so switching tabs clears the old trigger.
 */
export function triggerArgs(s: TriggerFields): Record<string, string> {
	const args: Record<string, string> = {
		trigger: s.trigger,
		schedule: "",
		every: "",
		at: "",
		days: "",
		start_at: "",
		timezone: s.timezone.trim(),
		overlap: s.overlap,
		missed: s.missed,
	};
	switch (s.trigger) {
		case "once":
			args.start_at = s.startAt.trim();
			break;
		case "interval":
			args.every = s.every.trim();
			break;
		case "daily":
			args.at = s.at.trim();
			break;
		case "weekly":
			args.at = s.at.trim();
			args.days = s.days.trim();
			break;
		case "cron":
			args.trigger = "";
			args.schedule = s.schedule.trim();
			break;
	}
	return args;
}

export const TRIGGERS: ScheduleTrigger[] = [
	"once",
	"interval",
	"daily",
	"weekly",
	"cron",
];

/** The trigger a configured task selects, inferred like the scheduler does. */
function taskTriggerKind(task: AgentTask): TriggerFields["trigger"] {
	const t = (task.trigger ?? "").trim().toLowerCase();
	if ((TRIGGERS as string[]).includes(t)) return t as ScheduleTrigger;
	if (task.schedule?.trim()) return "cron";
	if (task.every?.trim()) return "interval";
	if (task.days?.trim()) return "weekly";
	if (task.at?.trim()) return "daily";
	if (task.start_at?.trim()) return "once";
	return "";
}

/** The trigger editor's fields for a task from aviary.yaml. */
export function taskTriggerFields(task: AgentTask): TriggerFields {
	const trigger = taskTriggerKind(task);
	const timed = trigger === "daily" || trigger === "weekly";
	return {
		trigger,
		schedule: task.schedule ?? "",
		every: task.every ?? "",
		// The Daily and Weekly tabs start at 09:00, like a new Projects schedule.
		at: task.at || (timed ? "" : "09:00"),
		days: task.days ?? "",
		startAt: task.start_at ?? "",
		timezone: task.timezone ?? "",
		overlap: task.overlap === "skip" ? "skip" : "queue",
		missed: task.missed === "run_once" ? "run_once" : "skip",
	};
}

/**
 * Writes fields onto a task from aviary.yaml. A start_at on a recurring
 * trigger (its first allowed fire) is not shown by the editor, so it is kept
 * unless the task was a once trigger, whose start_at was the trigger itself.
 */
export function applyTaskTrigger(task: AgentTask, fields: TriggerFields) {
	const wasOnce = taskTriggerKind(task) === "once";
	const args = triggerArgs(fields);
	const set = (key: keyof AgentTask, v: string) => {
		if (v) (task as unknown as Record<string, unknown>)[key] = v;
		else delete task[key];
	};
	set("trigger", args.trigger);
	set("schedule", args.schedule);
	set("every", args.every);
	set("at", args.at);
	set("days", args.days);
	if (fields.trigger === "once" || wasOnce || !fields.trigger) {
		set("start_at", args.start_at);
	}
	set("timezone", fields.trigger ? args.timezone : "");
	set(
		"overlap",
		fields.trigger && args.overlap !== "queue" ? args.overlap : "",
	);
	set("missed", fields.trigger && args.missed !== "skip" ? args.missed : "");
}
