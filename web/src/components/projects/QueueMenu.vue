<template>
	<DropdownMenuRoot>
		<DropdownMenuTrigger asChild>
			<button type="button" data-testid="queue-menu" aria-label="Queue…" title="Queue…" :class="HEADER_ICON_CLS">
				<PlusIcon aria-hidden="true" class="h-5 w-5" />
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
// gh-discuss. Each choice opens that skill's picker. Its trigger is a neutral
// icon button like the header's refresh, logs, chat and settings (#1305).

import PlusIcon from "@heroicons/vue/24/outline/PlusIcon";
import {
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuPortal,
	DropdownMenuRoot,
	DropdownMenuTrigger,
} from "radix-vue";
import { computed } from "vue";
import { HEADER_ICON_CLS } from "../../lib/headerActions";
import type { QueueSkill } from "../../lib/projects";
import { queueOptions } from "./queueOptions";

const props = defineProps<{
	/** no_merge leaves pull requests for a human, so safe merge is off. */
	noMerge?: boolean;
}>();
const emit = defineEmits<{ pick: [skill: QueueSkill] }>();

const itemClass =
	"flex cursor-pointer items-start gap-2 rounded-lg px-3 py-2 text-sm text-gray-700 outline-none data-[disabled]:cursor-not-allowed data-[highlighted]:bg-gray-100 data-[disabled]:opacity-50 dark:text-gray-200 dark:data-[highlighted]:bg-gray-800";

const options = computed(() => queueOptions(props.noMerge));
</script>
