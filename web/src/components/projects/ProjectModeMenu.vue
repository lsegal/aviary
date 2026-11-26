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
				<p data-testid="project-mode-source" class="px-3 pb-1 text-xs text-gray-500 dark:text-gray-400">
					{{ inherited ? `Inherited: ${current}` : `Overridden: ${current} (global: ${projectModeName(defaultMode)})` }}
				</p>
				<DropdownMenuRadioGroup :model-value="choice" @update:model-value="pick">
					<DropdownMenuRadioItem v-for="option in projectModeChoices(defaultMode)" :key="option.value" :value="option.value"
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
				<template v-if="!inherited">
					<DropdownMenuSeparator class="my-1 h-px bg-gray-200 dark:bg-gray-700" />
					<DropdownMenuItem data-testid="project-mode-reset" :class="itemClass" @select="pick('inherit')">
						<ArrowUturnLeftIcon aria-hidden="true" class="mt-0.5 h-4 w-4 shrink-0" />
						Reset to global
					</DropdownMenuItem>
				</template>
				<p v-if="error" data-testid="project-mode-error" class="px-3 py-1.5 text-xs text-red-600 dark:text-red-400">{{ error }}</p>
			</DropdownMenuContent>
		</DropdownMenuPortal>
	</DropdownMenuRoot>
</template>

<script setup lang="ts">
// The project header's issue discovery menu (#1307): Watch picks up issues
// matching the filter, Manual never searches for issues, so only work queued
// on demand, scheduled project tasks and agent tools start jobs. Inherit
// drops the project's own mode so it follows the global one (#1371). A
// change applies at once through project_update, to this project only.

import ArrowUturnLeftIcon from "@heroicons/vue/16/solid/ArrowUturnLeftIcon";
import CheckIcon from "@heroicons/vue/16/solid/CheckIcon";
import EyeIcon from "@heroicons/vue/24/outline/EyeIcon";
import EyeSlashIcon from "@heroicons/vue/24/outline/EyeSlashIcon";
import {
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuItemIndicator,
	DropdownMenuLabel,
	DropdownMenuPortal,
	DropdownMenuRadioGroup,
	DropdownMenuRadioItem,
	DropdownMenuRoot,
	DropdownMenuSeparator,
	DropdownMenuTrigger,
} from "radix-vue";
import { computed, ref } from "vue";
import { useMCP } from "../../composables/useMCP";
import { HEADER_ICON_CLS } from "../../lib/headerActions";
import {
	type ProjectMode,
	type ProjectModeChoice,
	projectModeChoices,
	projectModeName,
} from "../../lib/projects";

// manual is the effective mode; inherited says it comes from defaultMode,
// the global one, rather than the project's own override.
const props = defineProps<{
	target: string;
	manual: boolean;
	inherited: boolean;
	defaultMode: ProjectMode;
}>();
const emit = defineEmits<{ changed: [mode: ProjectModeChoice] }>();

const { callTool } = useMCP();
const saving = ref(false);
const error = ref("");

const current = computed(() =>
	projectModeName(props.manual ? "manual" : "watch"),
);
const choice = computed<ProjectModeChoice>(() =>
	props.inherited ? "inherit" : props.manual ? "manual" : "watch",
);
const label = computed(
	() =>
		`Issue discovery: ${current.value}${props.manual ? " (no issue watching)" : ""}${props.inherited ? ", inherited" : ""}`,
);

const itemClass =
	"flex cursor-pointer items-start gap-2 rounded-lg px-3 py-2 text-sm text-gray-700 outline-none data-[highlighted]:bg-gray-100 dark:text-gray-200 dark:data-[highlighted]:bg-gray-800";

async function pick(value: string) {
	const mode = value as ProjectModeChoice;
	if (mode === choice.value) return;
	saving.value = true;
	error.value = "";
	try {
		await callTool("project_update", { target: props.target, mode });
		emit("changed", mode);
	} catch (e) {
		error.value = e instanceof Error ? e.message : String(e);
	} finally {
		saving.value = false;
	}
}
</script>
