import { computed, onBeforeUnmount, type Ref, ref, watch } from "vue";
import { compiledStatus, type TaskCompileAttempt } from "../lib/compiledStatus";
import { parseToolJSON } from "../lib/projects";
import type { Job } from "../stores/jobs";
import { type AgentTask, useSettingsStore } from "../stores/settings";
import { useMCP } from "./useMCP";

const PANEL_KEY = "aviary.compiledPanelOpen";
/** Whether the compiled script panel is open, shared by every editor and kept across reloads. */
const panelOpen = ref(localStorage.getItem(PANEL_KEY) === "true");
watch(panelOpen, (open) => localStorage.setItem(PANEL_KEY, String(open)));

export function useCompiledPanelOpen() {
	return panelOpen;
}

/**
 * A precompiling task's compile attempts and the newest run whose compiled
 * script failed, with the compiled script panel's actions: Recompile now,
 * which reloads the config until the new compile lands, and Disable precompile.
 */
export function useTaskCompiles(
	agent: Ref<string>,
	task: Ref<AgentTask | undefined>,
) {
	const { callTool } = useMCP();
	const settings = useSettingsStore();
	const attempts = ref<TaskCompileAttempt[]>([]);
	const lastFailedRun = ref<Job | null>(null);
	const busy = ref(false);
	const error = ref("");
	/** Whether a recompile is in flight, so older attempts do not describe the task. */
	const recompiling = ref(false);
	/** The attempts that describe the task's current script. */
	const current = computed(() => (recompiling.value ? [] : attempts.value));

	const name = computed(() => task.value?.name ?? "");

	async function refresh() {
		const a = agent.value;
		const n = name.value;
		if (!a || !n || !task.value) {
			attempts.value = [];
			lastFailedRun.value = null;
			return;
		}
		try {
			// One at a time: the live server's MCP transport prefers sequential calls.
			const list = (parseToolJSON(
				await callTool("task_compile_query", { agent: a, task: n }),
			) ?? []) as TaskCompileAttempt[];
			const jobs = (parseToolJSON(
				await callTool("job_list", { task: `${a}/${n}` }),
			) ?? []) as Job[];
			if (a !== agent.value || n !== name.value) return;
			attempts.value = list.sort((x, y) =>
				y.created_at.localeCompare(x.created_at),
			);
			lastFailedRun.value =
				jobs
					.filter((j) => j.script_error)
					.sort((x, y) => y.created_at.localeCompare(x.created_at))[0] ?? null;
		} catch {
			attempts.value = [];
			lastFailedRun.value = null;
		}
	}

	watch(
		() => [
			agent.value,
			name.value,
			task.value?.compiled_key,
			task.value?.compiled,
		],
		() => void refresh(),
		{ immediate: true },
	);

	let poll: ReturnType<typeof setTimeout> | undefined;
	onBeforeUnmount(() => clearTimeout(poll));

	async function act(fn: () => Promise<void>) {
		busy.value = true;
		error.value = "";
		try {
			await fn();
		} catch (e) {
			error.value = e instanceof Error ? e.message : String(e);
			recompiling.value = false;
		} finally {
			busy.value = false;
		}
	}

	/** Discards the script and compiles a fresh one, following it until it lands. */
	const recompile = () =>
		act(async () => {
			clearTimeout(poll);
			recompiling.value = true;
			await callTool("config_task_convert_to_script", {
				agent: agent.value,
				task: name.value,
			});
			await settings.fetchConfig();
			await refresh();
			let polls = 0;
			const follow = async () => {
				const t = task.value;
				if (!t || compiledStatus(t)?.state !== "compiling" || ++polls > 60) {
					recompiling.value = false;
					return;
				}
				poll = setTimeout(async () => {
					await settings.fetchConfig();
					await refresh();
					await follow();
				}, 2000);
			};
			await follow();
		});

	/** Turns precompile off, so runs use the model. */
	const disablePrecompile = () =>
		act(async () => {
			await callTool("task_update", {
				name: `${agent.value}/${name.value}`,
				precompile: false,
			});
			await settings.fetchConfig();
		});

	return {
		attempts: current,
		recompiling,
		lastFailedRun,
		busy,
		error,
		refresh,
		recompile,
		disablePrecompile,
	};
}
