<template>
	<AppLayout>
		<div class="flex h-full flex-col overflow-y-auto">
			<header
				class="sticky top-0 z-10 flex flex-col gap-3 border-b border-gray-200 bg-white px-4 py-4 dark:border-gray-800 dark:bg-gray-950 sm:flex-row sm:items-center sm:px-6">
				<div class="min-w-0">
					<p class="truncate text-xs text-gray-500">
						<router-link to="/projects" class="text-blue-600 hover:underline dark:text-blue-400">Projects</router-link> /
						<router-link :to="boardLink" class="text-blue-600 hover:underline dark:text-blue-400">{{ target }}</router-link> /
					</p>
					<h1 class="text-xl font-bold text-gray-900 dark:text-white">{{ adding ? "Settings for a new project" : "Settings" }}</h1>
				</div>
				<div class="flex flex-wrap items-center gap-2 sm:ml-auto">
					<span data-testid="save-note" class="text-sm text-gray-500 dark:text-gray-400">{{ saveNote }}</span>
					<button type="button" :disabled="!dirty || saving"
						class="rounded-lg border border-gray-300 px-3 py-1.5 text-sm text-gray-700 hover:bg-gray-50 disabled:opacity-50 dark:border-gray-700 dark:text-gray-200 dark:hover:bg-gray-800"
						@click="discard">Discard</button>
					<button type="button" :disabled="(!dirty && !adding) || saving || !form"
						class="rounded-lg bg-blue-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-blue-700 disabled:opacity-50"
						@click="save">{{ saving ? "Saving…" : adding ? "Save and start watching" : "Save" }}</button>
				</div>
			</header>

			<p v-if="error" role="alert"
				class="mx-4 mt-4 rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-900 dark:bg-red-950/40 dark:text-red-300 sm:mx-6">
				{{ error }}
			</p>

			<div v-if="!form" class="py-12 text-center text-sm text-gray-400">Loading settings…</div>
			<div v-else class="flex gap-6 px-4 py-4 sm:px-6">
				<nav class="sticky top-24 hidden h-fit w-44 shrink-0 space-y-1 text-sm lg:block" aria-label="Settings sections">
					<a v-for="s in SECTIONS" :key="s.id" :href="`#${s.id}`"
						class="block rounded-lg px-3 py-1.5 text-gray-600 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-gray-800">{{ s.label }}</a>
				</nav>

				<div class="min-w-0 max-w-4xl flex-1 space-y-5">
					<section id="general" :class="CARD">
						<h2 :class="CARD_HEAD">General <span :class="CARD_NOTE">Shared by every project. Grey tags mark defaults that aren't saved to aviary.yaml</span></h2>
						<div class="space-y-4 p-5">
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
										@update:checked="(v: boolean) => setToggle(t.key, v)">
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

					<section id="clis" :class="CARD">
						<h2 :class="CARD_HEAD">
							Coding CLIs <span :class="CARD_NOTE">For {{ target }}. Jobs use the first available choice and fall back down the list when its quota runs out</span>
							<span v-if="probe.refreshing.value" data-testid="cli-refreshing"
								class="ml-auto inline-flex items-center gap-1.5 text-xs font-normal text-gray-500">
								<span class="h-1.5 w-1.5 animate-pulse rounded-full bg-blue-500" /> Refreshing
							</span>
						</h2>
						<div class="space-y-3 p-5">
							<p v-if="!probe.clis.value && probe.refreshing.value" class="text-sm text-gray-500">Checking coding CLIs…</p>
							<p v-else-if="!probe.clis.value && probe.error.value" class="text-sm text-gray-500">
								Couldn't read the coding CLIs: {{ probe.error.value }}
							</p>
							<p v-if="!form.agents.length" class="text-xs text-gray-500" data-testid="cli-shared">
								Using the shared order. Changing it here gives {{ target }} its own.
							</p>
							<ol class="space-y-3">
								<li v-for="(row, i) in cliRows" :key="row.name" :data-cli="row.name"
									class="rounded-xl border px-4 py-3"
									:class="row.enabled ? 'border-gray-200 dark:border-gray-800' : 'border-dashed border-gray-300 dark:border-gray-700'">
									<div class="flex flex-wrap items-center gap-x-3 gap-y-1">
										<span class="w-4 text-sm text-gray-500">{{ row.enabled ? i + 1 : "–" }}</span>
										<b class="text-sm text-gray-900 dark:text-white">{{ row.name }}</b>
										<span class="inline-flex items-center gap-1 text-xs"
											:class="row.signedIn ? 'text-green-700 dark:text-green-400' : 'text-gray-500'">
											<span v-if="row.signedIn" class="h-1.5 w-1.5 rounded-full bg-green-500" />
											{{ row.status }}
										</span>
										<div class="ml-auto flex items-center gap-1">
											<template v-if="row.enabled">
												<button type="button" :class="ICON_BTN" :disabled="i === 0" :aria-label="`Move ${row.name} up`" @click="moveCLI(i, -1)">↑</button>
												<button type="button" :class="ICON_BTN" :disabled="i === specs.length - 1" :aria-label="`Move ${row.name} down`" @click="moveCLI(i, 1)">↓</button>
											</template>
											<button type="button" class="rounded-md px-2 py-0.5 text-xs text-blue-600 hover:bg-blue-50 dark:text-blue-400 dark:hover:bg-blue-950/40"
												@click="toggleCLI(row.spec)">{{ row.enabled ? "Remove" : "Use" }}</button>
										</div>
									</div>
									<div v-if="row.enabled && row.models.length" class="mt-2 flex flex-wrap gap-2 pl-7">
										<button v-for="m in [...row.models, '']" :key="m" type="button"
											class="rounded-md border px-2.5 py-1 font-mono text-xs"
											:class="row.model === m ? 'border-blue-400 bg-blue-50 text-blue-700 dark:border-blue-700 dark:bg-blue-950/40 dark:text-blue-300' : 'border-gray-200 text-gray-700 hover:bg-gray-50 dark:border-gray-700 dark:text-gray-300 dark:hover:bg-gray-800'"
											@click="setCLIModel(row.spec, m)">{{ m ? m.slice(row.name.length + 1) : "default model" }}</button>
									</div>
								</li>
							</ol>
							<div class="flex flex-wrap items-center gap-2 text-xs text-gray-500 dark:text-gray-400">
								<span>Each CLI comes from an agent definition. Jobs can also run on an Aviary agent, listed as <code class="font-mono">aviary/&lt;agent&gt;</code>.</span>
								<select v-if="unusedAgents.length" class="rounded-md border border-gray-300 px-2 py-1 text-xs dark:border-gray-700 dark:bg-gray-900"
									aria-label="Add an Aviary agent" @change="addAgent($event)">
									<option value="">Add an Aviary agent…</option>
									<option v-for="a in unusedAgents" :key="a" :value="a">{{ a }}</option>
								</select>
							</div>
						</div>
					</section>

					<section id="clones" :class="CARD">
						<h2 :class="CARD_HEAD">Clones <span :class="CARD_NOTE">Each job works in its own clone, shared across projects</span></h2>
						<div class="p-5">
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
									<span class="text-xs" :class="c.inUse ? 'text-amber-700 dark:text-amber-400' : 'text-gray-500'" :title="c.reason">
										{{ c.inUse ? "in use" : "unused" }}
									</span>
								</li>
							</ul>
							<button v-if="clones.clones.value.some((c) => !c.inUse)" type="button" :disabled="clones.clearing.value"
								class="mt-3 rounded-lg border border-gray-300 px-3 py-1.5 text-sm text-gray-700 hover:bg-gray-50 disabled:opacity-50 dark:border-gray-700 dark:text-gray-200 dark:hover:bg-gray-800"
								@click="clones.clear()">{{ clones.clearing.value ? "Clearing…" : "Clear unused clones" }}</button>
							<p v-if="clones.cleared.value" data-testid="clone-message" class="mt-2 text-xs text-green-700 dark:text-green-400">
								Removed {{ clones.cleared.value.removed }} {{ clones.cleared.value.removed === 1 ? "clone" : "clones" }},
								reclaimed {{ formatBytes(clones.cleared.value.reclaimed) }}<template v-if="clones.cleared.value.kept">, kept {{ clones.cleared.value.kept }} in use</template>.
							</p>
						</div>
					</section>

					<section id="yaml" :class="CARD">
						<h2 :class="CARD_HEAD">YAML <span :class="CARD_NOTE">Only what differs from defaults ends up in aviary.yaml</span></h2>
						<pre data-testid="project-yaml"
							class="overflow-x-auto rounded-b-xl bg-gray-50 px-5 py-4 font-mono text-xs text-gray-800 dark:bg-gray-900 dark:text-gray-200">{{ yaml || "# Every setting is a default, so nothing is written." }}</pre>
					</section>
				</div>
			</div>
		</div>
	</AppLayout>
