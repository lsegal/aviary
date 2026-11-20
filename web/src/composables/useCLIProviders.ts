import { onMounted, ref } from "vue";
import { type CodingCLI, normalizeCLIList } from "../lib/cliProviders";
import { parseToolJSON } from "../lib/projects";
import { useMCP } from "./useMCP";

const CLI_CACHE_KEY = "aviary.providers.clis";

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

/**
 * The coding-CLI providers from core Aviary's provider_cli_list: cached
 * result at once, refreshed in the background.
 */
export function useCLIProviders() {
	const { callTool } = useMCP();

	function refresh(): Promise<void> {
		if (cliInflight) return cliInflight;
		cliRefreshing.value = true;
		cliInflight = (async () => {
			try {
				const clis = normalizeCLIList(
					parseToolJSON(await callTool("provider_cli_list", {})),
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
