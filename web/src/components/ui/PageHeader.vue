<template>
	<header data-testid="page-header"
		class="shrink-0 border-b border-gray-200/80 bg-white/90 px-4 py-4 backdrop-blur sm:px-6 dark:border-gray-800/80 dark:bg-gray-950/88">
		<router-link v-if="parent" :to="parent.to" data-testid="page-back" :aria-label="`Back to ${parent.label}`"
			class="mb-2 inline-flex max-w-full items-center gap-1 text-sm font-medium text-blue-600 hover:underline dark:text-blue-400">
			<ArrowLeftIcon aria-hidden="true" class="h-4 w-4 shrink-0" />
			<span class="truncate">Back to {{ parent.label }}</span>
		</router-link>
		<div class="flex flex-wrap items-center justify-between gap-3">
			<!-- The title keeps a share of the row; past it the actions wrap below. -->
			<div class="flex min-w-0 flex-[1_1_14rem] items-center gap-3">
				<slot name="icon" />
				<div class="min-w-0 flex-1">
					<nav v-if="crumbs.length > 1" aria-label="Breadcrumb" data-testid="page-breadcrumb" class="min-w-0">
						<!-- Middle crumbs give up their width first, then the outer ones. -->
						<ol class="flex min-w-0 items-center gap-1.5 overflow-hidden text-xl font-bold text-gray-900 dark:text-white">
							<li v-for="(crumb, i) in crumbs" :key="i" class="flex items-center gap-1.5" :class="crumbClass(i)"
								:title="crumb.label">
								<ChevronRightIcon v-if="i > 0" aria-hidden="true"
									class="h-4 w-4 shrink-0 text-gray-400 dark:text-gray-500" />
								<h2 v-if="i === crumbs.length - 1" aria-current="page" data-testid="page-title"
									class="min-w-0 truncate">{{ crumb.label }}</h2>
								<router-link v-else-if="crumb.to" v-slot="{ href, navigate }" :to="crumb.to" custom>
									<a :href="href"
										class="min-w-0 truncate rounded text-gray-500 hover:text-gray-900 hover:underline dark:text-gray-400 dark:hover:text-white"
										@click="navigate">{{ crumb.label }}</a>
								</router-link>
								<span v-else class="min-w-0 truncate text-gray-500 dark:text-gray-400">{{ crumb.label }}</span>
							</li>
						</ol>
					</nav>
					<h2 v-else data-testid="page-title" class="truncate text-xl font-bold text-gray-900 dark:text-white">
						{{ crumbs[0]?.label }}</h2>
					<slot />
				</div>
			</div>
			<div v-if="$slots.actions" class="flex flex-wrap items-center gap-2">
				<slot name="actions" />
			</div>
		</div>
	</header>
</template>

<script setup lang="ts">
// The header every nested page shares (#1064): a back link to the page's
// parent, then its breadcrumb from the agent or System down, whose last
// crumb is the page's title (#986, #1036). Below the title sits the page's
// own detail line; its icon and actions sit beside it. Every link is a route
// link, so it works when the page is opened directly from its URL, and
// leaving with unsaved edits asks first through the router's guard. Only
// the title is marked the current page, even where a crumb's route matches
// it but for its query, so crumbs render their own anchors.
import ArrowLeftIcon from "@heroicons/vue/24/outline/ArrowLeftIcon";
import ChevronRightIcon from "@heroicons/vue/24/outline/ChevronRightIcon";
import { computed } from "vue";
import type { RouteLocationRaw } from "vue-router";

export interface PageCrumb {
	label: string;
	/** Where the crumb leads; the last crumb is the current page. */
	to?: RouteLocationRaw;
}

export interface PageBack {
	label: string;
	to: RouteLocationRaw;
}

const props = defineProps<{
	/** The crumbs, ending with the current page; one crumb is a plain title. */
	crumbs: PageCrumb[];
	/**
	 * The parent page the back link returns to; by default the nearest
	 * crumb above the current page that links, and none when null.
	 */
	back?: PageBack | null;
}>();

const parent = computed((): PageBack | null => {
	if (props.back !== undefined) return props.back;
	for (let i = props.crumbs.length - 2; i >= 0; i--) {
		const { label, to } = props.crumbs[i];
		if (to) return { label, to };
	}
	return null;
});

// Long names truncate with an ellipsis, middle crumbs first (#997): only
// they shrink, down to a sliver, while the first and current crumbs keep
// their width up to a share of the row and truncate past it.
function crumbClass(i: number): string {
	if (i === props.crumbs.length - 1) return "max-w-[40%] shrink-0";
	if (i === 0) return "max-w-[30%] shrink-0";
	return "min-w-[2.75rem] shrink";
}
</script>
