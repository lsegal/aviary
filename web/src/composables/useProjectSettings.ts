import { onMounted, type Ref, ref, watch } from "vue";
import {
	type ClearResult,
	normalizeCheck,
	normalizeClear,
	normalizeClones,
	type ProjectCheckResult,
	type ProjectClone,
} from "../lib/projectSettings";
import { parseToolJSON } from "../lib/projects";
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

/** project_check for a target, re-run (debounced) as the target changes. */
export function useProjectCheck(target: Ref<string>, delay = 400) {
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
				parseToolJSON(await callTool("project_check", { target: t })),
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

	watch(target, () => {
		clearTimeout(timer);
		result.value = null;
		checking.value = target.value.trim() !== "";
		timer = setTimeout(() => void run(), delay);
	});

	return { result, checking, error, run };
}

/**
 * Adds target to agent's projects (project_add), saving it to that agent in
 * aviary.yaml; the running watch picks it up right away.
 */
export function useAddProject() {
	const { callTool } = useMCP();
	return async (target: string, agent: string) => {
		await callTool("project_add", { target, agent });
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
		const res = parseToolJSON(
			await callTool("project_remove", { target: target.value }),
		) as { stopped_jobs?: number[] } | null;
		return res?.stopped_jobs ?? [];
	}

	return { inFlight, loadInFlight, remove };
}
