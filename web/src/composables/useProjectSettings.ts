import { onMounted, type Ref, ref, watch } from "vue";
import {
	type CodingCLI,
	normalizeCheck,
	normalizeCLIList,
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

/** A project's clones with their sizes, and clearing unused ones. */
export function useProjectClones(project: Ref<string>) {
	const { callTool } = useMCP();
	const clones = ref<ProjectClone[]>([]);
	const totalBytes = ref(0);
	const loaded = ref(false);
	const error = ref("");
	const clearing = ref(false);
	const message = ref("");

	async function load() {
		try {
			const res = normalizeClones(
				parseToolJSON(
					await callTool("project_clone_list", { project: project.value }),
				),
			);
			clones.value = res.clones;
			totalBytes.value = res.totalBytes;
			error.value = "";
		} catch (e) {
			error.value = e instanceof Error ? e.message : String(e);
		} finally {
			loaded.value = true;
		}
	}

	async function clear(path?: string) {
		clearing.value = true;
		try {
			const args: Record<string, unknown> = { project: project.value };
			if (path) args.path = path;
			const raw = parseToolJSON(await callTool("project_clone_clear", args));
			const reclaimed =
				raw && typeof raw === "object"
					? Number((raw as Record<string, unknown>).reclaimed_bytes) || 0
					: 0;
			message.value = reclaimed ? `reclaimed:${reclaimed}` : "cleared";
			await load();
		} catch (e) {
			error.value = e instanceof Error ? e.message : String(e);
		} finally {
			clearing.value = false;
		}
	}

	onMounted(() => void load());
	watch(project, () => void load());

	return { clones, totalBytes, loaded, error, clearing, message, load, clear };
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

/** Starts watching target with its current settings (project_add). */
export function useAddProject() {
	const { callTool } = useMCP();
	return async (target: string) => {
		await callTool("project_add", { target });
	};
}
