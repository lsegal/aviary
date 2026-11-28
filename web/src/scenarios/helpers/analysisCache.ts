// Mirrors web/e2e/helpers/analysisCache.ts for component tests (#1636).

type Args = Record<string, unknown>;
type Analysis = Record<string, unknown>;

/**
 * A stand-in for task_analyze's persistent server-side cache
 * (internal/mcp/task_analyze.go): analyses are kept by every keyed input and
 * shared by every page that uses the same instance, as the server's cache is
 * shared across reloads and browsers. analyses records each call that ran
 * the analyzer, which is what a cache hit must never do.
 */
export function analysisCache(analyze: (args: Args) => Analysis) {
	const entries = new Map<string, { result: Analysis; at: string }>();
	const analyses: Args[] = [];
	const key = (a: Args) =>
		JSON.stringify([
			a.agent,
			a.content,
			a.type,
			a.target ?? "",
			a.project ?? "",
			a.trigger ?? "schedule",
		]);
	return {
		analyses,
		/** Caches args' analysis as run at `at`, as an earlier session left it. */
		seed(args: Args, at: string) {
			entries.set(key(args), { result: analyze(args), at });
		},
		/** Drops every entry, as a compiler version bump does. */
		invalidate() {
			entries.clear();
		},
		handler(args: Args = {}): Analysis {
			const hit = args.force ? undefined : entries.get(key(args));
			if (hit) return { ...hit.result, cached: true, analyzed_at: hit.at };
			if (args.cached_only) return { miss: true };
			analyses.push(args);
			const at = new Date().toISOString();
			const result = analyze(args);
			entries.set(key(args), { result, at });
			return { ...result, analyzed_at: at };
		},
	};
}

let observer: MutationObserver | null = null;
let sawCheckingState = false;

/**
 * Records whether the cost feedback matched by selector ever showed
 * Checking…, from now on. Call it before rendering the page. Unlike the
 * spec's init script, the record survives reload(): it covers every page
 * load since the call.
 */
export function watchForChecking(
	selector = "[data-testid=task-cost-feedback]",
) {
	observer?.disconnect();
	sawCheckingState = false;
	observer = new MutationObserver(() => {
		for (const el of document.querySelectorAll(selector))
			if (el.getAttribute("data-state") === "checking") sawCheckingState = true;
	});
	observer.observe(document, {
		subtree: true,
		childList: true,
		attributes: true,
	});
}

export function sawChecking(): boolean {
	return sawCheckingState;
}
