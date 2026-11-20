<template>
	<div class="min-h-[60vh] space-y-5 p-5" data-testid="agent-projects">
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
						<router-link :to="`/projects/${encodeProject(t)}`" class="text-xs text-blue-600 hover:underline dark:text-blue-400">Board</router-link>
						<button type="button" :class="ICON_BTN" :aria-label="`Remove ${t}`" @click="removeProject(t)">✕</button>
					</li>
				</ul>
				<form class="space-y-2" @submit.prevent="addProject">
					<label class="block">
						<span :class="LABEL">Add a repository or project board</span>
						<div class="flex gap-2">
							<input v-model="newTarget" type="text" autocomplete="off" spellcheck="false" placeholder="owner/repo"
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
				<p class="text-xs text-gray-500 dark:text-gray-400">Save Changes writes the projects to {{ agentLabel }} in aviary.yaml; the running watch picks them up right away.</p>
			</div>
		</section>

		<section :class="CARD">
			<h3 :class="CARD_HEAD">Jobs run on <span :class="CARD_NOTE">{{ agentLabel }}'s model, then down its fallbacks when quota runs out</span></h3>
			<div class="space-y-2 p-4">
				<p v-if="!models.length" class="text-sm text-red-600 dark:text-red-400" data-testid="agent-problem">
					{{ agentLabel }} has no model a job can run on. Set one on the General subtab.
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

		<section :class="CARD" data-testid="agent-project-settings">
			<h3 :class="CARD_HEAD">
				Settings <span :class="CARD_NOTE">One engine runs every agent's projects, so these combine with other agents' project settings. Grey tags mark defaults that aren't saved</span>
			</h3>
			<div class="space-y-4 p-4">
				<div class="grid gap-4 sm:grid-cols-3">
					<label class="block">
						<span :class="LABEL">Jobs at once <DefaultTag :on="isDefault('concurrency')" /></span>
						<input v-model.number="form.concurrency" type="number" min="1" :class="INPUT" data-field="concurrency" />
					</label>
					<label class="block">
						<span :class="LABEL">Ready label <DefaultTag :on="isDefault('readyLabel')" /></span>
						<input v-model="form.readyLabel" type="text" placeholder="Off (board status only)" :class="INPUT" data-field="ready_label" />
					</label>
					<label class="block">
						<span :class="LABEL">How to read GitHub <DefaultTag :on="isDefault('delivery')" /></span>
						<select v-model="form.delivery" :class="INPUT" data-field="delivery">
							<option v-for="d in DELIVERY_OPTIONS" :key="d.key" :value="d.key">{{ d.label }}</option>
						</select>
					</label>
				</div>
				<label class="block">
					<span :class="LABEL">Who can steer jobs through comments</span>
					<input v-model="commentersText" type="text" placeholder="Only you (the signed-in gh user)" :class="[INPUT, 'font-mono']"
						data-field="allowed_commenters" @input="form.commenters = parseList(commentersText)" />
				</label>
				<div class="space-y-3">
					<label v-for="t in toggles" :key="t.key" class="flex cursor-pointer items-center gap-3 text-sm text-gray-800 dark:text-gray-200">
						<SwitchRoot :checked="form[t.key]" :aria-label="t.label" :data-field="t.key"
							class="relative inline-flex h-6 w-11 shrink-0 items-center rounded-full transition-colors focus:outline-none"
							:class="form[t.key] ? 'bg-blue-600' : 'bg-gray-200 dark:bg-gray-700'"
							@update:checked="(v: boolean) => (form[t.key] = v)">
							<SwitchThumb class="inline-block h-5 w-5 transform rounded-full bg-white shadow transition-transform"
								:class="form[t.key] ? 'translate-x-[110%]' : 'translate-x-[10%]'" />
						</SwitchRoot>
						{{ t.label }}
					</label>
				</div>
				<details class="rounded-lg border border-gray-200 dark:border-gray-800" :open="advancedOpen">
					<summary class="cursor-pointer px-3 py-2 text-sm font-medium text-gray-700 dark:text-gray-300">Issue selection and checking</summary>
					<div class="grid gap-4 border-t border-gray-200 p-3 dark:border-gray-800 sm:grid-cols-2">
						<div>
							<label class="block">
								<span :class="LABEL">Issue filter, one term group per line <DefaultTag :on="isDefault('filter')" /></span>
								<textarea v-model="filterText" rows="2" :disabled="form.allIssues" :class="[INPUT, 'font-mono disabled:opacity-50']"
									data-field="filter" @input="form.filter = lines(filterText)" />
							</label>
							<label class="mt-1 flex items-center gap-2 text-xs text-gray-600 dark:text-gray-400">
								<input v-model="form.allIssues" type="checkbox" data-field="all_issues" /> Every ready issue, not just filtered ones
							</label>
						</div>
						<div class="space-y-3">
							<label class="block">
								<span :class="LABEL">Check interval</span>
								<input v-model="form.interval" type="text" :placeholder="defaultInterval(form.delivery)" :class="INPUT" data-field="interval" />
							</label>
							<label class="block">
								<span :class="LABEL">Board ready status</span>
								<input v-model="form.readyState" type="text" placeholder="Auto-detect Todo or Ready" :class="INPUT" data-field="ready_state" />
							</label>
							<label class="block">
								<span :class="LABEL">Stuck-job watchdog interval</span>
								<input v-model="form.unblockInterval" type="text" placeholder="Default (0 turns it off)" :class="INPUT" data-field="unblock_interval" />
							</label>
						</div>
						<label class="block sm:col-span-2">
							<span :class="LABEL">Clone into, one directory per line (first with 2 GiB free)</span>
							<textarea v-model="tempDirsText" rows="2" placeholder="System temp directory" :class="[INPUT, 'font-mono']"
								data-field="temp_dirs" @input="form.tempDirs = lines(tempDirsText)" />
						</label>
					</div>
				</details>
			</div>
		</section>

		<section :class="CARD">
			<h3 :class="CARD_HEAD">Clones <span :class="CARD_NOTE">Each job works in its own clone, shared across projects</span></h3>
			<div class="p-4">
				<p v-if="!clones.loaded.value" class="text-sm text-gray-500">Reading clones…</p>
				<p v-else-if="clones.error.value" class="text-sm text-gray-500">Couldn't list clones: {{ clones.error.value }}</p>
				<p v-else-if="!clones.clones.value.length" class="text-sm text-gray-500">No clones on disk.</p>
				<ul v-else class="divide-y divide-gray-200 text-sm dark:divide-gray-800">
					<li v-for="c in clones.clones.value" :key="c.path" data-testid="clone-row" class="flex items-center gap-3 py-2">
						<div class="min-w-0 flex-1">
							<p class="truncate font-mono text-xs text-gray-800 dark:text-gray-200">{{ c.path }}</p>
							<p class="text-xs text-gray-500">
								{{ [c.repo && c.issue ? `${c.repo}#${c.issue}` : c.repo, c.pr ? `PR #${c.pr}` : ""].filter(Boolean).join(" · ") }}
							</p>
						</div>
						<span data-testid="clone-size" class="text-xs tabular-nums text-gray-500"
							:title="c.size === null ? 'Not measured in time' : undefined">{{ c.size === null ? "size unknown" : formatBytes(c.size) }}</span>
						<span class="text-xs" :class="c.inUse ? 'text-amber-700 dark:text-amber-400' : 'text-gray-500'" :title="c.reason">
							{{ c.inUse ? "in use" : "unused" }}
						</span>
					</li>
				</ul>
				<p v-if="clones.loaded.value && !clones.error.value && clones.clones.value.length" data-testid="clone-total" class="mt-2 text-xs text-gray-500">
					{{ clones.clones.value.length }} {{ clones.clones.value.length === 1 ? "clone" : "clones" }},
					{{ formatBytes(clones.totalSize.value) }} total<template v-if="clones.unmeasured.value">
						({{ clones.unmeasured.value }} not measured)</template>
				</p>
				<button v-if="clones.clones.value.some((c) => !c.inUse)" type="button" :disabled="clones.clearing.value"
					class="mt-3 rounded-lg border border-gray-300 px-3 py-1.5 text-sm text-gray-700 hover:bg-gray-50 disabled:opacity-50 dark:border-gray-700 dark:text-gray-200 dark:hover:bg-gray-800"
					@click="clones.clear()">{{ clones.clearing.value ? "Clearing…" : "Clear unused clones" }}</button>
				<p v-if="clones.cleared.value" data-testid="clone-message" class="mt-2 text-xs text-green-700 dark:text-green-400">
					Removed {{ clones.cleared.value.removed }} {{ clones.cleared.value.removed === 1 ? "clone" : "clones" }},
					reclaimed {{ formatBytes(clones.cleared.value.reclaimed) }}<template v-if="clones.cleared.value.kept">, kept {{ clones.cleared.value.kept }} in use</template>.
				</p>
			</div>
		</section>

		<section :class="CARD">
			<h3 :class="CARD_HEAD">YAML <span :class="CARD_NOTE">Only what differs from defaults ends up in aviary.yaml</span></h3>
			<pre data-testid="project-yaml"
				class="overflow-x-auto rounded-b-xl bg-gray-50 px-4 py-3 font-mono text-xs text-gray-800 dark:bg-gray-900 dark:text-gray-200">{{ yaml || "# Every setting is a default, so nothing is written." }}</pre>
		</section>
	</div>
