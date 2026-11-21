import {
	computed,
	type MaybeRefOrGetter,
	onScopeDispose,
	ref,
	shallowReactive,
	toValue,
	watch,
} from "vue";
import type { RouteLocationNormalized, Router } from "vue-router";

/**
 * Whether an editor's edits survive a navigation: true when the page that
 * holds them stays mounted, so leaving would not discard anything.
 */
export type KeepsEdits = (
	to: RouteLocationNormalized,
	from: RouteLocationNormalized,
) => boolean;

interface Editor {
	dirty: () => boolean;
	keeps: KeepsEdits;
}

// Every mounted editor with an explicit Save, registered while it is mounted.
const editors = shallowReactive(new Set<Editor>());

/** True while any registered editor holds unsaved edits. */
export const hasUnsavedChanges = computed(() =>
	[...editors].some((e) => e.dirty()),
);

const samePath: KeepsEdits = (to, from) => to.path === from.path;

/**
 * Registers an editor's dirty flag for the current component's lifetime.
 * While it is dirty, in-app navigation that would leave the editor asks
 * before leaving, and reloading or closing the tab shows the browser's own
 * prompt. By default a navigation leaves the editor when the path changes;
 * keeps overrides that for pages that hold their edits across routes.
 */
export function useUnsavedChanges(
	dirty: MaybeRefOrGetter<boolean>,
	options: { keeps?: KeepsEdits } = {},
) {
	const editor: Editor = {
		dirty: () => toValue(dirty),
		keeps: options.keeps ?? samePath,
	};
	editors.add(editor);
	onScopeDispose(() => editors.delete(editor));
}

/** The navigation waiting on the Leave / Stay prompt, if any. */
export const leavePrompt = ref<((leave: boolean) => void) | null>(null);

/** Answers the open Leave / Stay prompt. */
export function answerLeavePrompt(leave: boolean) {
	const resolve = leavePrompt.value;
	leavePrompt.value = null;
	resolve?.(leave);
}

function askToLeave(): Promise<boolean> {
	// A newer navigation replaces one still waiting; the old one stays put.
	answerLeavePrompt(false);
	return new Promise((resolve) => {
		leavePrompt.value = resolve;
	});
}

function onBeforeUnload(e: BeforeUnloadEvent) {
	e.preventDefault();
	// Older browsers show the prompt only when returnValue is set.
	e.returnValue = "";
}

/**
 * Installs the router guard and the beforeunload hook every editor
 * registered with useUnsavedChanges relies on. The beforeunload handler is
 * registered only while something is dirty, so clean pages unload freely.
 */
export function installUnsavedChanges(router: Router) {
	router.beforeEach(async (to, from) => {
		const leaving = [...editors].some((e) => e.dirty() && !e.keeps(to, from));
		if (!leaving) return;
		return await askToLeave();
	});
	watch(hasUnsavedChanges, (dirty) => {
		if (dirty) window.addEventListener("beforeunload", onBeforeUnload);
		else window.removeEventListener("beforeunload", onBeforeUnload);
	});
}
