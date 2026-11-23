<template>
	<div class="space-y-4" data-testid="channel-workflows">
		<!-- The channel's workflows. -->
		<template v-if="!editing">
			<div class="flex flex-wrap items-center justify-between gap-2">
				<p class="text-sm text-gray-600 dark:text-gray-400">
					Workflows run when a matching message arrives on this channel, and replace the agent's own reply to it.
				</p>
				<button type="button" data-testid="workflow-add"
					class="rounded-lg bg-blue-600 px-3 py-1.5 text-sm font-semibold text-white hover:bg-blue-500"
					@click="startNew">Add workflow</button>
			</div>
			<p v-if="!workflows.length" data-testid="workflows-empty"
				class="rounded-lg border border-dashed border-gray-300 px-3 py-6 text-center text-sm text-gray-500 dark:border-gray-700 dark:text-gray-400">
				No workflows yet.
			</p>
			<ul v-else class="divide-y divide-gray-200 rounded-lg border border-gray-200 dark:divide-gray-800 dark:border-gray-800">
				<li v-for="w in workflows" :key="w.name" data-testid="workflow-row" :data-workflow="w.name"
					class="flex flex-wrap items-center gap-3 px-3 py-2">
					<TaskEnabledSwitch :task="w.name" :enabled="w.enabled !== false" :busy="pause.isPending(agent, w.name)"
						@update="pause.setTaskEnabled(agent, w.name, $event)" />
					<div class="min-w-0 flex-1">
						<div class="flex items-center gap-2">
							<router-link :to="taskSessionPath(agent, w.name)" data-testid="workflow-open"
								class="truncate text-sm font-semibold text-gray-900 hover:underline dark:text-white">{{ w.name }}</router-link>
							<PausedChip v-if="w.enabled === false" />
						</div>
						<p class="truncate text-xs text-gray-500 dark:text-gray-400">{{ describe(w) }}</p>
					</div>
					<TaskPrecompileBadge :task="w" />
					<button type="button" data-testid="workflow-edit" :class="SMALL_BTN" @click="startEdit(w)">Edit</button>
					<button type="button" data-testid="workflow-delete" class="danger-btn" @click="remove(w)">Delete</button>
				</li>
			</ul>
		</template>

		<!-- Add or edit one workflow: the task form, with a source entry and
			text match where the schedule would be. -->
		<form v-else data-testid="workflow-form" class="space-y-3" @submit.prevent="save">
			<div class="flex items-center justify-between">
				<h4 class="text-sm font-semibold text-gray-800 dark:text-gray-200">{{ original ? `Edit ${original}` : "New workflow" }}</h4>
				<label class="inline-flex items-center gap-2 text-sm text-gray-600 dark:text-gray-300">
					Enabled
					<TaskEnabledSwitch task="workflow" :enabled="form.enabled" :busy="false" @update="form.enabled = $event" />
				</label>
			</div>
			<div class="grid gap-3 lg:grid-cols-2">
				<div>
					<label class="field-label" for="workflow-name">Name</label>
					<input id="workflow-name" v-model="form.name" data-testid="workflow-name" type="text" class="field-input font-mono"
						placeholder="triage-crashes" autocomplete="off" />
				</div>
				<div>
					<label class="field-label">Type</label>
					<AppSelect v-model="form.type" aria-label="Type" :options="TYPE_OPTIONS" :disabled="!!original" />
				</div>
			</div>

			<fieldset class="space-y-3 rounded-lg border border-gray-200 p-3 dark:border-gray-800">
				<legend class="px-1 text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">Trigger</legend>
				<div class="grid gap-3 lg:grid-cols-2">
					<div>
						<label class="field-label">Source entry</label>
						<AppCombobox v-model="form.entry" aria-label="Source entry" data-testid="workflow-entry"
							placeholder="Every conversation" empty-text="No conversations yet" clearable
							:options="entryOptions" />
						<p class="mt-1 text-xs text-gray-400 dark:text-gray-500">
							The conversation to watch: a Slack channel, a Signal contact or group. Empty watches every conversation on the channel.
						</p>
					</div>
					<div>
						<label class="field-label" for="workflow-match">Text match</label>
						<input id="workflow-match" v-model="form.match" data-testid="workflow-match" type="text" class="field-input font-mono"
							placeholder="*" autocomplete="off" spellcheck="false" />
						<p class="mt-1 text-xs text-gray-400 dark:text-gray-500">
							A case-insensitive wildcard over the whole message: <code>*</code> matches anything, <code>?</code> one character.
							Use <code>*crash*</code> for messages containing "crash".
						</p>
					</div>
				</div>
				<p v-if="takesEverything" role="note" data-testid="workflow-takeover-hint"
					class="rounded-lg border border-amber-200 bg-amber-50 px-3 py-2 text-xs text-amber-800 dark:border-amber-900 dark:bg-amber-950/40 dark:text-amber-300">
					This workflow will handle all messages on {{ form.entry ? "this entry" : "this channel" }}; the agent won't reply on its own.
				</p>
				<div data-testid="workflow-preview">
					<div class="flex items-center justify-between">
						<span class="field-label">Would match</span>
						<span v-if="preview.length" class="text-xs text-gray-500 dark:text-gray-400" data-testid="workflow-preview-count">
							{{ preview.filter((m) => m.matches).length }} of {{ preview.length }} recent messages
						</span>
					</div>
					<p v-if="!preview.length" class="text-xs text-gray-500 dark:text-gray-400">No recent messages to test against.</p>
					<ul v-else class="max-h-48 space-y-1 overflow-y-auto text-xs">
						<li v-for="(m, i) in preview" :key="i" data-testid="workflow-preview-message" :data-matches="m.matches"
							class="flex gap-2 rounded px-2 py-1"
							:class="m.matches ? 'bg-green-50 text-green-900 dark:bg-green-950/40 dark:text-green-200' : 'text-gray-500 dark:text-gray-400'">
							<span aria-hidden="true">{{ m.matches ? "✓" : "·" }}</span>
							<span class="sr-only">{{ m.matches ? "matches" : "does not match" }}</span>
							<span class="min-w-0 truncate"><span class="font-semibold">{{ m.sender || "Unknown sender" }}:</span> {{ m.text }}</span>
						</li>
					</ul>
				</div>
			</fieldset>

			<div class="grid gap-3 lg:grid-cols-2">
				<div>
					<label class="field-label">Send Via</label>
					<AppSelect v-model="form.via" aria-label="Send Via" data-testid="workflow-send-via" :options="viaOptions" />
				</div>
			</div>
			<div v-if="form.via === 'session'" class="grid gap-3 lg:grid-cols-2">
				<div>
					<label class="field-label">Session</label>
					<SessionSelect v-model="form.session" :agent="agent" aria-label="Session" />
				</div>
			</div>
			<div class="grid gap-3 lg:grid-cols-2">
				<div>
					<label class="field-label">While a run is active</label>
					<AppSelect v-model="form.overlap" aria-label="Overlap" :options="OVERLAP_OPTIONS" />
				</div>
			</div>

			<TaskPromptSection v-model:prompt="form.content" v-model:precompile="form.precompile" v-model:model="form.model"
				id-prefix="workflow" prompt-test-id="workflow-content" :agent="agent" :type="form.type" :target="target()"
				trigger-type="message" :rows="10" prompt-class="min-h-[20vh]" :precompile-help="precompileHelp" show-model
				prompt-placeholder="Triage this crash report and reply with next steps.">
				<template v-if="form.type !== 'script'" #hint>
					The triggering message and the messages just before it are added below the prompt.
				</template>
			</TaskPromptSection>

			<p v-if="error" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ error }}</p>
			<div class="flex justify-end gap-3">
				<button type="button" :disabled="busy" :class="SMALL_BTN" @click="editing = false">Cancel</button>
				<button type="submit" data-testid="workflow-save" :disabled="busy || !form.name.trim() || !form.content.trim()"
					class="rounded-lg bg-blue-600 px-4 py-2 text-sm font-semibold text-white hover:bg-blue-500 disabled:opacity-50">
					Save
				</button>
			</div>
		</form>
	</div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useMCP } from "../../composables/useMCP";