</template>

<script setup lang="ts">
import { SwitchRoot, SwitchThumb } from "radix-vue";
import { computed, h, ref, watch } from "vue";
import { useRoute } from "vue-router";
import {
	useProjectCheck,
	useProjectClones,
} from "../../composables/useProjectSettings";
import { useProviderStatus } from "../../composables/useProviderAuth";
import {
	DELIVERY_OPTIONS,
	defaultFields,
	defaultInterval,
	formatBytes,
	isListed,
	type JobModelsConfig,
	jobModels,
	joinReportTarget,
	type ProjectReports,
	type ProjectSettings,
	type ProjectsSection,
	parseList,
	projectOwner,
	projectsYAML,
	projectTargets,
	REPORT_EVENTS,
	reportsError,
	reportsFromSection,
	settingsFromSection,
	splitReportTarget,
	withoutProject,
	withProject,
	withReports,
	withSettings,
} from "../../lib/projectSettings";
import { encodeProject } from "../../lib/projects";
import type { AgentEntry } from "../../stores/settings";

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

type ToggleKey =
	| "merge"
	| "changelog"
	| "yolo"
	| "browserVision"
	| "browserVisible";
const TOGGLES: { key: ToggleKey; label: string }[] = [
	{ key: "merge", label: "Merge pull requests when CI passes" },
	{ key: "changelog", label: "Add a changelog entry to each pull request" },
	{
		key: "yolo",
		label: "Run CLI providers without their sandboxes and permission checks",
	},
];
// The browser switches only apply to browser delivery.
const BROWSER_TOGGLES: { key: ToggleKey; label: string }[] = [
	{
		key: "browserVision",
		label: "Read pages it no longer recognizes from capped screenshots",
	},
	{
		key: "browserVisible",
		label: "Show the browser it reads GitHub with, for debugging",
	},
];

