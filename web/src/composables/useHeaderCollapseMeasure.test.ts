import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { effectScope, nextTick, ref } from "vue";
import {
	type HeaderCollapseItem,
	useHeaderCollapse,
} from "./useHeaderCollapse";

// #1576: the composable measures the real header row: its actions, its
// chips on a single line, and each item's width, remembered while the item
// is in the "…" menu.

interface FakeRow {
	width: number;
	padding: number;
	gap: number;
	status: number;
	actions: number;
	items: Record<string, number>;
}

function fakeHeader(row: FakeRow) {
	const styles = new Map<object, Record<string, string>>();
	const status = { scrollWidth: 0 };
	const actions = {
		scrollWidth: 0,
		parentElement: null as unknown,
	};
	const titleRow = {
		clientWidth: 0,
		querySelector(sel: string) {
			if (sel === '[data-testid="settings-status"]') return status;
			const id = /data-testid="([^"]+)"/.exec(sel)?.[1] ?? "";
			const w = row.items[id];
			return w === undefined
				? null
				: { getBoundingClientRect: () => ({ width: w }) };
		},
	};
	actions.parentElement = titleRow;
	const anchor = {
		closest: (sel: string) =>
			sel === '[data-testid="settings-actions"]' ? actions : null,
	};
	const sync = () => {
		status.scrollWidth = row.status;
		actions.scrollWidth = row.actions;
		titleRow.clientWidth = row.width;
		styles.set(titleRow, {
			paddingLeft: `${row.padding}px`,
			paddingRight: `${row.padding}px`,
			columnGap: `${row.gap}px`,
		});
		styles.set(actions, { columnGap: `${row.gap}px` });
	};
	sync();
	vi.stubGlobal(
		"getComputedStyle",
		(el: object) =>
			styles.get(el) ?? { paddingLeft: "0", paddingRight: "0", columnGap: "0" },
	);
	return { anchor: anchor as unknown as HTMLElement, sync };
}

const ITEMS: HeaderCollapseItem[] = [
	{ id: "logs-toggle", below: 600 },
	{ id: "prompt-chat-toggle" },
	{ id: "queue-menu" },
	{ id: "board-info" },
];

let scope: ReturnType<typeof effectScope>;

beforeEach(() => {
	vi.spyOn(console, "warn").mockImplementation(() => {});
	scope = effectScope();
});

afterEach(() => {
	scope.stop();
	vi.unstubAllGlobals();
	vi.restoreAllMocks();
});

async function settle() {
	await nextTick();
	await nextTick();
}

it("counts the chips on a single line and moves items until it fits", async () => {
	// A 400px row with 16px padding leaves 368px. The chips are 200px and
	// the actions (Logs, Chat, Queue, "…": 4 × 36 + 3 × 8) 168px, plus the
	// 8px gap between them: 376px, 8px too wide.
	const row: FakeRow = {
		width: 400,
		padding: 16,
		gap: 8,
		status: 200,
		actions: 168,
		items: {
			"logs-toggle": 36,
			"prompt-chat-toggle": 36,
			"queue-menu": 36,
			"board-info": 24,
		},
	};
	const { anchor, sync } = fakeHeader(row);
	const el = ref<HTMLElement | null>(null);
	const single = ref(true);
	const collapsed = scope.run(() =>
		useHeaderCollapse(el, () => ITEMS, { singleLine: () => single.value }),
	);
	el.value = anchor;
	await settle();
	// Logs goes below its 600px threshold anyway, which is enough.
	expect([...(collapsed?.value ?? [])]).toEqual(["logs-toggle"]);

	// Logs left the row, using its remembered 36px width, and the row
	// narrows to 310px (278px inside): Chat and Queue go too.
	row.actions = 168 - 44;
	delete row.items["logs-toggle"];
	row.width = 310;
	sync();
	single.value = false;
	await settle();
	// Off a single line only the actions count: 124px fits in 278px.
	expect(collapsed?.value.size).toBe(1);
	single.value = true;
	await settle();
	expect([...(collapsed?.value ?? [])]).toEqual([
		"logs-toggle",
		"prompt-chat-toggle",
		"queue-menu",
	]);
});

it("collapses nothing outside a header row", async () => {
	const el = ref<HTMLElement | null>(null);
	const collapsed = scope.run(() => useHeaderCollapse(el, () => ITEMS));
	el.value = { closest: () => null } as unknown as HTMLElement;
	await settle();
	expect(collapsed?.value.size).toBe(0);
});
