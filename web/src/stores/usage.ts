import { defineStore } from "pinia";
import { computed, ref } from "vue";
import { useMCP } from "../composables/useMCP";

export interface UsageRecord {
	timestamp: string;
	session_id: string;
	agent_id: string;
	model: string;
	provider: string;
	input_tokens: number;
	output_tokens: number;
	cache_read_tokens?: number;
	cache_write_tokens?: number;
	tool_calls?: number;
	has_error?: boolean;
	has_throttle?: boolean;
	cost_usd?: number;
	project?: ProjectUsage;
	/** The "<agent>/<task>" scheduled task or workflow whose run spent the tokens. */
	task?: string;
	/** The channel whose message fired a workflow run. */
	channel?: string;
}

// ProjectUsage is the Projects work a usage record reports instead of token
// use: a job run's wall time, or an issue's merged or failed outcome. A
// merged outcome also carries the merged pull request's diff size.
export interface ProjectUsage {
	issue: string;
	outcome?: "merged" | "failed";
	duration_ms?: number;
	additions?: number;
	deletions?: number;
}

export interface ProjectSummary {
	issues: number;
	merged: number;
	failed: number;
	wallMs: number;
	additions: number;
	deletions: number;
}

function fmtDate(daysAgo: number): string {
	const d = new Date();
	d.setDate(d.getDate() - daysAgo);
	return d.toISOString().slice(0, 10);
}

function sumCost(records: UsageRecord[]): number | null {
	let total: number | null = null;
	for (const r of records) {
		if (r.cost_usd) total = (total ?? 0) + r.cost_usd;
	}
	return total;
}

