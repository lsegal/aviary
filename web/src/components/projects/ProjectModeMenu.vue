<template>
	<DropdownMenuRoot>
		<DropdownMenuTrigger asChild>
			<button type="button" data-testid="project-mode-menu" :aria-label="label" :title="label"
				:class="HEADER_ICON_CLS" :disabled="saving">
				<EyeSlashIcon v-if="manual" aria-hidden="true" class="h-5 w-5" />
				<EyeIcon v-else aria-hidden="true" class="h-5 w-5" />
			</button>
		</DropdownMenuTrigger>
		<DropdownMenuPortal>
			<DropdownMenuContent align="end" :side-offset="6" data-testid="project-mode-items"
				class="z-50 min-w-64 rounded-xl border border-gray-200 bg-white p-1 shadow-lg dark:border-gray-700 dark:bg-gray-900">
				<DropdownMenuLabel class="px-3 py-1.5 text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">
					Issue discovery
				</DropdownMenuLabel>
				<DropdownMenuRadioGroup :model-value="choice" @update:model-value="pick">
					<DropdownMenuRadioItem v-for="option in PROJECT_MODES" :key="option.value" :value="option.value"
						:data-testid="`project-mode-${option.value}`" :class="itemClass">
						<span class="mt-0.5 flex h-4 w-4 shrink-0 items-center justify-center">
							<DropdownMenuItemIndicator>
								<CheckIcon aria-hidden="true" class="h-4 w-4" />
							</DropdownMenuItemIndicator>
						</span>
						<span class="flex flex-col">
							<span>{{ option.label }}</span>
							<span class="text-xs text-gray-500 dark:text-gray-400">{{ option.hint }}</span>
						</span>
					</DropdownMenuRadioItem>
				</DropdownMenuRadioGroup>
				<p v-if="error" data-testid="project-mode-error" class="px-3 py-1.5 text-xs text-red-600 dark:text-red-400">{{ error }}</p>
			</DropdownMenuContent>
		</DropdownMenuPortal>
	</DropdownMenuRoot>
</template>

<script setup lang="ts">
// The project header's issue discovery menu (#1307): the eye button. On a
// narrow header it moves into the board's "…" menu instead (#1488); both
// share useProjectMode.

import CheckIcon from "@heroicons/vue/16/solid/CheckIcon";
import EyeIcon from "@heroicons/vue/24/outline/EyeIcon";
import EyeSlashIcon from "@heroicons/vue/24/outline/EyeSlashIcon";
import {
	DropdownMenuContent,
	DropdownMenuItemIndicator,
	DropdownMenuLabel,
	DropdownMenuPortal,
	DropdownMenuRadioGroup,
	DropdownMenuRadioItem,
	DropdownMenuRoot,
	DropdownMenuTrigger,
} from "radix-vue";
import { useProjectMode } from "../../composables/useProjectMode";
import { HEADER_ICON_CLS } from "../../lib/headerActions";
import {
	PROJECT_MODES,
	type ProjectMode,
	type ProjectModeChoice,
} from "../../lib/projects";

// manual is the effective mode; defaultMode is the global one.
const props = defineProps<{
	target: string;
	manual: boolean;
	defaultMode: ProjectMode;
}>();
const emit = defineEmits<{ changed: [mode: ProjectModeChoice] }>();

const { saving, error, choice, label, pick } = useProjectMode(props, (mode) =>
	emit("changed", mode),
);

const itemClass =
	"flex cursor-pointer items-start gap-2 rounded-lg px-3 py-2 text-sm text-gray-700 outline-none data-[highlighted]:bg-gray-100 dark:text-gray-200 dark:data-[highlighted]:bg-gray-800";
</script>
