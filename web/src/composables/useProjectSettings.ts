import { onMounted, type Ref, ref, watch } from "vue";
import {
	type ClearResult,
	normalizeCheck,
	normalizeClear,
	normalizeClones,
	type ProjectCheckResult,
	type ProjectClone,
} from "../lib/projectSettings";
import {
	normalizeStatus,
	type ProjectMode,
	parseToolJSON,
} from "../lib/projects";
import {
	dropProject,
	insertProject,
	refreshProjectList,
} from "./projectListStore";
import { useMCP } from "./useMCP";

/**
 * Every job clone on record, and clearing the ones no job needs. Clones are
 * shared by all projects, so the list is not filtered by project.
 */
export function useProjectClones() {
	const { callTool } = useMCP();
	const clones = ref<ProjectClone[]>([]);
	const roots = ref<string[]>([]);
	const totalSize = ref(0);
	const unmeasured = ref(0);
	const loaded = ref(false);
	const error = ref("");
	const clearing = ref(false);
	const cleared = ref<ClearResult | null>(null);

	async function load() {
		try {
			const res = normalizeClones(
				parseToolJSON(await callTool("project_clone_list", {})),
			);
			clones.value = res.clones;
			roots.value = res.roots;
			totalSize.value = res.totalSize;
			unmeasured.value = res.unmeasured;
			error.value = "";
		} catch (e) {
			error.value = e instanceof Error ? e.message : String(e);
		} finally {
			loaded.value = true;
		}
	}

	async function clear() {
		clearing.value = true;
		try {
			cleared.value = normalizeClear(
				parseToolJSON(await callTool("project_clone_clear", {})),
			);
			await load();
		} catch (e) {
			error.value = e instanceof Error ? e.message : String(e);
		} finally {
			clearing.value = false;
		}
	}

	onMounted(() => void load());

	return {
		clones,
		roots,
		totalSize,
		unmeasured,
		loaded,
		error,
		clearing,
		cleared,
		load,
		clear,
	};
}

/**
 * This Aviary instance's Code Loop identity, the persisted ID it signs its
 * GitHub claims with. It does not change while the server runs, so it is read
 * once rather than polled.
 */
export function useProjectInstanceID() {
	const { callTool } = useMCP();
	const instanceID = ref("");
	onMounted(async () => {
		try {
			instanceID.value = normalizeStatus(
				parseToolJSON(await callTool("project_status", {})),
			).instanceID;
		} catch {
			// Projects is unavailable; the card stays hidden.
		}
	});
	return instanceID;
}

/** What project_check needs besides the target to know the project's CLIs. */
export interface ProjectCheckOptions {
	/** The agent that owns, or will own, the project. */
	agent?: string;
	/** The agent's model and fallbacks as the form holds them, unsaved. */
	models?: string[];
	/** The mode the form picks, watch or manual. */
	mode?: ProjectMode;
}

/**
 * project_check for a target, re-run (debounced) as the target or options
 * change, so editing the agent's models re-checks which CLIs need skills.
 */
export function useProjectCheck(
	target: Ref<string>,
	options: () => ProjectCheckOptions = () => ({}),
	delay = 400,
) {
	const { callTool } = useMCP();
	const result = ref<ProjectCheckResult | null>(null);
	const checking = ref(false);
	const error = ref("");
	let timer: ReturnType<typeof setTimeout> | undefined;
	let seq = 0;

	async function run() {
		const t = target.value.trim();
		const mine = ++seq;
		if (!t) {
			result.value = null;
			error.value = "";
			checking.value = false;
			return;
		}
		checking.value = true;
		try {
			const res = normalizeCheck(
				parseToolJSON(
					await callTool("project_check", { target: t, ...options() }),
				),
			);
			if (mine !== seq) return;
			result.value = res;
			error.value = "";
		} catch (e) {
			if (mine !== seq) return;
			result.value = null;
			error.value = e instanceof Error ? e.message : String(e);
		} finally {
			if (mine === seq) checking.value = false;
		}
	}

	watch([target, () => JSON.stringify(options())], () => {
		clearTimeout(timer);
		result.value = null;
		checking.value = target.value.trim() !== "";
		timer = setTimeout(() => void run(), delay);
	});

	return { result, checking, error, run };
}

/**
 * Adds target to agent's projects (project_add), saving it to that agent in
 * aviary.yaml; the running watch picks it up right away. Every view lists
 * the project, as checking GitHub, before the server answers, and drops it
 * again when the add fails.
 */
export function useAddProject() {
	const { callTool } = useMCP();
	return async (target: string, agent: string, mode: ProjectMode = "watch") => {
		const undo = insertProject(target, agent);
		try {
			// Watch is the default, so only manual is passed.
			await callTool("project_add", {
				target,
				agent,
				...(mode === "manual" ? { mode } : {}),
			});
		} catch (e) {
			undo();
			throw e;
		}
		void refreshProjectList();
	};
}

/** The job statuses that still hold work, which deleting a project stops. */
const IN_FLIGHT = ["active", "waiting", "queued", "paused"];

/**
 * Deleting a project through project_remove: inFlight lists the issue numbers
 * of its jobs the deletion stops, and remove deletes it, resolving to the
 * jobs it stopped. The repository and clones on disk are left alone.
 */
export function useRemoveProject(target: Ref<string>) {
	const { callTool } = useMCP();
	const inFlight = ref<number[]>([]);

	async function loadInFlight() {
		inFlight.value = [];
		const res = parseToolJSON(
			await callTool("project_job_list", { target: target.value, limit: 500 }),
		) as { jobs?: { number?: number; status?: string }[] } | null;
		inFlight.value = (res?.jobs ?? [])
			.filter((j) => IN_FLIGHT.includes(j.status ?? ""))
			.map((j) => Number(j.number));
	}

	async function remove(): Promise<number[]> {
		const removed = target.value;
		const res = parseToolJSON(
			await callTool("project_remove", { target: removed }),
		) as { stopped_jobs?: number[] } | null;
		dropProject(removed);
		void refreshProjectList();
		return res?.stopped_jobs ?? [];
	}

	return { inFlight, loadInFlight, remove };
}
