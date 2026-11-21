<template>
	<div class="space-y-5 p-4" data-testid="agent-projects">
		<section :class="CARD" data-testid="agent-project-list">
			<h3 :class="CARD_HEAD">
				Projects <span :class="CARD_NOTE">GitHub projects {{ agentLabel }} owns. Their jobs run on {{ agentLabel }}'s model and fallbacks</span>
			</h3>
			<div class="space-y-4 p-4">
				<p v-if="!targets.length" class="rounded-lg border border-dashed border-gray-300 px-3 py-2 text-xs text-gray-500 dark:border-gray-700 dark:text-gray-400">
					{{ agentLabel }} owns no projects yet.
				</p>
				<ul v-else class="divide-y divide-gray-200 rounded-lg border border-gray-200 dark:divide-gray-800 dark:border-gray-800">
					<li v-for="t in targets" :key="t" data-testid="agent-project" :data-target="t"
						class="flex items-center gap-2 px-3 py-2" :class="t === selected ? 'bg-blue-50 dark:bg-blue-950/30' : ''">
						<button type="button" class="min-w-0 flex-1 truncate text-left font-mono text-sm text-gray-900 dark:text-white"
							:aria-pressed="t === selected" @click="selected = t">{{ t }}</button>
						<router-link :to="projectPath(agent.name, t)" class="text-xs text-blue-600 hover:underline dark:text-blue-400">Board</router-link>
						<button v-if="savedTargets.includes(t)" type="button" :class="ICON_BTN" :aria-label="`Delete ${t}`"
							title="Delete project" @click="deleting = t">✕</button>
						<button v-else type="button" :class="ICON_BTN" :aria-label="`Remove ${t}`"
							title="Not saved yet" @click="removeProject(t)">✕</button>
					</li>
				</ul>
				<form class="space-y-2" @submit.prevent="addProject">
					<label class="block">
						<span :class="LABEL">Add a repository or project board</span>
						<div class="flex gap-2">
							<input ref="newTargetInput" v-model="newTarget" type="text" autocomplete="off" spellcheck="false" placeholder="owner/repo"
								:class="[INPUT, 'font-mono']" data-field="new_project" />
							<button type="submit" :disabled="!canAdd"
								class="shrink-0 rounded-lg bg-blue-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-blue-700 disabled:opacity-50">Add</button>
						</div>
					</label>
					<p v-if="newTarget.trim()" data-testid="new-project-check" class="text-xs"
						:class="checkError || (checkResult && !checkResult.ok) ? 'text-amber-700 dark:text-amber-400' : 'text-gray-500'">
						{{ checkNote }}
					</p>
				</form>
				<p class="text-xs text-gray-500 dark:text-gray-400">Save Changes writes the projects to {{ agentLabel }} in aviary.yaml; the running watch picks them up right away.
					Every project runs under the shared settings below.
					<router-link :to="`/agents/${encodeURIComponent(agent.name)}/projects`" data-testid="agent-board-link" class="text-blue-600 hover:underline dark:text-blue-400">Open {{ agentLabel }}'s board</router-link>.</p>
			</div>
		</section>

		<section :class="CARD">
			<h3 :class="CARD_HEAD">Jobs run on <span :class="CARD_NOTE">{{ agentLabel }}'s model, then down its fallbacks when quota runs out</span></h3>
			<div class="space-y-2 p-4">
				<p v-if="!models.length" class="text-sm text-red-600 dark:text-red-400" data-testid="agent-problem">
					{{ agentLabel }} has no model a job can run on. Set one on its Settings page.
				</p>
				<ol v-else class="space-y-2" data-testid="job-models">
					<li v-for="(m, i) in models" :key="m.spec" :data-spec="m.spec"
						class="flex flex-wrap items-center gap-x-3 gap-y-1 rounded-xl border border-gray-200 px-4 py-2 dark:border-gray-800">
						<span class="w-4 text-sm text-gray-500">{{ i + 1 }}</span>
						<b class="font-mono text-sm text-gray-900 dark:text-white">{{ m.spec }}</b>
						<span class="text-xs text-gray-500">{{ i === 0 ? "model" : "fallback" }}<template v-if="m.spec.startsWith('aviary/')"> · runs on the agent itself</template></span>
					</li>
				</ol>
			</div>
		</section>

		<section v-if="selected" :class="CARD" data-testid="agent-project-reports">
			<h3 :class="CARD_HEAD">Reports <span :class="CARD_NOTE">Send {{ selected }}'s job events through an Aviary agent's channels</span></h3>
			<div class="space-y-4 p-4">
				<label class="flex cursor-pointer items-center gap-3 text-sm text-gray-800 dark:text-gray-200">
					<SwitchRoot :checked="reports.enabled" aria-label="Send job reports" data-field="reports"
						class="relative inline-flex h-6 w-11 shrink-0 items-center rounded-full transition-colors focus:outline-none"
						:class="reports.enabled ? 'bg-blue-600' : 'bg-gray-200 dark:bg-gray-700'"
						@update:checked="setReports">
						<SwitchThumb class="inline-block h-5 w-5 transform rounded-full bg-white shadow transition-transform"
							:class="reports.enabled ? 'translate-x-[110%]' : 'translate-x-[10%]'" />
					</SwitchRoot>
					Send job reports
				</label>
				<template v-if="reports.enabled">
					<label class="block max-w-xs">
						<span :class="LABEL">Aviary agent</span>
						<select :value="reports.agent" :class="INPUT" data-field="reports_agent"
							@change="updateReports({ agent: ($event.target as HTMLSelectElement).value })">
							<option value="">Choose an agent…</option>
							<option v-for="a in reportAgentOptions" :key="a" :value="a">{{ a }}</option>
						</select>
					</label>
					<div>
						<span :class="LABEL">Targets</span>
						<ul class="space-y-2">
							<li v-for="(t, i) in reportTargets" :key="i" data-testid="report-target" class="flex flex-wrap items-center gap-2">
								<select :value="t.channel" :class="TARGET_CHANNEL" :aria-label="`Target ${i + 1} channel`"
									@change="setReportTarget(i, ($event.target as HTMLSelectElement).value, t.id)">
									<option value="">Channel…</option>
									<option v-for="c in reportChannels(t.channel)" :key="c.value" :value="c.value">{{ c.label }}</option>
								</select>
								<input :value="t.id" type="text" :class="[INPUT, 'min-w-0 flex-1 font-mono']" :aria-label="`Target ${i + 1} delivery ID`"
									:placeholder="t.channel === 'session' ? 'Session name' : 'Channel, chat or user ID'"
									@input="setReportTarget(i, t.channel, ($event.target as HTMLInputElement).value)" />
								<button type="button" :class="ICON_BTN" :aria-label="`Remove target ${i + 1}`" @click="removeReportTarget(i)">✕</button>
							</li>
						</ul>
						<button type="button" class="mt-2 rounded-md px-2 py-0.5 text-xs text-blue-600 hover:bg-blue-50 dark:text-blue-400 dark:hover:bg-blue-950/40"
							@click="updateReports({ targets: [...reports.targets, ''] })">Add a target</button>
					</div>
					<fieldset>
						<legend :class="LABEL">Report when a job is</legend>
						<div class="flex flex-wrap gap-4">
							<label v-for="e in REPORT_EVENTS" :key="e.key" class="flex items-center gap-2 text-sm text-gray-800 dark:text-gray-200">
								<input type="checkbox" :checked="reports.events.includes(e.key)" :data-field="`report_${e.key}`"
									@change="toggleReportEvent(e.key)" />
								{{ e.label }}
							</label>
						</div>
					</fieldset>
					<p v-if="reportsProblem" data-testid="reports-error" class="text-sm text-amber-700 dark:text-amber-400">{{ reportsProblem }} Reports are not saved until this is fixed.</p>
				</template>
			</div>
		</section>

		<section :class="CARD">
			<h3 :class="CARD_HEAD">YAML <span :class="CARD_NOTE">Only what differs from defaults ends up in aviary.yaml</span></h3>
			<pre data-testid="agent-project-yaml"
				class="overflow-x-auto rounded-b-xl bg-gray-50 px-4 py-3 font-mono text-xs text-gray-800 dark:bg-gray-900 dark:text-gray-200">{{ yaml || "# Every setting is a default, so nothing is written." }}</pre>
		</section>

		<DeleteProjectDialog :target="deleting" @close="deleting = ''" @deleted="onDeleted" />
	</div>
