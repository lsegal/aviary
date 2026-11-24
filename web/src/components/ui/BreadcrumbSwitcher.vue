<template>
	<button ref="trigger" type="button" data-testid="crumb-switcher-trigger" aria-haspopup="listbox"
		:aria-expanded="open" :aria-controls="open ? listId : undefined" aria-keyshortcuts="Alt+S"
		:title="`${title} (${shortcutLabel})`"
		class="inline-flex min-w-0 max-w-full items-center gap-0.5 rounded hover:bg-gray-100 focus-visible:outline-2 focus-visible:outline-blue-500 dark:hover:bg-gray-800"
		@click="open ? close() : show()">
		<span class="min-w-0 truncate">{{ label }}</span>
		<ChevronDownIcon aria-hidden="true" class="h-3.5 w-3.5 shrink-0 text-gray-500 dark:text-gray-400" />
	</button>
	<!-- On the body, so the breadcrumb's clipped row cannot cut it off. -->
	<Teleport to="body">
		<div v-if="open" ref="panel" role="dialog" :aria-label="title" data-testid="crumb-switcher"
			class="fixed z-50 flex flex-col overflow-hidden rounded-xl border border-gray-200 bg-white text-sm font-normal text-gray-900 shadow-xl dark:border-gray-700 dark:bg-gray-900 dark:text-white"
			:style="{ left: `${pos.left}px`, top: `${pos.top}px`, width: `${pos.width}px`, maxHeight: `calc(100vh - ${pos.top + 8}px)` }"
			@keydown="onKeydown">
			<div class="px-3 pb-2 pt-2.5 font-semibold" data-testid="crumb-switcher-title">{{ title }}</div>
			<div class="relative px-2 pb-2">
				<MagnifyingGlassIcon aria-hidden="true"
					class="pointer-events-none absolute left-4 top-2 h-4 w-4 text-gray-400" />
				<input ref="search" v-model="query" type="text" role="combobox" data-testid="crumb-switcher-search"
					:aria-label="`Find ${noun}`" :placeholder="`Find a ${noun}…`" aria-autocomplete="list"
					aria-expanded="true" :aria-controls="listId"
					:aria-activedescendant="shown.length ? optionId(active) : undefined"
					class="w-full rounded-md border border-gray-300 bg-white py-1 pl-7 pr-2 text-sm focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-800" />
			</div>
			<ul :id="listId" role="listbox" :aria-label="title"
				class="min-h-0 flex-1 overflow-y-auto border-t border-gray-100 p-1 dark:border-gray-800">
				<li v-for="(item, i) in shown" :id="optionId(i)" :key="item.key" role="option"
					:aria-selected="i === active" :data-current="item.current || undefined"
					data-testid="crumb-switcher-option" :title="item.label"
					class="flex cursor-pointer items-center gap-2 rounded-lg px-2 py-1.5"
					:class="i === active ? 'bg-gray-100 dark:bg-gray-800' : ''" @mousemove="active = i"
					@click="go(item)">
					<span class="flex h-4 w-4 shrink-0 items-center justify-center">
						<span v-if="item.current" role="img" aria-label="Current">
							<CheckIcon aria-hidden="true" class="h-4 w-4 text-blue-600 dark:text-blue-400" />
						</span>
					</span>
					<span class="flex h-4 w-4 shrink-0 items-center justify-center text-gray-500 dark:text-gray-400"
						:data-icon="item.icon" data-testid="crumb-switcher-icon">
						<MessagingLogo v-if="item.icon === 'signal' || item.icon === 'slack' || item.icon === 'discord'"
							:name="item.icon" class="h-4 w-4" />
						<component :is="ICONS[item.icon]" v-else aria-hidden="true" class="h-4 w-4" />
					</span>
					<span class="min-w-0 flex-1 truncate">{{ item.label }}</span>
					<span v-if="item.detail" class="shrink-0 text-xs text-gray-500 dark:text-gray-400">{{ item.detail }}</span>
				</li>
				<li v-if="!shown.length" role="presentation" data-testid="crumb-switcher-empty"
					class="px-2 py-1.5 text-gray-500 dark:text-gray-400">No matching {{ noun }}s</li>
			</ul>
		</div>
	</Teleport>
</template>

