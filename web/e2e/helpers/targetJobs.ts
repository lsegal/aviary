/**
 * project_status's target_jobs for a mock's jobs: each project's entries in
 * the Running state and its waiting, paused or queued ones, as the Manager
 * counts them for the project list (#1657).
 */
export function targetJobs(
	jobs: { target: string; state?: string; status?: string }[],
): Record<string, { running: number; waiting: number }> {
	const counts: Record<string, { running: number; waiting: number }> = {};
	for (const j of jobs) {
		counts[j.target] ??= { running: 0, waiting: 0 };
		if (j.state === "running") counts[j.target].running++;
		if (["waiting", "paused", "queued"].includes(j.status ?? ""))
			counts[j.target].waiting++;
	}
	return counts;
}
