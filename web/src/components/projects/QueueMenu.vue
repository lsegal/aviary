<template>
	<DropdownMenuRoot>
		<DropdownMenuTrigger asChild>
			<button type="button" data-testid="queue-menu" title="Queue a job for this project"
				class="inline-flex items-center gap-1.5 rounded-lg border border-gray-200 px-2.5 py-1.5 text-sm text-gray-700 hover:bg-gray-50 dark:border-gray-700 dark:text-gray-200 dark:hover:bg-gray-800">
				<span data-testid="queue-menu-plus" aria-hidden="true"
					class="inline-flex h-5 w-5 items-center justify-center rounded-md bg-blue-600 text-white">
					<PlusIcon class="h-3.5 w-3.5" />
				</span>
				Queue
			</button>
		</DropdownMenuTrigger>
		<DropdownMenuPortal>
			<DropdownMenuContent align="end" :side-offset="6" data-testid="queue-menu-items"
				class="z-50 min-w-56 rounded-xl border border-gray-200 bg-white p-1 shadow-lg dark:border-gray-700 dark:bg-gray-900">
				<DropdownMenuItem v-for="option in options" :key="option.skill" :disabled="option.disabled"
					:data-testid="`queue-${option.skill}`" :title="option.title" :class="itemClass"
					@select="emit('pick', option.skill)">
					<component :is="option.icon" aria-hidden="true" class="mt-0.5 h-4 w-4 shrink-0" />
					<span class="flex flex-col">
						<span>{{ option.label }}</span>
						<span class="font-mono text-xs text-gray-500 dark:text-gray-400">{{ option.disabled ? "off: no_merge is set" : option.skill }}</span>
					</span>
				</DropdownMenuItem>
			</DropdownMenuContent>
		</DropdownMenuPortal>
	</DropdownMenuRoot>
</template>

<script setup lang="ts">
// The project header's "+ Queue" menu (#1095): fix an issue with gh-fix,
// safely merge pull requests with gh-safe-merge, or answer a Discussion with
// gh-discuss. Each choice opens that skill's picker. Its "+" is the
// sidebar's blue "+" (#1197).
import ArrowsRightLeftIcon from "@heroicons/vue/24/outline/ArrowsRightLeftIcon";
import ChatBubbleLeftEllipsisIcon from "@heroicons/vue/24/outline/ChatBubbleLeftEllipsisIcon";
import PlusIcon from "@heroicons/vue/16/solid/PlusIcon";
import WrenchScrewdriverIcon from "@heroicons/vue/24/outline/WrenchScrewdriverIcon";
import {
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuPortal,
	DropdownMenuRoot,
	DropdownMenuTrigger,
} from "radix-vue";
import { computed } from "vue";
import type { QueueSkill } from "../../lib/projects";

const props = defineProps<{
	/** no_merge leaves pull requests for a human, so safe merge is off. */
	noMerge?: boolean;
}>();
const emit = defineEmits<{ pick: [skill: QueueSkill] }>();

const itemClass =
	"flex cursor-pointer items-start gap-2 rounded-lg px-3 py-2 text-sm text-gray-700 outline-none data-[disabled]:cursor-not-allowed data-[highlighted]:bg-gray-100 data-[disabled]:opacity-50 dark:text-gray-200 dark:data-[highlighted]:bg-gray-800";

const options = computed(() => [
	{
		skill: "gh-fix" as const,
		label: "Fix issue",
		icon: WrenchScrewdriverIcon,
		disabled: false,
		title: "Have gh-fix fix issues",
	},
	{
		skill: "gh-safe-merge" as const,
		label: "Safe merge PR",
		icon: ArrowsRightLeftIcon,
		disabled: props.noMerge,
		title: props.noMerge
			? "Safe merge is off: the project's no_merge setting leaves pull requests for a human to merge"
			: "Queue pull requests for gh-safe-merge; they merge one at a time in the order picked",
	},
	{
		skill: "gh-discuss" as const,
		label: "Discuss",
		icon: ChatBubbleLeftEllipsisIcon,
		disabled: false,
		title: "Queue Discussion threads for gh-discuss",
	},
]);
</script>
