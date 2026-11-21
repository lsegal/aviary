<template>
	<!-- Add and Edit for an agent's tasks and files, in a modal. -->
	<DialogRoot :open="formOpen" @update:open="(v) => { if (!v && !busy) close() }">
		<DialogPortal>
			<DialogOverlay class="fixed inset-0 z-50 bg-black/50" />
			<DialogContent :data-testid="`${dialog?.kind}-dialog`"
				class="fixed left-1/2 top-1/2 z-50 max-h-[90dvh] w-[calc(100%-1.5rem)] max-w-lg -translate-x-1/2 -translate-y-1/2 overflow-y-auto rounded-xl bg-white p-6 shadow-2xl dark:bg-gray-900">
				<form @submit.prevent="submit">
					<DialogTitle class="text-base font-bold text-gray-900 dark:text-white">{{ title }}</DialogTitle>
					<DialogDescription class="mt-1 text-sm text-gray-600 dark:text-gray-400">{{ description }}</DialogDescription>

					<label for="agent-item-name" class="field-label mt-4">{{ dialog?.kind === "file" ? "File name" : "Task name" }}</label>
					<input id="agent-item-name" v-model="form.name" type="text" autocomplete="off" spellcheck="false"
						class="field-input font-mono" :placeholder="dialog?.kind === 'file' ? 'IDENTITY.md' : 'daily-briefing'" />

					<template v-if="dialog?.kind === 'task'">
						<template v-if="dialog.project && dialog.mode === 'create'">
							<label for="agent-task-agent" class="field-label mt-3">Agent</label>
							<select id="agent-task-agent" v-model="form.agent" class="field-input">
								<option v-for="a in agentChoices" :key="a" :value="a">{{ a }}</option>
							</select>
						</template>
						<label for="agent-task-schedule" class="field-label mt-3">Schedule</label>
						<input id="agent-task-schedule" v-model="form.schedule" type="text" autocomplete="off"
							class="field-input" placeholder="*/5 * * * *" />
						<label for="agent-task-prompt" class="field-label mt-3">{{ form.type === "script" ? "Script" : "Prompt" }}</label>
						<textarea id="agent-task-prompt" v-model="form.prompt" rows="6" class="field-input font-mono text-xs"
							:placeholder="form.type === 'script' ? 'print(\'hello from lua\')' : 'Task prompt...'" />
					</template>
					<p v-else class="mt-1 text-xs text-gray-400 dark:text-gray-500">
						Root-level <span class="font-mono">.md</span> only; <span class="font-mono">.md</span> is added when missing.
					</p>

					<p v-if="error" role="alert" class="mt-3 text-sm text-red-600 dark:text-red-400">{{ error }}</p>

					<div class="mt-6 flex justify-end gap-3">
						<DialogClose type="button" :disabled="busy"
							class="rounded-lg border border-gray-200 px-4 py-2 text-sm text-gray-600 hover:bg-gray-50 dark:border-gray-700 dark:text-gray-400 dark:hover:bg-gray-800">
							Cancel
						</DialogClose>
						<button type="submit" :disabled="busy || !form.name.trim()"
							class="rounded-lg bg-blue-600 px-4 py-2 text-sm font-semibold text-white hover:bg-blue-500 disabled:opacity-50">
							{{ dialog?.mode === "create" ? "Create" : "Save" }}
						</button>
					</div>
				</form>
			</DialogContent>
		</DialogPortal>
	</DialogRoot>

	<AlertDialogRoot :open="deleteOpen" @update:open="(v) => { if (!v && !busy) close() }">
		<AlertDialogPortal>
			<AlertDialogOverlay class="fixed inset-0 z-50 bg-black/50" />
			<AlertDialogContent
				class="fixed left-1/2 top-1/2 z-50 w-[calc(100%-1.5rem)] max-w-lg -translate-x-1/2 -translate-y-1/2 rounded-xl bg-white p-6 shadow-2xl dark:bg-gray-900">
				<AlertDialogTitle class="text-base font-bold text-gray-900 dark:text-white">
					Delete {{ dialog?.kind }}?
				</AlertDialogTitle>
				<AlertDialogDescription class="mt-2 text-sm text-gray-600 dark:text-gray-400">
					This will permanently delete
					<span class="break-all font-medium text-gray-900 dark:text-white">{{ dialog?.name }}</span>.
					This cannot be undone.
				</AlertDialogDescription>
				<p v-if="error" role="alert" class="mt-3 text-sm text-red-600 dark:text-red-400">{{ error }}</p>
				<div class="mt-6 flex justify-end gap-3">
					<AlertDialogCancel :disabled="busy"
						class="rounded-lg border border-gray-200 px-4 py-2 text-sm text-gray-600 hover:bg-gray-50 dark:border-gray-700 dark:text-gray-400 dark:hover:bg-gray-800">
						Cancel
					</AlertDialogCancel>
					<button type="button" :disabled="busy"
						class="rounded-lg bg-red-600 px-4 py-2 text-sm font-semibold text-white hover:bg-red-500 disabled:opacity-50"
						@click="confirmDelete">
						Delete
					</button>
				</div>
			</AlertDialogContent>
		</AlertDialogPortal>
	</AlertDialogRoot>