import { useTaskPause } from "../../composables/useTaskPause";
import { channelName, channelTarget } from "../../lib/channels";
import {
	type AgentChannel,
	type AgentTask,
	isWorkflow,
	taskSessionPath,
	useSettingsStore,
} from "../../stores/settings";
import SessionSelect from "../SessionSelect.vue";
import TaskPromptSection from "../TaskPromptSection.vue";
import PausedChip from "../tasks/PausedChip.vue";
import TaskEnabledSwitch from "../tasks/TaskEnabledSwitch.vue";
import TaskPrecompileBadge from "../tasks/TaskPrecompileBadge.vue";
import { AppCombobox, AppSelect } from "../ui/select";

// A channel's workflows: tasks run by a matching message on the channel
// instead of a schedule. They are created and changed through the same task
// tools as scheduled tasks, with trigger=message.

const props = defineProps<{ agent: string; channel: AgentChannel }>();

const settings = useSettingsStore();
const { callTool } = useMCP();
const pause = useTaskPause();
const route = useRoute();
const router = useRouter();

const SMALL_BTN =
	"rounded-lg border border-gray-200 px-3 py-1.5 text-xs font-medium text-gray-700 hover:bg-gray-100 disabled:opacity-50 dark:border-gray-700 dark:text-gray-300 dark:hover:bg-gray-800";
