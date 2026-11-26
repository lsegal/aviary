<template>
	<!-- Add and Edit for an agent's tasks and files, in a modal. -->
	<DialogRoot :open="formOpen" @update:open="(v) => { if (!v && !busy) close() }">
		<DialogPortal>
			<DialogOverlay class="fixed inset-0 z-50 bg-black/50" />
			<DialogContent :data-testid="`${dialog?.kind}-dialog`" :class="wide ? 'max-w-5xl' : 'max-w-2xl'"
				class="fixed left-1/2 top-1/2 z-50 max-h-[90dvh] w-[calc(100%-1.5rem)] -translate-x-1/2 -translate-y-1/2 overflow-y-auto rounded-xl bg-white p-6 shadow-2xl dark:bg-gray-900">
				<form @submit.prevent="submit">
					<DialogTitle class="text-base font-bold text-gray-900 dark:text-white">{{ title }}</DialogTitle>
					<DialogDescription class="mt-1 text-sm text-gray-600 dark:text-gray-400">{{ description }}</DialogDescription>

					<label for="agent-item-name" class="field-label mt-4">{{ dialog?.kind === "file" ? "File name" : "Task name" }}</label>
					<input id="agent-item-name" v-model="form.name" type="text" autocomplete="off" spellcheck="false"
						class="field-input font-mono" :placeholder="dialog?.kind === 'file' ? 'IDENTITY.md' : 'daily-briefing'" />

					<template v-if="dialog?.kind === 'task'">
						<div class="mt-3 grid gap-3 sm:grid-cols-2">
							<div>
								<span class="field-label">Type</span>
								<SegmentedControl v-model="form.type" label="Type" :options="typeOptions" />
							</div>
							<div>
								<span class="field-label">Target</span>
								<SegmentedControl v-model="form.targetKind" label="Target" :options="targetOptions" />
							</div>
						</div>
						<template v-if="form.targetKind === 'project'">
							<label for="agent-task-project" class="field-label mt-3">Project</label>
							<AppCombobox id="agent-task-project" v-model="form.project" data-testid="task-project"
								aria-describedby="agent-task-project-help" placeholder="Select a watched project…"
								empty-text="No matching projects"
								:options="projectChoices.map((p) => ({ value: p.target, label: p.target }))" />
							<p id="agent-task-project-help" class="mt-1 text-xs text-gray-500 dark:text-gray-400">
								{{ projectChoices.length ? "Runs in an isolated clone of the project, under the agent that owns it." : "No watched projects yet." }}
							</p>
						</template>
						<template v-else>
							<label for="agent-task-agent" class="field-label mt-3">Agent</label>
							<AppSelect id="agent-task-agent" v-model="form.agent" :disabled="dialog.mode !== 'create'"
								:options="agentChoices.map((a) => ({ value: a, label: a }))" />
						</template>
						<div class="mt-3">
							<TriggerEditor :key="editorKey" v-model="form.trigger" :preview="previewTrigger" :optional="form.targetKind !== 'project'" />
						</div>
						<TaskPromptSection v-model:prompt="form.prompt" v-model:precompile="form.precompute" id-prefix="agent-task"
							class="mt-3" :agent="feedbackAgent" :type="taskType" :target="form.target" :project="form.project"
							:trigger="form.trigger" :show-precompile="showPrecompute" :saved-task="savedTask"
							:task-agent="dialog.agent" />
						<label for="agent-task-send-to" class="field-label mt-3">Send To</label>
						<AppCombobox id="agent-task-send-to" v-model="form.target" data-testid="task-send-to"
							aria-describedby="agent-task-send-to-help" :options="sendToOptions"
							empty-text="No matching channels" />
						<p id="agent-task-send-to-help" class="mt-1 text-xs text-gray-500 dark:text-gray-400">
							Runs always show their output in the task's own session; a channel also gets a copy in its conversation.
						</p>
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

	<ConfirmDialog :open="deleteOpen" :title="`Delete ${dialog?.kind}?`" :action="confirmDelete" confirm-label="Delete"
		busy-label="Deleting…" test-id="delete-item-dialog" @update:open="(v) => { if (!v) close() }">
		<p>
			This will permanently delete
			<span class="break-all font-medium text-gray-900 dark:text-white">{{ dialog?.name }}</span>.
			This cannot be undone.
		</p>
		<DeleteDataOption v-if="dialog?.kind === 'task'" v-model="deleteData" kind="task" :agent="dialog.agent"
			:name="deleteOpen ? `${dialog.agent}/${dialog.name}` : ''" test-id="task-delete-data" />
	</ConfirmDialog>
</template>