</template>

<script setup lang="ts">
import { SwitchRoot, SwitchThumb } from "radix-vue";
import { computed, nextTick, ref, watch } from "vue";
import { useRoute } from "vue-router";
import { useProjectCheck } from "../../composables/useProjectSettings";
import { useProviderStatus } from "../../composables/useProviderAuth";
import {
	type AgentProjectsSection,
	agentProjectsYAML,
	isListed,
	type JobModelsConfig,
	jobModels,
	joinReportTarget,
	type ProjectReports,
	projectOwner,
	projectTargets,
	REPORT_EVENTS,
	reportsError,
	reportsFromSection,
	splitReportTarget,
	withoutProject,
	withProject,
	withReports,
} from "../../lib/projectSettings";
import { projectPath } from "../../lib/projects";
import { type AgentEntry, useSettingsStore } from "../../stores/settings";
import DeleteProjectDialog from "../projects/DeleteProjectDialog.vue";

const props = defineProps<{
	/** The agent whose projects are edited; its projects field is updated in place. */
	agent: AgentEntry;
	/** The draft config, for models.defaults, the other agents and their channels. */
	config: Omit<JobModelsConfig, "agents"> & { agents: AgentEntry[] };
}>();

const CARD =
	"rounded-xl border border-gray-200 bg-white dark:border-gray-800 dark:bg-gray-950";
