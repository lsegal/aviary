<template>
	<div aria-live="assertive" class="pointer-events-none fixed inset-x-0 top-0 z-[70] flex flex-col items-center gap-2 px-2 pt-2">
		<div v-for="n in banners" :key="n.id" data-testid="notification-banner" :data-severity="n.severity" role="alert"
			class="pointer-events-auto flex w-full max-w-2xl items-start gap-3 rounded-xl border px-4 py-3 shadow-xl"
			:class="tone[n.severity] ?? tone.info">
			<component :is="icon[n.severity] ?? icon.info" aria-hidden="true" class="mt-0.5 h-5 w-5 shrink-0" />
			<button type="button" class="min-w-0 flex-1 text-left" @click="notifications.open(n)">
				<span class="block font-semibold [overflow-wrap:anywhere]">{{ n.title }}<span v-if="n.count > 1" class="ml-1 font-normal opacity-70">×{{ n.count }}</span></span>
				<span v-if="n.body" class="mt-0.5 line-clamp-3 block whitespace-pre-line text-sm opacity-90 [overflow-wrap:anywhere]">{{ n.body }}</span>
			</button>
			<button type="button" aria-label="Dismiss" class="shrink-0 opacity-60 hover:opacity-100" @click="notifications.dismissBanner(n.id)">
				<XMarkIcon aria-hidden="true" class="h-5 w-5" />
			</button>
		</div>
	</div>
</template>

<script setup lang="ts">
// The big notification style (#1215): a banner across the top of the page
// for important events, such as a failed Code Loop job. Clicking it opens
// the notification's page.
import CheckCircleIcon from "@heroicons/vue/24/outline/CheckCircleIcon";
import ExclamationTriangleIcon from "@heroicons/vue/24/outline/ExclamationTriangleIcon";
import InformationCircleIcon from "@heroicons/vue/24/outline/InformationCircleIcon";
import XCircleIcon from "@heroicons/vue/24/outline/XCircleIcon";
import XMarkIcon from "@heroicons/vue/24/outline/XMarkIcon";
import { markRaw } from "vue";
import { useNotifications } from "../../composables/useNotifications";

const notifications = useNotifications();
const { banners } = notifications;

const tone: Record<string, string> = {
	info: "border-blue-200 bg-blue-50 text-blue-950 dark:border-blue-900 dark:bg-blue-950 dark:text-blue-100",
	success: "border-emerald-200 bg-emerald-50 text-emerald-950 dark:border-emerald-900 dark:bg-emerald-950 dark:text-emerald-100",
	warning: "border-amber-200 bg-amber-50 text-amber-950 dark:border-amber-900 dark:bg-amber-950 dark:text-amber-100",
	error: "border-red-200 bg-red-50 text-red-950 dark:border-red-900 dark:bg-red-950 dark:text-red-100",
};
const icon = {
	info: markRaw(InformationCircleIcon),
	success: markRaw(CheckCircleIcon),
	warning: markRaw(ExclamationTriangleIcon),
	error: markRaw(XCircleIcon),
} as Record<string, unknown>;
</script>
