<template>
	<PopoverRoot v-model:open="open">
		<PopoverTrigger asChild>
			<button type="button" data-testid="notification-bell" :aria-label="label" :title="label" :class="buttonClass">
				<BellIcon aria-hidden="true" :class="rail ? 'h-5 w-5' : 'h-4 w-4'" />
				<span v-if="unread > 0" data-testid="notification-unread"
					class="absolute -right-0.5 -top-0.5 min-w-4 rounded-full bg-red-600 px-1 text-center text-[10px] font-semibold leading-4 text-white">
					{{ unread > 99 ? "99+" : unread }}
				</span>
			</button>
		</PopoverTrigger>
		<PopoverPortal>
			<PopoverContent :side="side" align="end" :side-offset="6" :collision-padding="8" data-testid="notification-center"
				class="z-50 flex max-h-[min(32rem,80vh)] w-[min(22rem,calc(100vw-1rem))] flex-col overflow-hidden rounded-xl border border-gray-200 bg-white shadow-lg dark:border-gray-700 dark:bg-gray-900">
				<div class="flex items-center gap-2 border-b border-gray-200 px-3 py-2 dark:border-gray-800">
					<h2 class="flex-1 text-sm font-semibold text-gray-900 dark:text-white">Notifications</h2>
					<button type="button" data-testid="notification-mark-all-read" :disabled="unread === 0" :class="actionClass"
						@click="notifications.markRead()">Mark all read</button>
					<button type="button" data-testid="notification-clear" :disabled="history.length === 0" :class="actionClass"
						@click="notifications.clear()">Clear</button>
				</div>
				<ul v-if="history.length" class="min-h-0 flex-1 overflow-y-auto" data-testid="notification-list">
					<li v-for="n in history" :key="n.id">
						<button type="button" data-testid="notification-item" :data-read="n.read" :data-type="n.type"
							class="flex w-full items-start gap-2 px-3 py-2 text-left transition hover:bg-gray-50 dark:hover:bg-gray-800/60"
							@click="openItem(n)">
							<span aria-hidden="true" class="mt-1.5 h-2 w-2 shrink-0 rounded-full"
								:class="n.read ? 'bg-transparent' : severityDot[n.severity] ?? severityDot.info" />
							<span class="min-w-0 flex-1">
								<span class="flex items-baseline gap-2">
									<span class="min-w-0 flex-1 truncate text-sm text-gray-900 dark:text-gray-100"
										:class="n.read ? '' : 'font-semibold'">{{ n.title }}</span>
									<span v-if="n.count > 1" class="shrink-0 rounded bg-gray-100 px-1 text-[10px] text-gray-600 dark:bg-gray-800 dark:text-gray-300">×{{ n.count }}</span>
									<span class="shrink-0 text-[11px] text-gray-500 dark:text-gray-400">{{ ago(n.time) }}</span>
								</span>
								<span v-if="n.body" class="line-clamp-2 block whitespace-pre-line text-xs text-gray-600 dark:text-gray-400">{{ n.body }}</span>
							</span>
						</button>
					</li>
				</ul>
				<p v-else data-testid="notification-empty" class="px-3 py-8 text-center text-sm text-gray-500 dark:text-gray-400">
					No notifications
				</p>
				<RouterLink to="/system/notifications" data-testid="notification-settings-link"
					class="border-t border-gray-200 px-3 py-2 text-xs text-blue-600 hover:underline dark:border-gray-800 dark:text-blue-400"
					@click="open = false">Notification settings</RouterLink>
			</PopoverContent>
		</PopoverPortal>
	</PopoverRoot>
</template>

<script setup lang="ts">
// The notification center (#1215): a bell with the unread count, opening
// the recent notifications, newest first, with mark-read and clear.
// Clicking one opens its page.
import BellIcon from "@heroicons/vue/24/outline/BellIcon";
import {
	PopoverContent,
	PopoverPortal,
	PopoverRoot,
	PopoverTrigger,
} from "radix-vue";
import { computed, ref, watch } from "vue";
import { useNotifications } from "../../composables/useNotifications";
import { type AppNotification, ago } from "../../lib/notifications";

const props = defineProps<{
	/** Sized as a collapsed-rail icon, opening to its right. */
	rail?: boolean;
	/** Which side the list opens on. */
	side?: "top" | "right" | "bottom" | "left";
}>();

const notifications = useNotifications();
const { history, unread } = notifications;
const open = ref(false);
const side = computed(() => props.side ?? (props.rail ? "right" : "top"));

watch(open, (isOpen) => {
	if (isOpen) void notifications.refresh();
});

function openItem(n: AppNotification) {
	open.value = false;
	notifications.open(n);
}

const label = computed(() =>
	unread.value > 0 ? `Notifications, ${unread.value} unread` : "Notifications",
);

const buttonClass = computed(() => {
	const size = props.rail ? "h-10 w-10 rounded-xl" : "h-8 w-8 rounded-lg";
	const base = `relative inline-flex shrink-0 items-center justify-center transition-colors ${size}`;
	if (open.value)
		return `${base} bg-white text-gray-950 shadow-sm dark:bg-gray-800 dark:text-white`;
	return `${base} text-gray-500 hover:bg-gray-200 hover:text-gray-900 dark:text-gray-400 dark:hover:bg-gray-800 dark:hover:text-white`;
});

const actionClass =
	"rounded-md px-2 py-1 text-xs text-gray-600 transition hover:bg-gray-100 disabled:opacity-40 dark:text-gray-300 dark:hover:bg-gray-800";

const severityDot: Record<string, string> = {
	info: "bg-blue-500",
	success: "bg-emerald-500",
	warning: "bg-amber-500",
	error: "bg-red-500",
};
</script>
