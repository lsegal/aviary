import { type Ref, ref, watch } from "vue";

// A collapsible sidebar group that opens by itself when the route reaches
// one of its pages, so the active page is never hidden (#928). Opening
// doesn't pin it: a manual collapse holds until the route reaches another of
// the group's pages. Leaving the group's routes never collapses it.

export interface RouteExpandedGroupState {
	collapsed: Ref<boolean>;
	/** The page the group last opened for, cleared on leaving its routes. */
	expandedFor: Ref<string | null>;
}

/**
 * The group's state. Create it at module level so every layout instance
 * shares it, and a remount on the same page keeps a manual collapse.
 */
export function createRouteExpandedGroupState(
	collapsed = true,
): RouteExpandedGroupState {
	return { collapsed: ref(collapsed), expandedFor: ref(null) };
}

/**
 * Expands the group whenever `activePage` names a new page of the group, and
 * returns its collapsed state for the toggle. `activePage` is undefined when
 * the route is none of the group's pages.
 */
export function useRouteExpandedGroup(
	state: RouteExpandedGroupState,
	activePage: () => string | undefined,
): Ref<boolean> {
	watch(
		activePage,
		(page) => {
			if (page === state.expandedFor.value) return;
			state.expandedFor.value = page ?? null;
			if (page) state.collapsed.value = false;
		},
		{ immediate: true },
	);
	return state.collapsed;
}