export const useUsageStore = defineStore("usage", () => {
	const { callTool } = useMCP();

	// Token records drive every token and cost metric; project records only
	// the Projects summary, so they never count as messages.
	const records = ref<UsageRecord[]>([]);
	const projectRecords = ref<UsageRecord[]>([]);
	const loading = ref(false);
	const error = ref<string | null>(null);

	// Date range (YYYY-MM-DD)
	const startDate = ref<string>(fmtDate(7));
	const endDate = ref<string>(fmtDate(0));

	async function fetch() {
		loading.value = true;
		error.value = null;
		try {
			const raw = await callTool("usage_query", {
				start: startDate.value,
				end: endDate.value,
			});
			const all = (JSON.parse(raw) as UsageRecord[]) ?? [];
			records.value = all.filter((r) => !r.project);
			projectRecords.value = all.filter((r) => r.project);
		} catch (e) {
			error.value = e instanceof Error ? e.message : String(e);
		} finally {
			loading.value = false;
		}
	}

	function setPreset(days: number) {
		endDate.value = fmtDate(0);
		startDate.value = fmtDate(days);
		fetch();
	}

	// ── Totals ────────────────────────────────────────────────────────────────

	const totalInput = computed(() =>
		records.value.reduce((s, r) => s + r.input_tokens, 0),
	);
	const totalOutput = computed(() =>
		records.value.reduce((s, r) => s + r.output_tokens, 0),
	);
	const totalCacheRead = computed(() =>
		records.value.reduce((s, r) => s + (r.cache_read_tokens ?? 0), 0),
	);
	const totalCacheWrite = computed(() =>
		records.value.reduce((s, r) => s + (r.cache_write_tokens ?? 0), 0),
	);
	// Only CLI backends report a cost, so the total is null when no record in
	// range carries one rather than a misleading $0.00.
	const totalCost = computed(() => sumCost(records.value));
	const totalTokens = computed(() => totalInput.value + totalOutput.value);
	const totalMessages = computed(() => records.value.length);
	const totalToolCalls = computed(() =>
		records.value.reduce((s, r) => s + (r.tool_calls ?? 0), 0),
	);
	const totalErrors = computed(
		() => records.value.filter((r) => r.has_error).length,
	);
	const totalThrottles = computed(
		() => records.value.filter((r) => r.has_throttle).length,
	);
	const sessionCount = computed(
		() => new Set(records.value.map((r) => r.session_id)).size,
	);
	const avgTokensPerMsg = computed(() =>
		totalMessages.value
			? Math.round(totalTokens.value / totalMessages.value)
			: 0,
	);
	const errorRate = computed(() =>
		totalMessages.value ? (totalErrors.value / totalMessages.value) * 100 : 0,
	);
	const throttleRate = computed(() =>
		totalMessages.value
			? (totalThrottles.value / totalMessages.value) * 100
			: 0,
	);
	const cacheHitRate = computed(() => {
		const total = totalInput.value + totalCacheRead.value;
		return total > 0 ? (totalCacheRead.value / total) * 100 : 0;
	});

	// ── Projects ──────────────────────────────────────────────────────────────

	// An issue counts as merged when any of its pull requests merged in range,
	// and as failed when it did not and its latest outcome in range failed.
	// Lines added and removed total the pull requests that merged in range.
	function summarizeProjects(list: UsageRecord[]): ProjectSummary {
		const issues = new Set<string>();
		const merged = new Set<string>();
		const latest = new Map<string, { at: string; outcome: string }>();
		let wallMs = 0;
		let additions = 0;
		let deletions = 0;
		for (const r of list) {
			const p = r.project;
			if (!p) continue;
			issues.add(p.issue);
			wallMs += p.duration_ms ?? 0;
			additions += p.additions ?? 0;
			deletions += p.deletions ?? 0;
			if (!p.outcome) continue;
			if (p.outcome === "merged") merged.add(p.issue);
			const prev = latest.get(p.issue);
			if (!prev || r.timestamp >= prev.at)
				latest.set(p.issue, { at: r.timestamp, outcome: p.outcome });
		}
		let failed = 0;
		for (const [issue, last] of latest) {
			if (last.outcome === "failed" && !merged.has(issue)) failed++;
		}
		return {
			issues: issues.size,
			merged: merged.size,
			failed,
			wallMs,
			additions,
			deletions,
		};
	}

	const projectSummary = computed(() =>
		summarizeProjects(projectRecords.value),
	);

	// Each project is the repository of the issues its jobs worked, the
	// "owner/repo" before the issue's "#number".
	const projectBreakdown = computed(() => {
		const byProject = new Map<string, UsageRecord[]>();
		for (const r of projectRecords.value) {
			if (!r.project) continue;
			const name = r.project.issue.replace(/#\d+$/, "");
			const list = byProject.get(name) ?? [];
			list.push(r);
			byProject.set(name, list);
		}
		return [...byProject.entries()]
			.map(([name, list]) => ({ name, ...summarizeProjects(list) }))
			.sort((a, b) => a.name.localeCompare(b.name));
	});

	// ── Breakdowns ────────────────────────────────────────────────────────────

	function tally(key: keyof UsageRecord) {
		const m = new Map<string, { tokens: number; cost: number | null }>();
		for (const r of records.value) {
			const k = String(r[key] ?? "");
			const v = m.get(k) ?? { tokens: 0, cost: null };
			v.tokens += r.input_tokens + r.output_tokens;
			if (r.cost_usd) v.cost = (v.cost ?? 0) + r.cost_usd;
			m.set(k, v);
		}
		return [...m.entries()]
			.sort((a, b) => b[1].tokens - a[1].tokens)
			.slice(0, 5)
			.map(([name, v]) => ({ name, ...v }));
	}

	const topModels = computed(() => tally("model"));
	const topProviders = computed(() => tally("provider"));
	const topAgents = computed(() => tally("agent_id"));

	// ── Time activity ─────────────────────────────────────────────────────────

	const byDayOfWeek = computed(() => {
		const days = Array(7).fill(0) as number[];
		for (const r of records.value) {
			days[new Date(r.timestamp).getDay()] += r.input_tokens + r.output_tokens;
		}
		return days;
	});

	const byHour = computed(() => {
		const hours = Array(24).fill(0) as number[];
		for (const r of records.value) {
			hours[new Date(r.timestamp).getHours()] +=
				r.input_tokens + r.output_tokens;
		}
		return hours;
	});

	// ── Daily chart data ──────────────────────────────────────────────────────

	const byDay = computed(() => {
		const m = new Map<
			string,
			{ input: number; output: number; cache: number }
		>();
		for (const r of records.value) {
			const d = r.timestamp.slice(0, 10);
			const v = m.get(d) ?? { input: 0, output: 0, cache: 0 };
			v.input += r.input_tokens;
			v.output += r.output_tokens;
			v.cache += r.cache_read_tokens ?? 0;
			m.set(d, v);
		}
		// Fill gaps in the range.
		const result: Array<{
			date: string;
			input: number;
			output: number;
			cache: number;
		}> = [];
		const cur = new Date(startDate.value);
		const endD = new Date(endDate.value);
		while (cur <= endD) {
			const key = cur.toISOString().slice(0, 10);
			result.push({
				date: key,
				...(m.get(key) ?? { input: 0, output: 0, cache: 0 }),
			});
			cur.setDate(cur.getDate() + 1);
		}
		return result;
	});

	// ── Sessions list ─────────────────────────────────────────────────────────

	const sessionList = computed(() => {
		type S = {
			session_id: string;
			agent_id: string;
			model: string;
			provider: string;
			input: number;
			output: number;
			tool_calls: number;
			has_error: boolean;
			has_throttle: boolean;
			first_ts: string;
			last_ts: string;
			task?: string;
			channel?: string;
		};
		const m = new Map<string, S>();
		for (const r of records.value) {
			const s = m.get(r.session_id);
			if (!s) {
				m.set(r.session_id, {
					session_id: r.session_id,
					agent_id: r.agent_id,
					model: r.model,
					provider: r.provider,
					input: r.input_tokens,
					output: r.output_tokens,
					tool_calls: r.tool_calls ?? 0,
					has_error: r.has_error ?? false,
					has_throttle: r.has_throttle ?? false,
					first_ts: r.timestamp,
					last_ts: r.timestamp,
					task: r.task,
					channel: r.channel,
				});
			} else {
				s.input += r.input_tokens;
				s.output += r.output_tokens;
				s.tool_calls += r.tool_calls ?? 0;
				s.has_error = s.has_error || (r.has_error ?? false);
				s.has_throttle = s.has_throttle || (r.has_throttle ?? false);
				if (r.timestamp < s.first_ts) s.first_ts = r.timestamp;
				if (r.timestamp > s.last_ts) s.last_ts = r.timestamp;
			}
		}
		return [...m.values()].sort((a, b) => b.last_ts.localeCompare(a.last_ts));
	});

	return {
		records,
		projectRecords,
		projectSummary,
		projectBreakdown,
		loading,
		error,
		startDate,
		endDate,
		fetch,
		setPreset,
		fmtDate,
		totalInput,
		totalOutput,
		totalCacheRead,
		totalCacheWrite,
		totalTokens,
		totalCost,
		totalMessages,
		totalToolCalls,
		totalErrors,
		totalThrottles,
		sessionCount,
		avgTokensPerMsg,
		errorRate,
		throttleRate,
		cacheHitRate,
		topModels,
		topProviders,
		topAgents,
		byDayOfWeek,
		byHour,
		byDay,
		sessionList,
	};
});