const TYPE_OPTIONS = [
	{ value: "prompt", label: "prompt" },
	{ value: "script", label: "script" },
];
const OVERLAP_OPTIONS = [
	{ value: "queue", label: "Queue the new run" },
	{ value: "skip", label: "Skip the new run" },
];

const name = computed(() => channelName(props.channel));

/** Whether task belongs to this channel, picked by its name or ID. */
function ofChannel(task: AgentTask): boolean {
	const c = (task.channel ?? "").trim().toLowerCase();
	return (
		c === name.value.toLowerCase() ||
		c === (props.channel.id ?? "").trim().toLowerCase()
	);
}

const workflows = computed(() =>
	(
		settings.config?.agents
			.find((a) => a.name === props.agent)
			?.tasks.filter((t) => isWorkflow(t) && ofChannel(t)) ?? []
	)
		.slice()
		.sort((a, b) => a.name.localeCompare(b.name)),
);

function describe(w: AgentTask): string {
	const what =
		w.match?.trim() && w.match.trim() !== "*"
			? `messages matching ${w.match}`
			: "every message";
	const where = w.entry ? ` on ${w.entry}` : "";
	return `${what}${where} · ${viaLabel(w.target ?? "")}`;
}

function viaLabel(target: string): string {
	if (!target || target === "silent") return "silent";
	if (target === "reply") return "replies in the conversation";
	return `sends to ${target.replace(/^channel:|^session:/, "")}`;
}

// ── Form ──────────────────────────────────────────────────────────────────

const editing = ref(false);
const original = ref("");
const busy = ref(false);
const error = ref("");
const form = reactive({
	name: "",
	type: "prompt",
	entry: "",
	match: "",
	model: "",
	via: "reply",
	session: "",
	overlap: "queue",
	content: "",
	enabled: true,
	precompile: false,
});

/** The workflow being edited as saved, for its compiled state. */
const saved = computed(() =>
	workflows.value.find((w) => w.name === original.value),
);

/** What precompiling does for this workflow, and whether its script exists yet. */
const precompileHelp = computed(() => {
	if (form.type === "script" || !form.precompile) return "";
	const w = saved.value;
	if (w?.precompile === true && w.precompile_note) return w.precompile_note;
	if (w?.precompile === true && w.compiled)
		return "Precompiled: each matching message runs its script with no model calls, and falls back to the model if it fails";
	return "";
});

const otherChannels = computed(
	() =>
		settings.config?.agents
			.find((a) => a.name === props.agent)
			?.channels.map((c) => channelName(c)) ?? [],
);
const viaOptions = computed(() => [
	{ value: "", label: "silent" },
	{ value: "reply", label: "reply to the source conversation" },
	{ value: "session", label: "a session" },
	...otherChannels.value.map((c) => ({
		value: channelTarget(c),
		label: `channel: ${c}`,
	})),
]);

const takesEverything = computed(() => {
	const m = form.match.trim();
	return m === "" || /^\*+$/.test(m);
});

function reset(w?: AgentTask) {
	error.value = "";
	original.value = w?.name ?? "";
	form.name = w?.name ?? "";
	form.type = w?.type === "script" ? "script" : "prompt";
	form.entry = w?.entry ?? "";
	form.match = w?.match ?? "";
	form.model = w?.model ?? "";
	form.overlap = w?.overlap ?? "queue";
	form.content = w?.prompt ?? "";
	form.enabled = w ? w.enabled !== false : true;
	// A saved workflow keeps its own choice, where unset is off as it runs;
	// a new one follows scheduler.precompute_tasks, as new tasks do.
	form.precompile = w
		? w.precompile === true
		: settings.config?.scheduler?.precompute_tasks !== false;
	const target = w ? (w.target ?? "") : "reply";
	form.session = target.startsWith("session:") ? target.slice(8) : "";
	form.via = target.startsWith("session:") ? "session" : target;
	editing.value = true;
	void loadEntries();
}

