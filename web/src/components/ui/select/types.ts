/** One choice in an AppSelect or AppCombobox. */
export interface SelectOption<V extends string | number = string> {
	value: V;
	label: string;
	/** A second, muted line under the label. */
	description?: string;
	disabled?: boolean;
	/** Options sharing a group are listed together under this header. */
	group?: string;
	/** Extra text the combobox search matches besides label and description. */
	keywords?: string;
}

export interface SelectOptionGroup<V extends string | number = string> {
	label: string;
	options: SelectOption<V>[];
}

/**
 * Splits options into groups in order of first appearance; ungrouped options
 * form groups with an empty label wherever they fall.
 */
export function groupOptions<V extends string | number>(
	options: SelectOption<V>[],
): SelectOptionGroup<V>[] {
	const groups: SelectOptionGroup<V>[] = [];
	for (const option of options) {
		const label = option.group ?? "";
		const last = groups[groups.length - 1];
		const named = label ? groups.find((g) => g.label === label) : undefined;
		if (named) named.options.push(option);
		else if (!label && last && !last.label) last.options.push(option);
		else groups.push({ label, options: [option] });
	}
	return groups;
}

/**
 * The primitives reserve the empty string for "nothing selected", so an
 * option whose value is "" is stored under this key instead.
 */
export const EMPTY_KEY = "\u0000empty";

export function optionKey(value: string | number): string {
	const s = String(value);
	return s === "" ? EMPTY_KEY : s;
}

/** Whether option matches a combobox search term. */
export function matchesSearch(
	option: SelectOption<string | number>,
	term: string,
): boolean {
	const q = term.trim().toLowerCase();
	if (!q) return true;
	return [option.label, option.description, option.keywords, String(option.value)]
		.filter(Boolean)
		.some((s) => String(s).toLowerCase().includes(q));
}
