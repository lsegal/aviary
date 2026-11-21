<template>
	<section v-show="activeTab === 'projects'" class="space-y-5 pb-8" data-testid="projects-settings-tab">
		<p class="text-xs text-gray-500 dark:text-gray-400">
			Every agent's projects, and the settings every project of every agent runs under. Each agent's GitHub
			Projects page is the board of its projects' jobs.
		</p>
	<section v-if="instanceID" :class="CARD" data-testid="projects-instance-id">
		<div class="flex flex-wrap items-center gap-x-4 gap-y-1 px-4 py-3">
			<span class="text-sm font-semibold text-gray-900 dark:text-white">Instance ID</span>
			<code class="rounded-md bg-gray-100 px-2 py-1 font-mono text-lg font-bold tracking-wider text-gray-900 dark:bg-gray-800 dark:text-white">{{ instanceID }}</code>
			<span :class="CARD_NOTE">This instance signs its GitHub claims as <code class="font-mono">/aviary:{{ instanceID }}</code>. It is kept across restarts.</span>
		</div>
	</section>
	<section :class="CARD" data-testid="agent-projects-settings">
		<h3 :class="CARD_HEAD">Agents' projects <span :class="CARD_NOTE">Each project belongs to exactly one agent</span></h3>
		<p v-if="!agents.length" class="p-4 text-sm text-gray-500 dark:text-gray-400">
			No agents yet. <router-link to="/agents?add=1" class="text-blue-600 hover:underline dark:text-blue-400">Add an agent</router-link> to give it projects.
		</p>
		<template v-else>
			<nav class="flex flex-wrap border-b border-gray-200 px-4 dark:border-gray-800" role="tablist" aria-label="Agent">
				<button v-for="a in agents" :key="a.name" type="button" role="tab" data-testid="agent-projects-agent"
					:aria-selected="a === agent" class="-mb-px border-b-2 px-3 py-2 text-sm"
					:class="a === agent ? 'border-blue-600 font-semibold text-blue-700 dark:border-blue-400 dark:text-blue-400' : 'border-transparent text-gray-500 hover:text-gray-800 dark:text-gray-400 dark:hover:text-gray-200'"
					@click="selectAgent(a.name)">
					{{ a.name }} <span class="text-xs font-normal text-gray-400">{{ projectTargets(a.projects).length }}</span>
				</button>
			</nav>
			<SettingsAgentProjects v-if="agent && activeTab === 'projects'" :key="agent.name" :agent="agent" :config="settings.draft" />
		</template>
	</section>
	<section :class="CARD" data-testid="project-settings">
		<h3 :class="CARD_HEAD">
			Settings <span :class="CARD_NOTE">One set for every project of every agent. Grey tags mark defaults that aren't saved</span>
		</h3>
		<div class="space-y-4 p-4">
			<div class="grid gap-4 sm:grid-cols-2">
				<label class="block">
					<span :class="LABEL">Jobs at once, across every project <DefaultTag :on="isDefault('concurrency')" /></span>
					<input v-model.number="form.concurrency" type="number" min="1" :class="INPUT" data-field="concurrency" />
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
			<label class="block max-w-xs">
				<span :class="LABEL">CLI approval mode</span>
				<select v-model="form.approvalMode" :class="INPUT" data-field="approval_mode">
					<option value="">Same as the general approval mode</option>
					<option v-for="m in APPROVAL_MODES" :key="m.value" :value="m.value">{{ m.label }}</option>
				</select>
				<span class="mt-1 block text-xs text-gray-500 dark:text-gray-400">How much jobs' CLI providers may do
					without asking, translated into each CLI's own flags at launch.</span>
			</label>
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
					<span data-testid="clone-state" class="max-w-xs text-right text-xs" :class="c.inUse ? 'text-amber-700 dark:text-amber-400' : 'text-gray-500'">
						{{ cloneState(c) }}<template v-if="c.reason">: <span data-testid="clone-reason">{{ c.reason }}</span></template>
					</span>
				</li>
			</ul>
			<p v-if="clones.loaded.value && !clones.error.value && clones.clones.value.length" data-testid="clone-total" class="mt-2 text-xs text-gray-500">
				{{ clones.clones.value.length }} {{ clones.clones.value.length === 1 ? "clone" : "clones" }},
				{{ formatBytes(clones.totalSize.value) }} total<template v-if="clones.unmeasured.value">
					({{ clones.unmeasured.value }} not measured)</template>
			</p>
			<button v-if="clones.clones.value.some((c) => cloneState(c) === 'unused')" type="button" :disabled="clones.clearing.value"
				class="mt-3 rounded-lg border border-gray-300 px-3 py-1.5 text-sm text-gray-700 hover:bg-gray-50 disabled:opacity-50 dark:border-gray-700 dark:text-gray-200 dark:hover:bg-gray-800"
				@click="clones.clear()">{{ clones.clearing.value ? "Clearing…" : "Clear unused clones" }}</button>
			<p v-if="clones.cleared.value" data-testid="clone-message" class="mt-2 text-xs text-green-700 dark:text-green-400">
				Removed {{ clones.cleared.value.removed }} {{ clones.cleared.value.removed === 1 ? "clone" : "clones" }},
				reclaimed {{ formatBytes(clones.cleared.value.reclaimed) }}<template v-if="clones.cleared.value.kept">, kept {{ clones.cleared.value.kept }}</template>.
			</p>
		</div>
	</section>


		<section :class="CARD">
			<h3 :class="CARD_HEAD">YAML <span :class="CARD_NOTE">Only what differs from defaults ends up in aviary.yaml</span></h3>
			<pre data-testid="project-yaml"
				class="overflow-x-auto rounded-b-xl bg-gray-50 px-4 py-3 font-mono text-xs text-gray-800 dark:bg-gray-900 dark:text-gray-200">{{ yaml || "# Every setting is a default, so nothing is written." }}</pre>
		</section>
	</section>