function startNew() {
	reset();
}

function startEdit(w: AgentTask) {
	reset(w);
}

function target(): string {
	if (form.via === "session")
		return form.session ? `session:${form.session}` : "";
	return form.via;
}

async function save() {
	busy.value = true;
	error.value = "";
	const newName = form.name.trim();
	const precompiled = saved.value?.precompile === true;
	try {
		if (original.value && original.value !== newName) {
			await settings.renameTask(props.agent, original.value, newName);
		}
		if (original.value) {
			await callTool("task_update", {
				name: `${props.agent}/${newName}`,
				enabled: form.enabled,
				content: form.content,
				entry: form.entry,
				match: form.match,
				model: form.model,
				target: target(),
				overlap: form.overlap,
				// Changing it starts the compiled script over, so an unchanged
				// choice is left alone.
				...(form.type === "prompt" && form.precompile !== precompiled
					? { precompile: form.precompile }
					: {}),
			});
		} else {
			await callTool("task_schedule", {
				agent: props.agent,
				name: newName,
				type: form.type,
				content: form.content,
				trigger: "message",
				channel: name.value,
				entry: form.entry,
				match: form.match,
				model: form.model,
				target: target(),
				overlap: form.overlap,
				...(form.type === "prompt" ? { precompile: form.precompile } : {}),
			});
			if (!form.enabled)
				await callTool("task_update", {
					name: `${props.agent}/${newName}`,
					enabled: false,
				});
		}
		await settings.fetchConfig();
		editing.value = false;
	} catch (e) {
		error.value = e instanceof Error ? e.message : String(e);
	} finally {
		busy.value = false;
	}
}

async function remove(w: AgentTask) {
	if (!window.confirm(`Delete the workflow ${w.name}?`)) return;
	try {
		await callTool("task_delete", { name: `${props.agent}/${w.name}` });
		await settings.fetchConfig();
	} catch (e) {
		error.value = e instanceof Error ? e.message : String(e);
	}
}

// ── Source entries and the "would match" preview ───────────────────────────

const entries = ref<{ entry: string; title?: string }[]>([]);
const entryOptions = computed(() => {
	const opts = entries.value.map((e) => ({
		value: e.entry,
		label: e.title && e.title !== e.entry ? `${e.title} (${e.entry})` : e.entry,
	}));
	if (form.entry && !opts.some((o) => o.value === form.entry))
		opts.unshift({ value: form.entry, label: form.entry });
	return opts;
});

async function loadEntries() {
	try {
		const raw = await callTool("workflow_entries", {
			agent: props.agent,
			channel: name.value,
		});
		entries.value = JSON.parse(raw) ?? [];
	} catch {
		entries.value = [];
	}
}

interface PreviewMessage {
	entry: string;
	sender?: string;
	text: string;
	timestamp: string;
	matches: boolean;
}
const preview = ref<PreviewMessage[]>([]);
let previewTimer: ReturnType<typeof setTimeout> | undefined;
watch(
	() => [editing.value, form.entry, form.match],
	() => {
		clearTimeout(previewTimer);
		if (!editing.value) return;
		previewTimer = setTimeout(async () => {
			try {
				const raw = await callTool("workflow_preview", {
					agent: props.agent,
					channel: name.value,
					entry: form.entry,
					match: form.match,
					limit: 15,
				});
				preview.value = JSON.parse(raw) ?? [];
			} catch {
				preview.value = [];
			}
		}, 250);
	},
);

// ?workflow=<name> opens that workflow's form, as a card's gear does.
function openFromRoute() {
	const wanted =
		typeof route.query.workflow === "string" ? route.query.workflow : "";
	if (!wanted) return;
	const w = workflows.value.find((t) => t.name === wanted);
	if (w) startEdit(w);
	const { workflow: _, ...query } = route.query;
	void router.replace({ query });
}

onMounted(openFromRoute);
watch(workflows, () => {
	if (!editing.value) openFromRoute();
});
</script>
