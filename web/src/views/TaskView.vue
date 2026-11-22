<template>
	<AppLayout>
		<div class="flex h-full min-h-0 flex-col">
			<!-- One task: the session its runs share, with its settings behind the gear. -->
			<header v-if="task" data-testid="task-header"
				class="flex flex-wrap items-center gap-3 border-b border-gray-200 px-4 py-3 sm:px-6 dark:border-gray-800">
				<span class="inline-flex h-7 w-7 shrink-0 items-center justify-center rounded-lg bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-300">
					<ClockIcon aria-hidden="true" class="h-4 w-4" />
				</span>
				<div class="min-w-0">
					<h2 class="truncate text-lg font-bold text-gray-900 dark:text-white" data-testid="task-title">{{ task.name }}</h2>
					<p class="text-xs text-gray-500 dark:text-gray-400">
						{{ typeLabel }}<template v-if="task.enabled === false"> · disabled</template>
					</p>
				</div>
				<div class="ml-auto flex items-center gap-2">
					<router-link :to="agentTaskPath(agent, task.name)" data-testid="task-settings-link"
						aria-label="Task settings" title="Task settings"
						class="rounded-lg border border-gray-200 p-1.5 text-gray-700 hover:bg-gray-50 dark:border-gray-700 dark:text-gray-200 dark:hover:bg-gray-800">
						<Cog6ToothIcon aria-hidden="true" class="h-5 w-5" />
					</router-link>
				</div>
			</header>

			<div v-if="task" class="min-h-0 flex-1">
				<ChatView v-if="sessionId" :key="sessionId" :agent="agent" :session="sessionId" data-testid="task-chat" />
				<p v-else-if="error" role="alert" class="px-6 py-4 text-sm text-red-600 dark:text-red-400">{{ error }}</p>
			</div>
			<div v-else class="flex h-full flex-col items-center justify-center gap-3 px-6 text-center text-sm text-gray-500 dark:text-gray-400"
				data-testid="task-missing">
				<p>No task named <span class="font-medium text-gray-900 dark:text-white">{{ taskParam }}</span>.</p>
				<router-link :to="agentTaskPath(agent)"
					class="rounded-lg border border-gray-200 px-3 py-1.5 text-gray-700 hover:bg-gray-50 dark:border-gray-700 dark:text-gray-200 dark:hover:bg-gray-800">
					All scheduled tasks
				</router-link>
			</div>
		</div>
	</AppLayout>
</template>

<script setup lang="ts">
import ClockIcon from "@heroicons/vue/24/outline/ClockIcon";
import Cog6ToothIcon from "@heroicons/vue/24/outline/Cog6ToothIcon";
import { computed, ref, watch } from "vue";
import { useRoute } from "vue-router";
import AppLayout from "../components/AppLayout.vue";
import { useMCP } from "../composables/useMCP";
import {
	agentTaskPath,
	agentTaskType,
	useSettingsStore,
} from "../stores/settings";
import ChatView from "./ChatView.vue";

const route = useRoute();
const settings = useSettingsStore();
const { callTool } = useMCP();

const agent = computed(() =>
	typeof route.params.agent === "string" ? route.params.agent : "",
);
const taskParam = computed(() =>
	typeof route.params.task === "string" ? route.params.task : "",
);

const task = computed(() =>
	settings.config?.agents
		.find((a) => a.name === agent.value)
		?.tasks.find((t) => t.name === taskParam.value),
);

const TYPE_LABELS = {
	prompt: "Prompt task",
	script: "Script task",
	project: "Project task",
} as const;
const typeLabel = computed(() => TYPE_LABELS[agentTaskType(task.value?.type)]);

const sessionId = ref("");
const error = ref("");

// The task's session is named after it; a rename moves the session along.
async function loadSession() {
	sessionId.value = "";
	error.value = "";
	const t = task.value;
	if (!t) return;
	try {
		const raw = await callTool("task_session", {
			agent: agent.value,
			name: t.name,
		});
		sessionId.value = (JSON.parse(raw) as { id: string }).id;
	} catch (e) {
		error.value = e instanceof Error ? e.message : String(e);
	}
}

watch(
	() => [agent.value, task.value?.name],
	() => void loadSession(),
	{ immediate: true },
);
</script>
