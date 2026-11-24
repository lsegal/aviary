<template>
	<div :class="title ? '' : 'flex min-w-0 items-center gap-1'">
		<router-link v-if="back" :to="back.to" :data-testid="`${testid}-back`" :aria-label="`Back to ${back.label}`"
			:title="title ? undefined : `Back to ${back.label}`"
			:class="title
				? 'mb-2 inline-flex max-w-full items-center gap-1 text-sm font-medium text-blue-600 hover:underline dark:text-blue-400'
				: 'inline-flex h-7 w-7 shrink-0 items-center justify-center rounded-md text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-900 dark:text-gray-400 dark:hover:bg-gray-800 dark:hover:text-white'">
			<ArrowLeftIcon aria-hidden="true" class="h-4 w-4 shrink-0" />
			<span v-if="title" class="truncate">Back to {{ back.label }}</span>
		</router-link>
		<div :class="title ? 'flex flex-wrap items-center justify-between gap-3' : 'min-w-0 flex-1'">
		<div :class="title ? 'flex min-w-0 items-center gap-3' : 'min-w-0'">
			<slot name="icon" />
			<nav v-if="crumbs.length > 1" aria-label="Breadcrumb" :data-testid="`${testid}-breadcrumb`" class="min-w-0">
				<!-- Middle crumbs give up their width first, then the outer ones. -->
				<ol class="flex min-w-0 items-center gap-1.5 overflow-hidden"
					:class="title ? 'text-xl font-bold text-gray-900 dark:text-white' : 'text-sm text-gray-900 dark:text-white'">
					<li v-for="(crumb, i) in crumbs" :key="i" class="flex items-center gap-1.5" :class="crumbClass(i)"
						:title="crumb.label">
						<ChevronRightIcon v-if="i > 0" aria-hidden="true"
							class="h-4 w-4 shrink-0 text-gray-400 dark:text-gray-500" />
						<component :is="title ? 'h2' : 'span'" v-if="i === crumbs.length - 1" aria-current="page"
							:data-testid="`${testid}-title`" class="min-w-0 truncate" :class="title ? '' : 'font-medium'">
							<BreadcrumbSwitcher v-if="switcher" :label="crumb.label" :title="switcher.title"
								:items="switcher.items" />
							<template v-else>{{ crumb.label }}</template></component>
						<router-link v-else-if="crumb.to" :to="crumb.to"
							class="min-w-0 truncate rounded text-gray-500 hover:text-gray-900 hover:underline dark:text-gray-400 dark:hover:text-white">
							{{ crumb.label }}</router-link>
						<span v-else class="min-w-0 truncate text-gray-500 dark:text-gray-400">{{ crumb.label }}</span>
					</li>
				</ol>
			</nav>
			<component :is="title ? 'h2' : 'span'" v-else :data-testid="`${testid}-title`"
				:class="title ? 'text-xl font-bold text-gray-900 dark:text-white' : 'text-sm font-medium text-gray-900 dark:text-white'">
				{{ crumbs[0]?.label }}</component>
		</div>
		<div v-if="$slots.actions" class="flex flex-wrap items-center gap-2">
			<slot name="actions" />
		</div>
		</div>
	</div>
</template>

<script setup lang="ts">
// The breadcrumb every nested page shares (#1036): an optional back link to
// the parent page, then the trail from the agent or System down to the
// current page. Every crumb but the last links to its page. Settings pages
// show it as their title; other pages show it compact above their content.
import ArrowLeftIcon from "@heroicons/vue/24/outline/ArrowLeftIcon";
import ChevronRightIcon from "@heroicons/vue/24/outline/ChevronRightIcon";
import type { RouteLocationRaw } from "vue-router";
import type { SwitcherItem } from "../../lib/switcher";
import BreadcrumbSwitcher from "./BreadcrumbSwitcher.vue";

export interface PageCrumb {
	label: string;
	/** Where the crumb leads; the last crumb is the current page. */
	to?: RouteLocationRaw;
}

const props = defineProps<{
	/** The crumbs, ending with the current page; one crumb is a plain title. */
	crumbs: PageCrumb[];
	/** The parent page the back link returns to. */
	back?: { label: string; to: RouteLocationRaw } | null;
	/** Shows the crumbs as the page's heading, with a labeled back link. */
	title?: boolean;
	/** Makes the current crumb a switcher between its siblings (#1063). */
	switcher?: { title: string; items: SwitcherItem[] } | null;
	/** The prefix of the test IDs: -back, -breadcrumb and -title. */
	testid: string;
}>();

// Long names truncate with an ellipsis, middle crumbs first (#997): only
// they shrink, down to a sliver, while the first and current crumbs keep
// their width up to a share of the row and truncate past it.
function crumbClass(i: number): string {
	if (i === props.crumbs.length - 1) return "max-w-[40%] shrink-0";
	if (i === 0) return "max-w-[30%] shrink-0";
	return "min-w-[2.75rem] shrink";
}
</script>