</template>

<script setup lang="ts">
import {
	AlertDialogCancel,
	AlertDialogContent,
	AlertDialogDescription,
	AlertDialogOverlay,
	AlertDialogPortal,
	AlertDialogRoot,
	AlertDialogTitle,
	DialogClose,
	DialogContent,
	DialogDescription,
	DialogOverlay,
	DialogPortal,
	DialogRoot,
	DialogTitle,
} from "radix-vue";
import { computed, reactive, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useAgentItemDialogs } from "../../composables/useAgentItemDialogs";
import { agentFilePath, useAgentFilesStore } from "../../stores/agentFiles";
import {
	type AgentTask,
	agentTaskPath,
	agentTaskType,
	useSettingsStore,
} from "../../stores/settings";

const { dialog, close } = useAgentItemDialogs();
const settings = useSettingsStore();
const files = useAgentFilesStore();
const route = useRoute();
const router = useRouter();

const busy = ref(false);
const error = ref("");
const form = reactive({
	name: "",
	schedule: "",
	prompt: "",
	type: "prompt",
	agent: "",
});

/** The agents a project task may be created under, its default first. */
const agentChoices = computed(() => {
	const d = dialog.value;
	if (!d) return [];
	return [...new Set([d.agent, ...(d.agents ?? [])].filter(Boolean))];
});

const formOpen = computed(
	() => !!dialog.value && dialog.value.mode !== "delete",
);
const deleteOpen = computed(() => dialog.value?.mode === "delete");

const title = computed(() => {
	const d = dialog.value;
	if (!d) return "";
	if (d.mode === "create") {
		if (d.kind === "file") return "Add file";
		return d.project ? "Add project scheduled task" : "Add task";
	}
	return d.kind === "task" ? "Edit task" : "Rename file";
});

const description = computed(() => {
	const d = dialog.value;
	if (!d) return "";
	if (d.kind === "file")
		return d.mode === "create"
			? `A new markdown file in ${d.agent}'s directory.`
			: `Renames ${d.name}, keeping its content.`;
	if (d.mode !== "create")
		return `Renames ${d.name} or changes its schedule and prompt.`;
	return d.project
		? `A scheduled task that runs its prompt against ${d.project}. It is listed in Scheduled Tasks, and this project's Schedules section edits the rest of its settings.`
		: `A scheduled task for ${d.agent}. The Scheduled Tasks page edits the rest of its settings.`;
});

function agentTasks(agent: string): AgentTask[] {
	return settings.config?.agents.find((a) => a.name === agent)?.tasks ?? [];
}

