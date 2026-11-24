<template>
	<div data-testid="settings-header"
		class="sticky top-0 z-20 -mx-4 mb-6 border-b border-gray-200/80 bg-white/90 px-4 py-4 backdrop-blur sm:-mx-6 sm:px-6 dark:border-gray-800/80 dark:bg-gray-950/88">
		<PageBreadcrumb title testid="settings" :crumbs="crumbs" :back="back">
			<template #icon>
				<slot name="icon" />
			</template>
			<template #actions>
				<slot name="actions" />
			</template>
		</PageBreadcrumb>
	</div>
</template>

<script setup lang="ts">
// The header every Settings page shares (#986): a back link to the page the
// settings belong to, the page's breadcrumb, and its actions. The breadcrumb
// is the one every nested page shows (#1036), here as the page's title. The
// links are route links, so they work when a settings URL is opened
// directly, and leaving with unsaved edits asks first through the router's
// guard.
import type { RouteLocationRaw } from "vue-router";
import PageBreadcrumb, { type PageCrumb } from "../ui/PageBreadcrumb.vue";

export type SettingsCrumb = PageCrumb;

defineProps<{
	/** The crumbs, ending with the current page; one crumb is a plain title. */
	crumbs: SettingsCrumb[];
	/** The parent page the back link returns to. */
	back?: { label: string; to: RouteLocationRaw } | null;
}>();
</script>
