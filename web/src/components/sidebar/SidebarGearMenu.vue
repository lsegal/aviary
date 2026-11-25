<template>
	<DropdownMenuRoot v-model:open="open">
		<DropdownMenuTrigger asChild>
			<button type="button" aria-label="Settings menu" title="Settings menu" data-testid="gear-menu-button"
				:class="buttonClass" @pointerenter="prefetchSettingsView" @focus="prefetchSettingsView">
				<Cog6ToothIcon aria-hidden="true" :class="rail ? 'h-5 w-5' : 'h-4 w-4'" />
			</button>
		</DropdownMenuTrigger>
		<DropdownMenuContent :side="rail ? 'right' : 'top'" align="end" :side-offset="6"
			data-testid="gear-menu"
			class="z-50 min-w-40 rounded-xl border border-gray-200 bg-white p-1 shadow-lg dark:border-gray-700 dark:bg-gray-900">
			<DropdownMenuItem v-if="minimal" :class="itemClass" data-testid="gear-menu-display"
				@select="router.push('/system/display')">
				<SwatchIcon aria-hidden="true" class="h-4 w-4" />
				Display
			</DropdownMenuItem>
			<DropdownMenuItem v-if="minimal" :class="itemClass" data-testid="gear-menu-notifications"
				@select="router.push('/system/notifications')">
				<BellIcon aria-hidden="true" class="h-4 w-4" />
				Notifications
			</DropdownMenuItem>
			<template v-else>
				<DropdownMenuItem :class="itemClass" data-testid="gear-menu-settings" @select="router.push('/system/settings')">
					<Cog6ToothIcon aria-hidden="true" class="h-4 w-4" />
					Settings
				</DropdownMenuItem>
				<DropdownMenuItem :class="itemClass" data-testid="gear-menu-edit-nav"
					@select="router.push({ path: '/system/display', hash: '#sidebar' })">
					<AdjustmentsHorizontalIcon aria-hidden="true" class="h-4 w-4" />
					Edit sidebar
				</DropdownMenuItem>
				<DropdownMenuItem :class="itemClass" :disabled="restartDisabled" data-testid="restart-button"
					@select="emit('restart')">
					<ArrowPathIcon aria-hidden="true" class="h-4 w-4" :class="restartPending ? 'animate-spin' : ''" />
					Restart
				</DropdownMenuItem>
				<DropdownMenuItem :class="dangerClass" data-testid="gear-menu-logout" @select="logout">
					<ArrowRightOnRectangleIcon aria-hidden="true" class="h-4 w-4" />
					Logout
				</DropdownMenuItem>
			</template>
		</DropdownMenuContent>
	</DropdownMenuRoot>
</template>

<script setup lang="ts">
// The sidebar's gear (#822): a menu of Settings, Edit sidebar (#1003), the
// safe Restart, and Logout, rather than a link straight to Settings. Edit
// sidebar opens Settings → Display at its Sidebar section (#1093), and a
// watch session's menu is Display and Notifications (#991, #1215). Hovering, focusing or
// opening it prefetches the Settings chunk so choosing Settings routes at
// once (#897).
import AdjustmentsHorizontalIcon from "@heroicons/vue/24/outline/AdjustmentsHorizontalIcon";
import ArrowPathIcon from "@heroicons/vue/24/outline/ArrowPathIcon";
import ArrowRightOnRectangleIcon from "@heroicons/vue/24/outline/ArrowRightOnRectangleIcon";
import BellIcon from "@heroicons/vue/24/outline/BellIcon";
import Cog6ToothIcon from "@heroicons/vue/24/outline/Cog6ToothIcon";
import SwatchIcon from "@heroicons/vue/24/outline/SwatchIcon";
import {
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuRoot,
	DropdownMenuTrigger,
} from "radix-vue";
import { computed, ref, watch } from "vue";
import { useRouter } from "vue-router";
import { isSettingsPath } from "../../lib/systemNav";
import { prefetchSettingsView } from "../../router/settingsView";
import { useAuthStore } from "../../stores/auth";

const props = defineProps<{
	/** Sized as a collapsed-rail icon, opening to its right. */
	rail?: boolean;
	/** A watch session's menu (#991), offering only Display. */
	minimal?: boolean;
	restartDisabled?: boolean;
	restartPending?: boolean;
}>();
const emit = defineEmits<{ restart: [] }>();

const open = ref(false);
watch(open, (isOpen) => {
	if (isOpen) prefetchSettingsView();
});
const router = useRouter();
const auth = useAuthStore();

function logout() {
	auth.logout();
	void router.push("/login");
}

const settingsActive = computed(() =>
	isSettingsPath(router.currentRoute.value.path),
);

const buttonClass = computed(() => {
	const size = props.rail ? "h-10 w-10 rounded-xl" : "h-8 w-8 rounded-lg";
	const base = `inline-flex shrink-0 items-center justify-center transition-colors ${size}`;
	if (settingsActive.value || open.value)
		return `${base} bg-white text-gray-950 shadow-sm dark:bg-gray-800 dark:text-white`;
	return `${base} text-gray-500 hover:bg-gray-200 hover:text-gray-900 dark:text-gray-400 dark:hover:bg-gray-800 dark:hover:text-white`;
});

const itemBase =
	"flex cursor-pointer items-center gap-2 rounded-lg px-3 py-2 text-sm outline-none data-[disabled]:cursor-default data-[disabled]:opacity-40";
const itemClass = `${itemBase} text-gray-700 data-[highlighted]:bg-gray-100 dark:text-gray-200 dark:data-[highlighted]:bg-gray-800`;
const dangerClass = `${itemBase} text-red-600 data-[highlighted]:bg-red-50 dark:text-red-400 dark:data-[highlighted]:bg-red-950/40`;
</script>