<script setup lang="ts">
// The breadcrumb leaf as a switcher between its siblings (#1063), like
// GitHub's repository switcher: the leaf with a caret opens a popover with
// a focused search box over the sibling projects, tasks, channels or files.
// Alt+S opens it from anywhere on the page.
import BoltIcon from "@heroicons/vue/24/outline/BoltIcon";
import BookOpenIcon from "@heroicons/vue/24/outline/BookOpenIcon";
import ChatBubbleLeftRightIcon from "@heroicons/vue/24/outline/ChatBubbleLeftRightIcon";
import CheckIcon from "@heroicons/vue/24/outline/CheckIcon";
import ChevronDownIcon from "@heroicons/vue/24/outline/ChevronDownIcon";
import ClockIcon from "@heroicons/vue/24/outline/ClockIcon";
import DocumentTextIcon from "@heroicons/vue/24/outline/DocumentTextIcon";
import HashtagIcon from "@heroicons/vue/24/outline/HashtagIcon";
import LockClosedIcon from "@heroicons/vue/24/outline/LockClosedIcon";
import MagnifyingGlassIcon from "@heroicons/vue/24/outline/MagnifyingGlassIcon";
import ViewColumnsIcon from "@heroicons/vue/24/outline/ViewColumnsIcon";
import {
	type Component,
	computed,
	nextTick,
	onBeforeUnmount,
	onMounted,
	ref,
	useId,
	watch,
} from "vue";
import { useRoute, useRouter } from "vue-router";
import {
	filterSwitcherItems,
	SWITCHER_KEY,
	type SwitcherIcon,
	type SwitcherItem,
} from "../../lib/switcher";
import MessagingLogo from "../MessagingLogo.vue";

const props = defineProps<{
	/** The current item's name, shown as the leaf crumb. */
	label: string;
	/** The popover's title, such as "Switch project". */
	title: string;
	items: SwitcherItem[];
}>();

const ICONS: Record<SwitcherIcon, Component> = {
	repo: BookOpenIcon,
	board: ViewColumnsIcon,
	discussions: ChatBubbleLeftRightIcon,
	task: ClockIcon,
	workflow: BoltIcon,
	signal: HashtagIcon,
	slack: HashtagIcon,
	discord: HashtagIcon,
	channel: HashtagIcon,
	file: DocumentTextIcon,
	builtin: LockClosedIcon,
};

const isMac = /Mac|iPhone|iPad/.test(navigator.platform);
const shortcutLabel = isMac ? `⌥${SWITCHER_KEY}` : `Alt+${SWITCHER_KEY}`;

const router = useRouter();
const route = useRoute();
const listId = `crumb-switcher-${useId()}`;
const optionId = (i: number) => `${listId}-${i}`;

const trigger = ref<HTMLButtonElement | null>(null);
const panel = ref<HTMLElement | null>(null);
const search = ref<HTMLInputElement | null>(null);
const open = ref(false);
const query = ref("");
const active = ref(0);
const pos = ref({ left: 0, top: 0, width: 320 });

const noun = computed(() => props.title.replace(/^Switch /, ""));
const shown = computed(() => filterSwitcherItems(props.items, query.value));

watch(query, () => {
	active.value = 0;
});
watch(active, () => {
	void nextTick(() =>
		document
			.getElementById(optionId(active.value))
			?.scrollIntoView({ block: "nearest" }),
	);
});

// Under the leaf, kept on screen: a phone-width popover spans the screen.
function place() {
	const rect = trigger.value?.getBoundingClientRect();
	if (!rect) return;
	const width = Math.min(320, window.innerWidth - 16);
	const left = Math.max(8, Math.min(rect.left, window.innerWidth - width - 8));
	pos.value = { left, top: rect.bottom + 4, width };
}

async function show() {
	query.value = "";
	place();
	open.value = true;
	const current = shown.value.findIndex((item) => item.current);
	active.value = Math.max(current, 0);
	await nextTick();
	search.value?.focus();
}

function close(refocus = true) {
	if (!open.value) return;
	open.value = false;
	if (refocus) trigger.value?.focus();
}

function go(item: SwitcherItem) {
	close(false);
	if (!item.current) void router.push(item.to);
}

function onKeydown(e: KeyboardEvent) {
	const n = shown.value.length;
	if (e.key === "Escape") {
		e.preventDefault();
		e.stopPropagation();
		close();
	} else if (e.key === "ArrowDown" && n) {
		e.preventDefault();
		active.value = (active.value + 1) % n;
	} else if (e.key === "ArrowUp" && n) {
		e.preventDefault();
		active.value = (active.value - 1 + n) % n;
	} else if (e.key === "Enter" && n) {
		e.preventDefault();
		go(shown.value[active.value]);
	} else if (e.key === "Tab") {
		close(false);
	}
}

function onShortcut(e: KeyboardEvent) {
	if (e.defaultPrevented || !e.altKey || e.ctrlKey || e.metaKey) return;
	if (e.code !== `Key${SWITCHER_KEY}`) return;
	e.preventDefault();
	void (open.value ? search.value?.focus() : show());
}

function onPointerDown(e: PointerEvent) {
	const t = e.target as Node;
	if (!open.value || panel.value?.contains(t) || trigger.value?.contains(t))
		return;
	close(false);
}

function onResize() {
	if (open.value) place();
}

watch(
	() => route.fullPath,
	() => close(false),
);

onMounted(() => {
	window.addEventListener("keydown", onShortcut);
	document.addEventListener("pointerdown", onPointerDown, true);
	window.addEventListener("resize", onResize);
});
onBeforeUnmount(() => {
	window.removeEventListener("keydown", onShortcut);
	document.removeEventListener("pointerdown", onPointerDown, true);
	window.removeEventListener("resize", onResize);
});
</script>
