<template>
	<div data-testid="settings-header"
		class="sticky top-0 z-20 -mx-4 mb-6 border-b border-gray-200/80 bg-white/90 px-4 py-4 backdrop-blur sm:-mx-6 sm:px-6 dark:border-gray-800/80 dark:bg-gray-950/88">
		<router-link v-if="back" :to="back.to" data-testid="settings-back"
			class="mb-2 inline-flex max-w-full items-center gap-1 text-sm font-medium text-blue-600 hover:underline dark:text-blue-400">
			<ArrowLeftIcon aria-hidden="true" class="h-4 w-4 shrink-0" />
			<span class="truncate">Back to {{ back.label }}</span>
		</router-link>
		<div class="flex flex-wrap items-center justify-between gap-3">
			<div class="flex min-w-0 items-center gap-3">
				<slot name="icon" />
				<nav v-if="crumbs.length > 1" aria-label="Breadcrumb" data-testid="settings-breadcrumb" class="min-w-0">
					<ol class="flex min-w-0 flex-wrap items-center gap-x-1.5 gap-y-1 text-xl font-bold text-gray-900 dark:text-white">
						<li v-for="(crumb, i) in crumbs" :key="i" class="flex min-w-0 items-center gap-1.5">
							<ChevronRightIcon v-if="i > 0" aria-hidden="true"
								class="h-4 w-4 shrink-0 text-gray-400 dark:text-gray-500" />
							<h2 v-if="i === crumbs.length - 1" aria-current="page" data-testid="settings-title" class="truncate">
								{{ crumb.label }}</h2>
							<router-link v-else-if="crumb.to" :to="crumb.to"
								class="truncate rounded text-gray-500 hover:text-gray-900 hover:underline dark:text-gray-400 dark:hover:text-white">
								{{ crumb.label }}</router-link>
							<span v-else class="truncate text-gray-500 dark:text-gray-400">{{ crumb.label }}</span>
						</li>
					</ol>
				</nav>
				<h2 v-else class="text-xl font-bold text-gray-900 dark:text-white" data-testid="settings-title">
					{{ crumbs[0]?.label }}</h2>
			</div>
			<div class="flex flex-wrap items-center gap-2">
				<slot name="actions" />
			</div>
		</div>
	</div>
</template>

<script setup lang="ts">
// The header every Settings page shares (#986): a back link to the page the
// settings belong to, the page's breadcrumb, and its actions. The links are
// route links, so they work when a settings URL is opened directly, and
// leaving with unsaved edits asks first through the router's guard.
import ArrowLeftIcon from "@heroicons/vue/24/outline/ArrowLeftIcon";
import ChevronRightIcon from "@heroicons/vue/24/outline/ChevronRightIcon";
import type { RouteLocationRaw } from "vue-router";

export interface SettingsCrumb {
	label: string;
	/** Where the crumb leads; the last crumb is the current page. */
	to?: RouteLocationRaw;
}

defineProps<{
	/** The crumbs, ending with the current page; one crumb is a plain title. */
	crumbs: SettingsCrumb[];
	/** The parent page the back link returns to. */
	back?: { label: string; to: RouteLocationRaw } | null;
}>();
</script>
