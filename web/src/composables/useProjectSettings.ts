import { onMounted, type Ref, ref, watch } from "vue";
import {
	type ClearResult,
	type CodingCLI,
	normalizeCheck,
	normalizeCLIList,
	normalizeClear,
	normalizeClones,
	type ProjectCheckResult,
	type ProjectClone,
} from "../lib/projectSettings";
import { parseToolJSON } from "../lib/projects";
import { useMCP } from "./useMCP";

const CLI_CACHE_KEY = "aviary.projects.cli-probe";

// The CLI probe is slow (it runs every coding CLI's doctor), so its last
// result is shared across components and kept in localStorage. Settings render
// from the cache at once and the probe refreshes it in the background.
const cliCache = ref<CodingCLI[] | null>(readCLICache());
const cliRefreshing = ref(false);
const cliError = ref("");
let cliInflight: Promise<void> | null = null;

function readCLICache(): CodingCLI[] | null {
	try {
		const raw = localStorage.getItem(CLI_CACHE_KEY);
		return raw ? normalizeCLIList(JSON.parse(raw)) : null;
	} catch {
		return null;
	}
}

/** The coding-CLI probe: cached result at once, refreshed in the background. */
export function useCLIProbe() {
	const { callTool } = useMCP();

	function refresh(): Promise<void> {
		if (cliInflight) return cliInflight;
		cliRefreshing.value = true;
		cliInflight = (async () => {
			try {
				const clis = normalizeCLIList(
					parseToolJSON(await callTool("project_cli_list", {})),
				);
				cliCache.value = clis;
				cliError.value = "";
				try {
					localStorage.setItem(CLI_CACHE_KEY, JSON.stringify(clis));
				} catch {
					// Storage full or disabled; the in-memory cache still works.
				}
			} catch (e) {
				cliError.value = e instanceof Error ? e.message : String(e);
			} finally {
				cliRefreshing.value = false;
				cliInflight = null;
			}
		})();
		return cliInflight;
	}

	onMounted(() => void refresh());

	return {
		clis: cliCache,
		refreshing: cliRefreshing,
		error: cliError,
		refresh,
	};
}

/**
 * Every job clone on record, and clearing the ones no job needs. Clones are
 * shared by all projects, so the list is not filtered by project.
 */
export function useProjectClones() {
	const { callTool } = useMCP();
	const clones = ref<ProjectClone[]>([]);
	const roots = ref<string[]>([]);
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

	return { clones, roots, loaded, error, clearing, cleared, load, clear };
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
 * Starts watching target with the current settings as a runtime watch
 * (project_add with runtime), so nothing is written to aviary.yaml.
 */
export function useAddProject() {
	const { callTool } = useMCP();
	return async (target: string) => {
		await callTool("project_add", { target, runtime: true });
	};
}