</template>

<script setup lang="ts">
import { SwitchRoot, SwitchThumb } from "radix-vue";
import { computed, h, inject, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import {
	useProjectClones,
	useProjectInstanceID,
} from "../../composables/useProjectSettings";
import { APPROVAL_MODES } from "../../lib/approval";
import {
	cloneState,
	DELIVERY_OPTIONS,
	defaultFields,
	defaultInterval,
	formatBytes,
	type ProjectSettings,
	type ProjectsSection,
	parseList,
	projectsYAML,
	projectTargets,
	settingsFromSection,
	withSettings,
} from "../../lib/projectSettings";
import type { AppConfig } from "../../stores/settings";
import { settingsViewContextKey } from "./context";
import SettingsAgentProjects from "./SettingsAgentProjects.vue";

const context = inject(settingsViewContextKey) as
	| { activeTab: string; draft: AppConfig }
	| undefined;
if (!context) throw new Error("Settings view context is not available.");
const settings = context;
const activeTab = computed(() => settings.activeTab);

const CARD =
	"rounded-xl border border-gray-200 bg-white dark:border-gray-800 dark:bg-gray-950";
const CARD_HEAD =
	"flex flex-wrap items-baseline gap-x-2 border-b border-gray-200 px-4 py-3 text-sm font-semibold text-gray-900 dark:border-gray-800 dark:text-white";
const CARD_NOTE = "text-xs font-normal text-gray-500 dark:text-gray-400";
const LABEL =
	"mb-1 flex items-center gap-2 text-xs font-medium text-gray-700 dark:text-gray-300";
const INPUT =
	"w-full rounded-lg border border-gray-300 px-3 py-2 text-sm focus:border-blue-500 focus:outline-none dark:border-gray-700 dark:bg-gray-900 dark:text-white";

type ToggleKey = "merge" | "changelog" | "browserVision" | "browserVisible";
const TOGGLES: { key: ToggleKey; label: string }[] = [
	{ key: "merge", label: "Merge pull requests when CI passes" },
	{ key: "changelog", label: "Add a changelog entry to each pull request" },
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

const clones = useProjectClones();
const instanceID = useProjectInstanceID();

// The agent whose projects are shown comes from ?agent, so the boards'
// Settings links open on their agent.
const route = useRoute();
const router = useRouter();
const agents = computed(() =>
	settings.draft.agents.filter((a) => a.name.trim()),
);
const agent = computed(
	() =>
		agents.value.find((a) => a.name === route.query.agent) ?? agents.value[0],
);
function selectAgent(name: string) {
	void router.replace({ query: { agent: name } });
}

const section = computed<ProjectsSection>(() => settings.draft.projects ?? {});

/** Stores the shared section, dropping it once it holds nothing. */
function store(next: ProjectsSection) {
	const clean = Object.fromEntries(
		Object.entries(next).filter(([, v]) => v !== undefined),
	);
	settings.draft.projects = Object.keys(clean).length ? clean : undefined;
}

const lines = (text: string) =>
	text
		.split("\n")
		.map((l) => l.trim())
		.filter(Boolean);

const form = ref<ProjectSettings>(settingsFromSection(section.value));
const commentersText = ref("");
const filterText = ref("");
const tempDirsText = ref("");
function syncText() {
	commentersText.value = form.value.commenters.join(", ");
	filterText.value = form.value.filter.join("\n");
	tempDirsText.value = form.value.tempDirs.join("\n");
}
syncText();
watch(
	form,
	(s) => {
		const next = withSettings(section.value, s);
		if (JSON.stringify(next) !== JSON.stringify(section.value)) store(next);
	},
	{ deep: true },
);
// A reload or revert replaces the draft; the form follows it.
watch(section, (s) => {
	if (JSON.stringify(withSettings(s, form.value)) === JSON.stringify(s)) return;
	form.value = settingsFromSection(s);
	syncText();
});

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

const yaml = computed(() => projectsYAML(section.value));
</script>