</template>

<script setup lang="ts">
import { SwitchRoot, SwitchThumb } from "radix-vue";
import { computed, h, onMounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import AppLayout from "../components/AppLayout.vue";
import {
	useAddProject,
	useCLIProbe,
	useProjectClones,
} from "../composables/useProjectSettings";
import {
	DELIVERY_OPTIONS,
	defaultFields,
	defaultInterval,
	formatBytes,
	isListed,
	type ProjectSettings,
	type ProjectsSection,
	parseList,
	projectsYAML,
	settingsFromSection,
	sharedAgents,
	specModel,
	specName,
	withSettings,
} from "../lib/projectSettings";
import { encodeProject } from "../lib/projects";
import { useSettingsStore } from "../stores/settings";

const CARD =
	"scroll-mt-24 rounded-xl border border-gray-200 bg-white dark:border-gray-800 dark:bg-gray-950";
const CARD_HEAD =
	"flex flex-wrap items-baseline gap-x-2 border-b border-gray-200 px-5 py-3 text-base font-semibold text-gray-900 dark:border-gray-800 dark:text-white";
const CARD_NOTE = "text-sm font-normal text-gray-500 dark:text-gray-400";
const LABEL =
	"mb-1 flex items-center gap-2 text-xs font-medium text-gray-700 dark:text-gray-300";
const INPUT =
	"w-full rounded-lg border border-gray-300 px-3 py-2 text-sm focus:border-blue-500 focus:outline-none dark:border-gray-700 dark:bg-gray-900 dark:text-white";
const ICON_BTN =
	"rounded-md px-1.5 text-sm text-gray-500 hover:bg-gray-100 disabled:opacity-30 dark:hover:bg-gray-800";

const SECTIONS = [
	{ id: "general", label: "General" },
	{ id: "clis", label: "Coding CLIs" },
	{ id: "clones", label: "Clones" },
	{ id: "yaml", label: "YAML" },
];

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
		label: "Run coding CLIs without their sandboxes and permission checks",
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
const router = useRouter();
const store = useSettingsStore();
const probe = useCLIProbe();
const clones = useProjectClones();
const addProject = useAddProject();

const target = computed(() =>
	typeof route.params.project === "string" ? route.params.project : "",
);
const adding = computed(() => route.query.add === "1");
const boardLink = computed(() => `/projects/${encodeProject(target.value)}`);

const form = ref<ProjectSettings | null>(null);
const loaded = ref("");
const commentersText = ref("");
const filterText = ref("");
const tempDirsText = ref("");
const saving = ref(false);
const saved = ref(false);
const error = ref("");

const lines = (text: string) =>
	text
		.split("\n")
		.map((l) => l.trim())
		.filter(Boolean);

const section = computed<ProjectsSection>(() => store.config?.projects ?? {});

function reset() {
	const s = settingsFromSection(target.value, section.value);
	form.value = s;
	// Baseline from the same builder that saves, so key order never reads as a change.
	loaded.value = JSON.stringify(next.value);
	commentersText.value = s.commenters.join(", ");
	filterText.value = s.filter.join("\n");
	tempDirsText.value = s.tempDirs.join("\n");
}

onMounted(async () => {
	// Settings load straight from config; nothing waits on the CLI probe.
	if (!store.config) await store.fetchConfig();
	if (store.error) error.value = store.error;
	reset();
});
watch(target, reset);

// The section as it will be saved. A project joins `list` once a change is
// made on its page; one watched with defaults writes nothing.
const next = computed<ProjectsSection>(() => {
	if (!form.value) return section.value;
	const unlisted = (s: ProjectSettings) =>
		JSON.stringify(withSettings(section.value, s, false));
	const changed =
		unlisted(form.value) !==
		unlisted(settingsFromSection(target.value, section.value));
	return withSettings(
		section.value,
		form.value,
		isListed(section.value, target.value) || changed,
	);
});
const dirty = computed(
	() => !!form.value && JSON.stringify(next.value) !== loaded.value,
);
const yaml = computed(() => projectsYAML(next.value));
const defaults = computed(() =>
	form.value ? defaultFields(form.value) : new Set<keyof ProjectSettings>(),
);
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

const saveNote = computed(() => {
	if (saving.value) return "";
	if (adding.value)
		return "Nothing is written to aviary.yaml unless a setting differs from its default";
	if (saved.value && !dirty.value)
		return "Saved and applied to the running watch";
	return "Changes apply to the running watch right away";
});

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
const toggles = computed(() =>
	form.value?.delivery === "browser"
		? [...TOGGLES, ...BROWSER_TOGGLES]
		: TOGGLES,
);

function setToggle(key: ToggleKey, v: boolean) {
	if (form.value) form.value[key] = v;
}

function discard() {
	reset();
	error.value = "";
}

async function save() {
	if (!form.value || !store.config) return;
	saving.value = true;
	error.value = "";
	try {
		const listed = isListed(next.value, target.value);
		if (dirty.value) {
			await store.saveConfig({ ...store.config, projects: next.value });
			loaded.value = JSON.stringify(next.value);
		}
		if (adding.value) {
			// A project saved to the list is picked up by the config watcher;
			// one left at defaults is watched for this run only.
			if (!listed) await addProject(target.value);
			await router.replace(boardLink.value);
			return;
		}
		saved.value = true;
	} catch (e) {
		error.value = e instanceof Error ? e.message : String(e);
	} finally {
		saving.value = false;
	}
}

// ── Coding CLIs ─────────────────────────────────────────────────────────────

// Without its own order, the project uses the shared one.
const defaultSpecs = computed(() => sharedAgents(section.value));
const specs = computed(() =>
	form.value?.agents.length ? form.value.agents : defaultSpecs.value,
);

function setSpecs(list: string[]) {
	if (!form.value) return;
	const same =
		list.length === defaultSpecs.value.length &&
		list.every((s, i) => s === defaultSpecs.value[i]);
	form.value.agents = same ? [] : list;
}

const cliRows = computed(() => {
	const probed = probe.clis.value ?? [];
	const row = (spec: string, enabled: boolean) => {
		const name = specName(spec);
		const cli = probed.find((c) => c.name === name);
		const isAgent = name === "aviary";
		let status = "";
		if (isAgent) status = "Aviary agent";
		else if (cli && !cli.installed) status = "not installed";
		else if (cli)
			status = cli.signedIn
				? ["signed in", cli.quota].filter(Boolean).join(" · ")
				: cli.auth || "signed out";
		else if (probe.clis.value) status = "unknown CLI";
		return {
			spec,
			name: isAgent ? spec : name,
			enabled,
			model: specModel(spec),
			models: cli?.models ?? [],
			signedIn: isAgent || !!cli?.signedIn,
			status,
		};
	};
	const used = new Set(specs.value.map(specName));
	return [
		...specs.value.map((s) => row(s, true)),
		...probed.filter((c) => !used.has(c.name)).map((c) => row(c.name, false)),
	];
});

function toggleCLI(spec: string) {
	const cur = specs.value;
	setSpecs(cur.includes(spec) ? cur.filter((s) => s !== spec) : [...cur, spec]);
}

function setCLIModel(spec: string, model: string) {
	setSpecs(specs.value.map((s) => (s === spec ? model || specName(spec) : s)));
}

function moveCLI(i: number, dir: number) {
	const list = [...specs.value];
	const j = i + dir;
	if (j < 0 || j >= list.length) return;
	[list[i], list[j]] = [list[j], list[i]];
	setSpecs(list);
}

const unusedAgents = computed(() =>
	(store.config?.agents ?? [])
		.map((a) => `aviary/${a.name}`)
		.filter((spec) => !specs.value.includes(spec)),
);

function addAgent(e: Event) {
	const sel = e.target as HTMLSelectElement;
	if (sel.value) setSpecs([...specs.value, sel.value]);
	sel.value = "";
}
</script>