function DefaultTag(p: { on: boolean }) {
	return p.on
		? h(
				"span",
				{
					class:
						"rounded bg-gray-100 px-1.5 py-0.5 text-[10px] font-normal text-gray-500 dark:bg-gray-800 dark:text-gray-400",
				},
				"default",
			)
		: null;
}
DefaultTag.props = ["on"];

const route = useRoute();
// The CLI providers, so a model such as claude/opus shows as its own spec.
const cliStatus = useProviderStatus({ refreshOnMount: false });
const cliNames = computed(() =>
	(cliStatus.clis.value ?? []).map((cli) => cli.name),
);
// The probe is slow, so it runs here only when nothing is cached yet.
if (cliStatus.clis.value === null) void cliStatus.refresh();
const clones = useProjectClones();

const agentLabel = computed(() => props.agent.name || "This agent");
const section = computed<ProjectsSection>(() => props.agent.projects ?? {});
const targets = computed(() => projectTargets(section.value));

/** Stores the agent's section, dropping it once it holds nothing. */
function store(next: ProjectsSection) {
	const clean = Object.fromEntries(
		Object.entries(next).filter(([, v]) => v !== undefined),
	);
	props.agent.projects = Object.keys(clean).length ? clean : undefined;
}

const lines = (text: string) =>
	text
		.split("\n")
		.map((l) => l.trim())
		.filter(Boolean);

// ── Projects ────────────────────────────────────────────────────────────────

const queryTarget = (key: string) =>
	typeof route.query[key] === "string" ? route.query[key].trim() : "";
const selected = ref(queryTarget("project") || targets.value[0] || "");
watch(targets, (list) => {
	if (!list.includes(selected.value)) selected.value = list[0] ?? "";
});

const newTarget = ref("");
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

// ── Settings ────────────────────────────────────────────────────────────────

const form = ref<ProjectSettings>(settingsFromSection(section.value));
const commentersText = ref(form.value.commenters.join(", "));
const filterText = ref(form.value.filter.join("\n"));
const tempDirsText = ref(form.value.tempDirs.join("\n"));
watch(
	form,
	(s) => {
		const next = withSettings(section.value, s);
		if (JSON.stringify(next) !== JSON.stringify(section.value)) store(next);
	},
	{ deep: true },
);

const defaults = computed(() => defaultFields(form.value));
const isDefault = (key: keyof ProjectSettings) => defaults.value.has(key);
const advancedOpen = computed(
	() =>
		!(
			[
				"filter",
				"allIssues",
				"interval",
				"readyState",
				"unblockInterval",
				"tempDirs",
			] as const
		).every(isDefault),
);
const toggles = computed(() =>
	form.value.delivery === "browser"
		? [...TOGGLES, ...BROWSER_TOGGLES]
		: TOGGLES,
);

const yaml = computed(() =>
	projectsYAML(props.agent.name || "agent", section.value),
);
</script>