const CARD_HEAD =
	"flex flex-wrap items-baseline gap-x-2 border-b border-gray-200 px-4 py-3 text-sm font-semibold text-gray-900 dark:border-gray-800 dark:text-white";
const CARD_NOTE = "text-xs font-normal text-gray-500 dark:text-gray-400";
const LABEL =
	"mb-1 flex items-center gap-2 text-xs font-medium text-gray-700 dark:text-gray-300";
const INPUT =
	"w-full rounded-lg border border-gray-300 px-3 py-2 text-sm focus:border-blue-500 focus:outline-none dark:border-gray-700 dark:bg-gray-900 dark:text-white";
// INPUT without w-full, so the channel sits beside its delivery ID.
const TARGET_CHANNEL = `${INPUT.replace("w-full ", "")} w-44 shrink-0`;
const ICON_BTN =
	"rounded-md px-1.5 text-sm text-gray-500 hover:bg-gray-100 disabled:opacity-30 dark:hover:bg-gray-800";

const route = useRoute();
// The CLI providers, so a model such as claude/opus shows as its own spec.
const cliStatus = useProviderStatus({ refreshOnMount: false });
const cliNames = computed(() =>
	(cliStatus.clis.value ?? []).map((cli) => cli.name),
);
// The probe is slow, so it runs here only when nothing is cached yet.
if (cliStatus.clis.value === null) void cliStatus.refresh();

const agentLabel = computed(() => props.agent.name || "This agent");
const section = computed<AgentProjectsSection>(
	() => props.agent.projects ?? [],
);
const targets = computed(() => projectTargets(section.value));

/** Stores the agent's projects, dropping them once there are none. */
function store(next: AgentProjectsSection) {
	props.agent.projects = next.length ? next : undefined;
}

// ── Projects ────────────────────────────────────────────────────────────────

const queryTarget = (key: string) =>
	typeof route.query[key] === "string" ? route.query[key].trim() : "";
const selected = ref(queryTarget("project") || targets.value[0] || "");
watch(targets, (list) => {
	if (!list.includes(selected.value)) selected.value = list[0] ?? "";
});

const newTarget = ref("");
// The sidebar's + Add project opens this page with ?new to focus the input.
const newTargetInput = ref<HTMLInputElement | null>(null);
watch(
	() => route.query.new,
	async (v) => {
		if (v === undefined) return;
		await nextTick();
		newTargetInput.value?.focus();
	},
	{ immediate: true },
);
const check = useProjectCheck(newTarget);
const checkResult = check.result;
const checkError = check.error;
// The checks normalize the target (URLs become owner/repo).
const normalized = computed(
	() => checkResult.value?.target || newTarget.value.trim(),
);
const otherOwner = computed(() => {
	const owner = projectOwner(props.config, normalized.value);
	return owner && owner !== props.agent.name ? owner : "";
});
const canAdd = computed(
	() =>
		!!newTarget.value.trim() &&
		!check.checking.value &&
		!checkError.value &&
		!!checkResult.value &&
		!otherOwner.value &&
		!isListed(section.value, normalized.value),
);
const checkNote = computed(() => {
	if (check.checking.value) return "Checking…";
	if (checkError.value) return `Can't watch this: ${checkError.value}`;
	if (otherOwner.value)
		return `${normalized.value} already belongs to agent ${otherOwner.value}. A project has exactly one agent.`;
	if (isListed(section.value, normalized.value))
		return `${agentLabel.value} already owns ${normalized.value}.`;
	if (!checkResult.value) return "";
	const problems = checkResult.value.checks.filter((c) => c.status !== "ok");
	return problems.length
		? problems.map((c) => c.message).join(" ")
		: "Ready to go.";
});