watch(dialog, (d) => {
	error.value = "";
	busy.value = false;
	const task =
		d?.kind === "task" && d.name
			? agentTasks(d.agent).find((t) => t.name === d.name)
			: undefined;
	form.name = d?.mode === "edit" ? (d.name ?? "") : "";
	form.schedule = task?.schedule ?? "";
	form.prompt = task?.prompt ?? "";
	form.type = agentTaskType(task?.type);
	form.agent = d?.agent ?? "";
});

/** Whether the route shows this agent's task or file. */
function showing(kind: "task" | "file", agent: string, name: string): boolean {
	const path =
		kind === "task" ? agentTaskPath(agent).path : agentFilePath(agent).path;
	return route.path === path && route.query[kind] === name;
}

async function submitTask(agent: string, mode: "create" | "edit", from = "") {
	const project = mode === "create" ? dialog.value?.project : undefined;
	const onCreated = dialog.value?.onCreated;
	if (project) agent = form.agent || agent;
	const name = form.name.trim();
	const clash = agentTasks(agent).some(
		(t) => t.name === name && t.name !== from,
	);
	if (clash) throw new Error(`A task named ${name} already exists.`);
	const fields = { schedule: form.schedule.trim(), prompt: form.prompt };
	if (mode === "create") {
		await settings.saveAgentTasks(agent, (tasks) => [
			...tasks,
			{
				enabled: true,
				name,
				type: project ? "project" : "prompt",
				...(project ? { project } : {}),
				watch: "",
				target: "",
				run_once: false,
				from_file: true,
				...fields,
			},
		]);
		if (onCreated) onCreated(agent, name);
		else await router.push(agentTaskPath(agent, name));
		return;
	}
	const current = agentTasks(agent).find((t) => t.name === from);
	if (!current) throw new Error(`Task ${from} no longer exists.`);
	if (
		current.schedule !== fields.schedule ||
		current.prompt !== fields.prompt
	) {
		await settings.saveAgentTasks(agent, (tasks) =>
			tasks.map((t) => (t.name === from ? { ...t, ...fields } : t)),
		);
	}
	if (name !== from) {
		const wasShowing = showing("task", agent, from);
		await settings.renameTask(agent, from, name);
		if (wasShowing) await router.replace(agentTaskPath(agent, name));
	}
}

async function submitFile(agent: string, mode: "create" | "edit", from = "") {
	if (mode === "create") {
		const file = await files.create(agent, form.name);
		await router.push(agentFilePath(agent, file));
		return;
	}
	const wasShowing = showing("file", agent, from);
	const file = await files.rename(agent, from, form.name);
	if (wasShowing && file !== from)
		await router.replace(agentFilePath(agent, file));
}

async function submit() {
	const d = dialog.value;
	if (!d || d.mode === "delete" || busy.value || !form.name.trim()) return;
	busy.value = true;
	error.value = "";
	try {
		if (d.kind === "task") await submitTask(d.agent, d.mode, d.name);
		else await submitFile(d.agent, d.mode, d.name);
		close();
	} catch (e) {
		error.value = e instanceof Error ? e.message : String(e);
	} finally {
		busy.value = false;
	}
}

async function confirmDelete() {
	const d = dialog.value;
	if (!d?.name || busy.value) return;
	const name = d.name;
	busy.value = true;
	error.value = "";
	try {
		const wasShowing = showing(d.kind, d.agent, name);
		if (d.kind === "task") {
			await settings.saveAgentTasks(d.agent, (tasks) =>
				tasks.filter((t) => t.name !== name),
			);
		} else {
			await files.remove(d.agent, name);
		}
		close();
		if (wasShowing)
			await router.replace(
				d.kind === "task" ? agentTaskPath(d.agent) : agentFilePath(d.agent),
			);
	} catch (e) {
		error.value = e instanceof Error ? e.message : String(e);
	} finally {
		busy.value = false;
	}
}
</script>
