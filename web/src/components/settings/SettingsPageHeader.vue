<template>
	<div data-testid="settings-header" class="shrink-0 bg-white dark:bg-gray-950">
		<!-- The breadcrumb bar: back to the parent page, then the trail. -->
		<!-- Only this bar stays pinned while the page scrolls; the title row
			slides up under it (#1437). -->
		<div data-testid="settings-crumb-bar"
			class="relative z-20 flex min-w-0 items-center gap-1 border-b bg-white dark:bg-gray-950 border-gray-200 px-2 py-1.5 sm:px-4 dark:border-gray-800">
			<router-link v-if="parent" :to="parent.to" data-testid="settings-back" :aria-label="`Back to ${parent.label}`"
				:title="`Back to ${parent.label}`" :class="BACK_CLS">
				<ArrowLeftIcon aria-hidden="true" class="h-4 w-4 shrink-0" />
			</router-link>
			<span v-else data-testid="settings-back-none" aria-hidden="true" :class="[BACK_CLS, 'pointer-events-none opacity-40']">
				<ArrowLeftIcon aria-hidden="true" class="h-4 w-4 shrink-0" />
			</span>
			<nav aria-label="Breadcrumb" data-testid="settings-breadcrumb" class="min-w-0 flex-1">
				<!-- Middle crumbs give up their width first, then the outer ones.
					On narrow screens only the parent and the current page show. -->
				<ol class="flex min-w-0 items-center gap-1.5 overflow-hidden text-sm text-gray-900 dark:text-white">
					<li v-for="(crumb, i) in crumbs" :key="i" class="items-center gap-1.5" :class="crumbClass(i)"
						:title="crumb.label">
						<ChevronRightIcon v-if="i > 0" aria-hidden="true" class="h-4 w-4 shrink-0 text-gray-400 dark:text-gray-500"
							:class="{ 'hidden sm:block': i === crumbs.length - 2 && i > 0 }" />
						<span v-if="i === crumbs.length - 1" aria-current="page" data-testid="settings-current"
							class="min-w-0 truncate font-medium">
							<BreadcrumbSwitcher v-if="switcher" :label="crumb.label" :title="switcher.title" :items="switcher.items" />
							<template v-else>{{ crumb.label }}</template>
						</span>
						<router-link v-else-if="crumb.to" v-slot="{ href, navigate }" :to="crumb.to" custom>
							<a :href="href"
								class="min-w-0 truncate rounded text-gray-500 hover:text-gray-900 hover:underline dark:text-gray-400 dark:hover:text-white"
								@click="navigate">{{ crumb.label }}</a>
						</router-link>
						<span v-else class="min-w-0 truncate text-gray-500 dark:text-gray-400">{{ crumb.label }}</span>
					</li>
				</ol>
			</nav>
		</div>
		<!-- The title row: the section's icon tile, the title with its status
			and subtitle, and the page's actions on the right. A compact header
			is one line at every width: its chips on the left and its toolbar
			on the right, never wrapping; the page hides the title and moves
			what doesn't fit into its "…" menu (#1576, #1584). -->
		<div data-testid="settings-title-row" :style="collapsed ? { marginTop: `-${collapsed}px` } : undefined"
			class="flex items-center border-b border-gray-200 px-4 sm:px-6 dark:border-gray-800"
			:class="compact ? 'relative flex-nowrap gap-2 py-2' : 'flex-wrap gap-3 py-3'">
			<div class="flex min-w-0 items-center gap-3"
				:class="compact ? 'flex-[0_1_auto]' : 'flex-[1_1_14rem]'">
				<span v-if="!compact" data-testid="settings-header-icon" aria-hidden="true"
					class="inline-flex h-7 w-7 shrink-0 items-center justify-center rounded-lg" :class="identity.tileClass">
					<slot name="icon">
						<component :is="identity.icon" class="h-4 w-4" :class="identity.iconClass" />
					</slot>
				</span>
				<div class="min-w-0 flex-1">
					<!-- The status chips wrap below the title rather than squeeze it
						(#1209); only a title wider than the row truncates. -->
					<div class="flex min-w-0 items-center gap-x-2 gap-y-1" :class="compact ? 'flex-nowrap' : 'flex-wrap'">
						<h2 data-testid="settings-title" class="max-w-full min-w-0 truncate text-lg font-bold text-gray-900 dark:text-white"
							:class="{ 'sr-only left-0 top-0': compact && titleHidden, 'shrink-0': compact }">
							<a v-if="titleLink" :href="titleLink.href" target="_blank" rel="noopener noreferrer"
								class="hover:underline" :title="titleLink.title">{{ title }}</a>
							<template v-else>{{ title }}</template>
						</h2>
						<div id="page-header-status" data-testid="settings-status" class="flex min-w-0 max-w-full items-center gap-2 empty:hidden"
							:class="compact ? 'flex-nowrap' : 'flex-wrap'">
							<slot name="status" />
						</div>
					</div>
					<div id="page-header-subtitle" data-testid="settings-subtitle" class="min-w-0 text-xs text-gray-500 dark:text-gray-400">
						<slot />
					</div>
				</div>
			</div>
			<!-- The actions keep to one row (#1347): when they don't fit beside
				the title they move under it, and a select truncates its label
				before anything wraps. -->
			<div data-testid="settings-actions" class="ml-auto flex min-w-0 max-w-full flex-nowrap items-center justify-end gap-2">
				<div id="page-header-actions" class="contents" />
				<slot name="actions" />
			</div>
		</div>
	</div>
</template>

<script setup lang="ts">
// The header every routed page shares (#1147): the Scheduled Tasks task
// page's header from before #1067. A thin breadcrumb bar, with an arrow
// back to the page's parent and the trail from the agent or System down to
// the page (#1036), then a title row: the section's icon tile, the page's
// title with an optional status chip and subtitle, and its actions on the
// right. Every link is a route link, so it works when the page is opened
// directly from its URL, and leaving with unsaved edits asks first through
// the router's guard. Only the current crumb is marked the current page,
// even where a crumb's route matches it but for its query, so crumbs
// render their own anchors. A nested route's page, such as a Code Loop
// job, teleports its status, subtitle and actions into the
// #page-header-status, #page-header-subtitle and #page-header-actions
// targets.
import ArrowLeftIcon from "@heroicons/vue/24/outline/ArrowLeftIcon";
import ChevronRightIcon from "@heroicons/vue/24/outline/ChevronRightIcon";
import { computed } from "vue";
import type { RouteLocationRaw } from "vue-router";
import type { SwitcherItem } from "../../lib/switcher";
import type { SystemPageIdentity } from "../../lib/systemNav";
import BreadcrumbSwitcher from "../ui/BreadcrumbSwitcher.vue";

export interface SettingsCrumb {
	label: string;
	/** Where the crumb leads; the last crumb is the current page. */
	to?: RouteLocationRaw;
}

export interface SettingsBack {
	label: string;
	to: RouteLocationRaw;
}

const props = defineProps<{
	/** The crumbs, ending with the current page, whose label is the title. */
	crumbs: SettingsCrumb[];
	/** The section's icon and its muted tile (#896). */
	identity: SystemPageIdentity;
	/**
	 * The parent page the back arrow returns to; by default the nearest
	 * crumb above the current page that links, and none when null.
	 */
	back?: SettingsBack | null;
	/** An outside page the title opens in a new tab, such as a repository. */
	titleLink?: { href: string; title: string } | null;
	/** Makes the current crumb a switcher between its siblings (#1063). */
	switcher?: { title: string; items: SwitcherItem[] } | null;
	/** How far the title row has slid up under the breadcrumb bar (#1437). */
	collapsed?: number;
	/**
	 * Drops the icon tile and keeps the title, chips and toolbar on one
	 * line at every width (#1570, #1576, #1584).
	 */
	compact?: boolean;
	/**
	 * In a compact header, shows the title only to screen readers, since
	 * the breadcrumb names the page; the page hides it when the line has
	 * no room to spare for it (#1584).
	 */
	titleHidden?: boolean;
}>();

const BACK_CLS =
	"inline-flex h-7 w-7 shrink-0 items-center justify-center rounded-md text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-900 dark:text-gray-400 dark:hover:bg-gray-800 dark:hover:text-white";

const title = computed(() => props.crumbs.at(-1)?.label ?? "");

const parent = computed((): SettingsBack | null => {
	if (props.back !== undefined) return props.back;
	for (let i = props.crumbs.length - 2; i >= 0; i--) {
		const { label, to } = props.crumbs[i];
		if (to) return { label, to };
	}
	return null;
});

// Long names truncate with an ellipsis, middle crumbs first (#997): only
// they shrink, down to a sliver, while the first and current crumbs keep
// their width up to a share of the row and truncate past it. Below sm only
// the parent and current crumbs show, as "← Parent › Page".
function crumbClass(i: number): string {
	const last = props.crumbs.length - 1;
	if (i === last) return "flex max-w-[60%] shrink-0 sm:max-w-[40%]";
	const shown = i === last - 1 ? "flex" : "hidden sm:flex";
	if (i === 0) return `${shown} max-w-[40%] shrink-0 sm:max-w-[30%]`;
	return `${shown} min-w-[2.75rem] shrink`;
}
</script>