<script setup lang="ts">
import {
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
import { useMCP } from "../../composables/useMCP";
import { useCompiledPanelOpen } from "../../composables/useTaskCompiles";
import { useTriggerPreview } from "../../composables/useTriggerPreview";
import { channelName, channelTarget } from "../../lib/channels";
import { projectTargets } from "../../lib/projectSettings";
import {
	applyTaskTrigger,
	type TriggerFields,
	taskTriggerFields,
} from "../../lib/trigger";
import { agentFilePath, useAgentFilesStore } from "../../stores/agentFiles";
import {
	type AgentTask,
	type AgentTaskType,
	agentTaskPath,
	agentTaskType,
	precompilesBesidePrompt,
	useSettingsStore,
} from "../../stores/settings";
import TaskPromptSection from "../TaskPromptSection.vue";
import TriggerEditor from "../TriggerEditor.vue";
import ConfirmDialog from "../ui/ConfirmDialog.vue";
import DeleteDataOption from "../ui/DeleteDataOption.vue";
import SegmentedControl, {
	type SegmentedOption,
} from "../ui/SegmentedControl.vue";
import { AppCombobox, AppSelect, type SelectOption } from "../ui/select";

type TaskKind = "prompt" | "script";
type TargetKind = "agent" | "project";

const { dialog, close } = useAgentItemDialogs();
const settings = useSettingsStore();
const files = useAgentFilesStore();
const route = useRoute();
const router = useRouter();
const { callTool } = useMCP();
const previewTrigger = useTriggerPreview();

const busy = ref(false);
const error = ref("");
/** Whether deleting a task deletes its session and run history too. */
const deleteData = ref(true);
const form = reactive({
	name: "",
	trigger: taskTriggerFields({ name: "" }),
	prompt: "",
	type: "prompt" as TaskKind,
	/** Whether the task runs under an agent or against a watched project. */
	targetKind: "agent" as TargetKind,
	agent: "",
	project: "",
	precompute: true,
	/** Where runs forward their output: "" or channel:<name>. */
	target: "",
});
// Remounts the trigger editor for each dialog, so its preview starts afresh.
const editorKey = ref(0);

const typeOptions = computed((): SegmentedOption<TaskKind>[] => [
	{ value: "prompt", label: "Prompt" },
	{
		value: "script",
		label: "Script",
		disabled: form.targetKind === "project",
		disabledReason: "Project tasks run a prompt as a Code Loop job",
	},
]);
const targetOptions: SegmentedOption<TargetKind>[] = [
	{ value: "agent", label: "Agent" },
	{ value: "project", label: "Project" },
];

/** The agents a task may be created under, the dialog's agent first. */
const agentChoices = computed(() => {
	const d = dialog.value;
	if (!d) return [];
	const names = (settings.config?.agents ?? []).map((a) => a.name);
	return [...new Set([d.agent, ...names].filter(Boolean))];
});

/**
 * The watched projects a task may target, each with the agent that owns it
 * and runs its tasks. A new task may target any agent's project; an edited
 * one stays under its agent, so only that agent's projects are offered.
 */
const projectChoices = computed(() => {
	const d = dialog.value;
	if (d?.kind !== "task") return [];
	const owners = new Map<string, string>();
	// The board's Schedule button names a project its agent runs, owned or not.
	if (d.mode === "create" && d.project) owners.set(d.project, d.agent);
	for (const a of settings.config?.agents ?? []) {
		if (d.mode !== "create" && a.name !== d.agent) continue;
		for (const target of projectTargets(a.projects))
			if (!owners.has(target)) owners.set(target, a.name);
	}
	// An edited task keeps a project its agent no longer watches.
	const current = d.mode === "edit" ? form.project : "";
	if (current && !owners.has(current)) owners.set(current, d.agent);
	return [...owners].map(([target, agent]) => ({ target, agent }));
});

/** The agent the task belongs to: the project's owner for a project task. */
const taskAgent = computed(() => {
	const d = dialog.value;
	if (d?.kind !== "task" || d.mode === "delete") return "";
	if (d.mode !== "create") return d.agent;
	if (form.targetKind === "agent") return form.agent || d.agent;
	return (
		projectChoices.value.find((p) => p.target === form.project)?.agent ?? ""
	);
});

/** The task's saved type for the dialog's Type and Target. */
const taskType = computed(
	(): AgentTaskType => (form.targetKind === "project" ? "project" : form.type),
);

/**
 * A prompt or project task keeps its prompt and its precompile setting,
 * which every save can change, and runs its compiled script beside the
 * prompt, falling back to the model if the script fails.
 */
const canPrecompute = computed(() =>
	precompilesBesidePrompt({ type: taskType.value }),
);
// Creating shows the choice for every type, disabled for a script.
const showPrecompute = computed(
	() => dialog.value?.mode === "create" || canPrecompute.value,
);
/**
 * The precompile choice a task opens with: a saved task's own, where unset
 * is off as it is when the task runs, else the default.
 */
function initialPrecompute(task?: AgentTask): boolean {
	if (task && canPrecompute.value) return task.precompile === true;
	return (
		canPrecompute.value &&
		settings.config?.scheduler?.precompute_tasks !== false
	);
}
watch(canPrecompute, (can) => {
	form.precompute = can && initialPrecompute(editedTask());
});

// A project task runs a prompt on a time trigger, so choosing Project makes
// a script a prompt and gives a task with no trigger the daily default.
watch(
	() => form.targetKind,
	(kind) => {
		if (kind !== "project") return;
		form.type = "prompt";
		if (!form.trigger.trigger)
			form.trigger = { ...form.trigger, trigger: "daily" };
		if (!form.project) form.project = projectChoices.value[0]?.target ?? "";
	},
);

/** An edited task's delivery target that is not a channel offered. */
const otherTarget = computed(() => {
	const t = form.target;
	if (!t || taskChannels.value.some((c) => channelTarget(channelName(c)) === t))
		return "";
	return dialog.value?.mode === "edit" ? t : "";
});

/** The task agent's channels, offered by name as Send To. */
const taskChannels = computed(
	() =>
		settings.config?.agents
			.find((a) => a.name === taskAgent.value)
			?.channels?.filter((c) => c.type && c.id) ?? [],
);

/** The agent whose model and tools the task's feedback is analyzed with. */
const feedbackAgent = computed(
	() => taskAgent.value || dialog.value?.agent || "",
);
const sendToOptions = computed<SelectOption[]>(() => [
	{ value: "", label: "Task session only" },
	...taskChannels.value.map((c) => ({
		value: channelTarget(channelName(c)),
		label: `${channelName(c)} · ${c.type}`,
	})),
	...(otherTarget.value
		? [{ value: otherTarget.value, label: otherTarget.value }]
		: []),
]);

// Another agent clears a Send To that named one of the old agent's channels.
watch(taskAgent, (agent, prev) => {
	if (prev && agent !== prev) form.target = "";
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
		return form.targetKind === "project"
			? "Add project scheduled task"
			: "Add task";
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
		return `Renames ${d.name} or changes its type, target, trigger and ${form.type}.`;
	if (form.targetKind === "project")
		return `A scheduled task that runs its prompt against ${form.project || "a watched project"}. It is listed in Scheduled Tasks, and its box on the project's board edits the rest of its settings.`;
	return `A scheduled task for ${taskAgent.value}. The Scheduled Tasks page edits the rest of its settings.`;
});

function agentTasks(agent: string): AgentTask[] {
	return settings.config?.agents.find((a) => a.name === agent)?.tasks ?? [];
}

watch(dialog, (d) => {
	error.value = "";
	deleteData.value = true;
	busy.value = false;
	const task =
		d?.kind === "task" && d.name
			? agentTasks(d.agent).find((t) => t.name === d.name)
			: undefined;
	const type = task ? agentTaskType(task.type) : "prompt";
	const project = task ? type === "project" : !!d?.project;
	form.name = d?.mode === "edit" ? (d.name ?? "") : "";
	form.trigger = task ? taskTriggerFields(task) : newTaskTrigger(project);
	editorKey.value++;
	form.prompt = task?.prompt ?? "";
	form.type = type === "script" ? "script" : "prompt";
	form.targetKind = project ? "project" : "agent";
	form.agent = d?.agent ?? "";
	form.project = task ? (task.project ?? "") : (d?.project ?? "");
	form.precompute = initialPrecompute(task);
	form.target = task?.target ?? "";
});

/**
 * A new task's trigger: daily at 09:00 for a project task, which needs a
 * time trigger, and none for an agent task until one is picked.
 */
function newTaskTrigger(project: boolean): TriggerFields {
	const fields = taskTriggerFields({ name: "" });
	return project ? { ...fields, trigger: "daily" } : fields;
}

/** The task with the dialog's trigger fields written onto it. */
function withTrigger(task: AgentTask, trigger: TriggerFields): AgentTask {
	const next = { ...task };
	applyTaskTrigger(next, trigger);
	return next;
}

/** Whether the route shows this agent's task or file. */
function showing(kind: "task" | "file", agent: string, name: string): boolean {
	const path =
		kind === "task" ? agentTaskPath(agent).path : agentFilePath(agent).path;
	return route.path === path && route.query[kind] === name;
}

/**
 * The task with the dialog's type, target, prompt, Send To and trigger
 * written onto it. Leaving Project drops the project-only fields, which the
 * config rejects on other types.
 */
function withForm(task: AgentTask, trigger: TriggerFields): AgentTask {
	const next: AgentTask = {
		...task,
		type: taskType.value,
		prompt: form.prompt,
		target: form.target,
	};
	if (taskType.value === "project") {
		next.project = form.project;
	} else {
		delete next.project;
		delete next.model;
	}
	// Unset is off, so an unchanged task saves unchanged.
	if (canPrecompute.value && form.precompute) {
		// Turning it on, including after Aviary turned it off, compiles anew.
		if (task.precompile !== true) dropCompiled(next);
		next.precompile = true;
	} else {
		delete next.precompile;
		dropCompiled(next);
	}
	return withTrigger(next, trigger);
}

/** Drops a task's compiled script and what Aviary tracks about it. */
function dropCompiled(task: AgentTask) {
	delete task.compiled;
	delete task.compiled_key;
	delete task.script_verified;
	delete task.script_failures;
	delete task.precompile_note;
}

async function submitTask(mode: "create" | "edit", from = "") {
	const onCreated = dialog.value?.onCreated;
	const agent = taskAgent.value;
	if (taskType.value === "project" && !form.project)
		throw new Error("Choose a watched project.");
	if (!agent) throw new Error("Choose an agent.");
	const name = form.name.trim();
	const clash = agentTasks(agent).some(
		(t) => t.name === name && t.name !== from,
	);
	if (clash) throw new Error(`A task named ${name} already exists.`);
	const trigger = { ...form.trigger };
	if (mode === "create") {
		const task = withForm(
			{
				enabled: true,
				name,
				watch: "",
				run_once: false,
				from_file: true,
			},
			trigger,
		);
		await settings.saveAgentTasks(agent, (tasks) => [...tasks, task]);
		// Precompile compiles a script beside the prompt in the background;
		// the task already exists, so a failure to start it does not fail
		// creation.
		if (canPrecompute.value && form.precompute && form.prompt.trim()) {
			await callTool("config_task_convert_to_script", {
				agent,
				task: name,
			}).catch((e) => console.error("precompute task", e));
		}
		if (onCreated) onCreated(agent, name);
		else await router.push(agentTaskPath(agent, name));
		return;
	}
	const current = agentTasks(agent).find((t) => t.name === from);
	if (!current) throw new Error(`Task ${from} no longer exists.`);
	if (JSON.stringify(withForm(current, trigger)) !== JSON.stringify(current)) {
		await settings.saveAgentTasks(agent, (tasks) =>
			tasks.map((t) => (t.name === from ? withForm(t, trigger) : t)),
		);
	}
	if (name !== from) {
		const wasShowing = showing("task", agent, from);
		await settings.renameTask(agent, from, name);
		if (wasShowing) await router.replace(agentTaskPath(agent, name));
	}
	// A precompiled task compiles again in the background when its prompt,
	// project or tools changed; its runs compile first otherwise.
	if (canPrecompute.value && form.precompute && form.prompt.trim()) {
		await callTool("config_task_convert_to_script", {
			agent,
			task: name,
		}).catch((e) => console.error("precompute task", e));
	}
}

/** The saved task the open dialog edits, if any. */
/** The edited task as saved, live from the config, for its compiled script panel. */
const savedTask = computed(() => editedTask());
// The dialog widens while the compiled script shows beside the prompt.
const compiledPanelOpen = useCompiledPanelOpen();
const wide = computed(
	() =>
		compiledPanelOpen.value &&
		!!savedTask.value &&
		precompilesBesidePrompt(savedTask.value) &&
		(savedTask.value.precompile === true || !!savedTask.value.precompile_note),
);

function editedTask(): AgentTask | undefined {
	const d = dialog.value;
	if (d?.kind !== "task" || d.mode !== "edit" || !d.name) return undefined;
	return agentTasks(d.agent).find((t) => t.name === d.name);
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
		if (d.kind === "task") await submitTask(d.mode, d.name);
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
	if (!d?.name) return;
	const name = d.name;
	const wasShowing = showing(d.kind, d.agent, name);
	if (d.kind === "task") {
		await settings.saveAgentTasks(
			d.agent,
			(tasks) => tasks.filter((t) => t.name !== name),
			deleteData.value ? [{ kind: "task", agent: d.agent, name }] : [],
		);
	} else {
		await files.remove(d.agent, name);
	}
	close();
	if (wasShowing)
		await router.replace(
			d.kind === "task" ? agentTaskPath(d.agent) : agentFilePath(d.agent),
		);
}
</script>