function addProject() {
	if (!canAdd.value) return;
	const target = normalized.value;
	store(withProject(section.value, target));
	selected.value = target;
	newTarget.value = "";
}

function removeProject(target: string) {
	store(withoutProject(section.value, target));
}

// The agent's projects as last saved. One added here but not saved yet is
// dropped from the draft; a saved one is deleted with project_remove, which
// stops its jobs and rewrites aviary.yaml itself.
const settingsStore = useSettingsStore();
const savedTargets = computed(() =>
	projectTargets(
		settingsStore.config?.agents.find((a) => a.name === props.agent.name)
			?.projects,
	),
);

// The saved project whose ✕ opened the delete confirmation.
const deleting = ref("");

async function onDeleted(target: string) {
	deleting.value = "";
	removeProject(target);
	await settingsStore.fetchConfig();
}

// A project handed over from the board's Add project dialog starts listed.
const handedOver = queryTarget("add");
if (
	handedOver &&
	!isListed(section.value, handedOver) &&
	!projectOwner(props.config, handedOver)
) {
	store(withProject(section.value, handedOver));
	selected.value = handedOver;
}

const models = computed(() =>
	jobModels(props.agent.name, props.config, cliNames.value),
);

// ── Reports ─────────────────────────────────────────────────────────────────

// The selected project's reports form. It is written back to the section
// only while it is valid, so a half-filled form never reaches aviary.yaml.
const reports = ref<ProjectReports>(
	reportsFromSection(section.value, selected.value),
);
watch(selected, (target) => {
	reports.value = reportsFromSection(section.value, target);
});

function updateReports(patch: Partial<ProjectReports>) {
	reports.value = { ...reports.value, ...patch };
}

const agentNames = computed(() =>
	props.config.agents.map((a) => a.name).filter(Boolean),
);
// Keep a saved agent that is no longer configured selectable, so the error
// below explains it instead of the picker silently showing nothing.
const reportAgentOptions = computed(() => {
	const cur = reports.value.agent;
	return cur && !agentNames.value.includes(cur)
		? [...agentNames.value, cur]
		: agentNames.value;
});
const reportTargets = computed(() =>
	reports.value.targets.map(splitReportTarget),
);
const reportsProblem = computed(() =>
	reportsError(reports.value, agentNames.value),
);
watch(
	reports,
	(r) => {
		if (!selected.value || reportsProblem.value) return;
		const next = withReports(section.value, selected.value, r);
		if (JSON.stringify(next) !== JSON.stringify(section.value)) store(next);
	},
	{ deep: true },
);

/** The reporting agent's channels as target choices, plus session. */
function reportChannels(current: string) {
	const agent = props.config.agents.find((a) => a.name === reports.value.agent);
	const options = [
		{ value: "session", label: "session" },
		...(agent?.channels ?? [])
			.filter((ch) => ch.type && ch.id)
			.map((ch) => ({
				value: `${ch.type}:${ch.id}`,
				label: `${ch.type}: ${ch.id}`,
			})),
	];
	if (current && !options.some((o) => o.value === current))
		options.push({ value: current, label: current });
	return options;
}

function setReports(on: boolean) {
	const r = reports.value;
	updateReports({
		enabled: on,
		// Reports go through this agent's channels unless another is chosen.
		agent: r.agent || (on ? props.agent.name : ""),
		targets: on && !r.targets.length ? [""] : r.targets,
	});
}

// Targets are edited as channel + delivery ID rows; a row with no ID is kept
// as its bare channel prefix so it stays on screen while being filled in.
function setReportTarget(i: number, channel: string, id: string) {
	const list = [...reports.value.targets];
	list[i] = joinReportTarget(channel, id) || (channel ? `${channel}:` : "");
	updateReports({ targets: list });
}

function removeReportTarget(i: number) {
	updateReports({ targets: reports.value.targets.filter((_, j) => j !== i) });
}

function toggleReportEvent(key: string) {
	const events = reports.value.events;
	updateReports({
		events: events.includes(key)
			? events.filter((e) => e !== key)
			: REPORT_EVENTS.map((e) => e.key).filter(
					(e) => e === key || events.includes(e),
				),
	});
}

const yaml = computed(() =>
	agentProjectsYAML(props.agent.name || "agent", section.value),
);
</script>
