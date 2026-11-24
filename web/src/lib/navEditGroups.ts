import type { ComputedRef, InjectionKey } from "vue";
import type { NavList } from "./navPrefs";

/** A row of the sidebar editor (#1003). */
export interface NavEditItem {
	key: string;
	label: string;
}

/** One arrangeable list in the sidebar editor. */
export interface NavEditGroup {
	list: NavList;
	title: string;
	items: NavEditItem[];
}

/**
 * The sidebar editor's lists, provided by the layout, which knows the pages
 * this session shows, to the editor in Settings → Display (#1093).
 */
export const navEditGroupsKey: InjectionKey<ComputedRef<NavEditGroup[]>> =
	Symbol("navEditGroups");
