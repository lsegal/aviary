import { afterEach, beforeEach, expect, it } from "vitest";
import { effectScope, nextTick, ref, toRaw } from "vue";
import { useFilterRowFit } from "./useFilterRowFit";

// #1490: the board toolbar decides its layout from its own width and its
// items' widths, re-measuring whenever any of them resizes.

let observers: FakeObserver[] = [];
class FakeObserver {
	targets = new Set<Element>();
	constructor(readonly callback: () => void) {
		observers.push(this);
	}
	observe(el: Element) {
		this.targets.add(el);
	}
	unobserve(el: Element) {
		this.targets.delete(el);
	}
	disconnect() {
		this.targets.clear();
	}
}
const original = globalThis.ResizeObserver;
beforeEach(() => {
	observers = [];
	globalThis.ResizeObserver = FakeObserver as unknown as typeof ResizeObserver;
});
afterEach(() => {
	globalThis.ResizeObserver = original;
});

const el = (width: number) =>
	({ clientWidth: width, offsetWidth: width }) as unknown as HTMLElement;
const resize = () => {
	for (const o of observers) if (o.targets.size) o.callback();
};

it("folds and unfolds the filters as the toolbar's width changes", async () => {
	const row = el(1000);
	const toggle = el(36);
	const count = el(60);
	const scope = effectScope();
	const fit = scope.run(() =>
		useFilterRowFit(ref(row), ref(470), [ref(toggle), ref(count)]),
	);
	if (!fit) throw new Error("no fit");
	await nextTick();
	expect(fit.collapsed.value).toBe(false);
	expect(fit.stacked.value).toBe(false);

	// A chat panel narrows the toolbar: the filters fold, search stays.
	(row as { clientWidth: number }).clientWidth = 800;
	resize();
	expect(fit.collapsed.value).toBe(true);
	expect(fit.stacked.value).toBe(false);

	// A phone stacks search on its own row.
	(row as { clientWidth: number }).clientWidth = 358;
	resize();
	expect(fit.stacked.value).toBe(true);

	// Disposing the scope stops observing.
	scope.stop();
	expect(observers.every((o) => o.targets.size === 0)).toBe(true);
});

it("re-observes when an item it measures is replaced", async () => {
	const count = ref<HTMLElement | null>(el(60));
	const scope = effectScope();
	scope.run(() => useFilterRowFit(ref(el(1000)), ref(470), [count]));
	await nextTick();
	const replacement = el(120);
	count.value = replacement;
	await nextTick();
	// Plain test objects come back from a ref as proxies; DOM elements don't.
	const watched = [...(observers.at(-1)?.targets ?? [])].map((t) => toRaw(t));
	expect(watched).toContain(replacement);
	scope.stop();
});
