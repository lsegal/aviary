// Customized navigation (#1003): the user's order and hidden items for each
// list of navigation items. The sidebar's agent and System pages, the
// collapsed rail and the top row all arrange their items through
// arrangeNav, so they share one reorder and visibility rule.

/** The navigation lists the user can arrange. */
export type NavList = "agentPages" | "systemPages" | "top";

export interface NavPrefs {
	/** Per list, the item keys in the user's order. */
	order: Partial<Record<NavList, string[]>>;
	/** Per list, the item keys the user hid. */
	hidden: Partial<Record<NavList, string[]>>;
}

export function emptyNavPrefs(): NavPrefs {
	return { order: {}, hidden: {} };
}

/** Whether prefs differ from the built-in order and visibility. */
export function isCustomized(prefs: NavPrefs): boolean {
	return [...Object.values(prefs.order), ...Object.values(prefs.hidden)].some(
		(keys) => (keys?.length ?? 0) > 0,
	);
}

/**
 * The keys of defaults in the saved order. A key the saved order lacks, such
 * as a page added by a later version, takes the place after the key that
 * precedes it by default, or the front when nothing does, instead of being
 * dropped. Saved keys no longer defined are left out.
 */
export function mergeOrder(defaults: string[], saved?: string[]): string[] {
	if (!saved?.length) return [...defaults];
	const known = new Set(defaults);
	const result = saved.filter(
		(k, i) => known.has(k) && saved.indexOf(k) === i,
	);
	defaults.forEach((key, i) => {
		if (result.includes(key)) return;
		let at = 0;
		for (let j = i - 1; j >= 0; j--) {
			const prev = result.indexOf(defaults[j]);
			if (prev >= 0) {
				at = prev + 1;
				break;
			}
		}
		result.splice(at, 0, key);
	});
	return result;
}

export interface ArrangeOptions {
	/** Keys that never show, whatever the preferences say, such as the
	 * pages a minimal `aviary watch` session leaves out (#991). */
	forcedHidden?: ReadonlySet<string>;
	/** Shows the items the user hid, so they stay reachable. */
	showHidden?: boolean;
}

/** items, in the list's saved order, without the hidden ones. */
export function arrangeNav<T extends { key: string }>(
	items: T[],
	list: NavList,
	prefs: NavPrefs,
	opts: ArrangeOptions = {},
): T[] {
	const byKey = new Map(items.map((item) => [item.key, item]));
	const hidden = new Set(opts.showHidden ? [] : (prefs.hidden[list] ?? []));
	return mergeOrder(
		items.map((item) => item.key),
		prefs.order[list],
	)
		.filter((key) => !hidden.has(key) && !opts.forcedHidden?.has(key))
		.map((key) => byKey.get(key) as T);
}

/** order with key moved by delta places, clamped to the ends. */
export function moveKey(order: string[], key: string, delta: number): string[] {
	const from = order.indexOf(key);
	if (from < 0) return order;
	return moveIndex(order, from, from + delta);
}

/** order with the item at from moved to index to, clamped to the ends. */
export function moveIndex(order: string[], from: number, to: number): string[] {
	const next = [...order];
	const [key] = next.splice(from, 1);
	next.splice(Math.max(0, Math.min(to, next.length)), 0, key);
	return next;
}

/**
 * The list's saved order after the user arranged the keys currently shown.
 * Saved keys not shown now, such as the other agents' or the other layout's
 * top-row items, keep their place after the shown ones.
 */
export function withOrder(
	saved: string[] | undefined,
	shown: string[],
): string[] {
	return [...shown, ...(saved ?? []).filter((k) => !shown.includes(k))];
}
