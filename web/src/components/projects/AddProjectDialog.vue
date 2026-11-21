<template>
	<div class="fixed inset-0 z-50 flex items-start justify-center overflow-y-auto bg-black/40 px-3 py-6 sm:py-16"
		@click.self="emit('close')" @keydown.esc="emit('close')">
		<div role="dialog" aria-modal="true" aria-labelledby="add-project-title"
			class="w-full max-w-xl overflow-hidden rounded-2xl bg-white shadow-xl dark:bg-gray-900">
			<form class="px-5 pt-5 sm:px-6" @submit.prevent="start">
				<h2 id="add-project-title" class="text-lg font-semibold text-gray-900 dark:text-white">Watch a repository</h2>
				<p class="mt-1 text-sm text-gray-600 dark:text-gray-400">
					Issues that get the ready label are claimed, fixed on their own branch, and opened as pull requests.
				</p>

				<label for="add-project-target" class="mt-5 block text-xs font-medium text-gray-700 dark:text-gray-300">
					Repository or project board
				</label>
				<input id="add-project-target" ref="input" v-model="target" type="text" autocomplete="off" spellcheck="false"
					placeholder="owner/repo"
					class="mt-1 w-full rounded-lg border border-gray-300 px-3 py-2 font-mono text-sm focus:border-blue-500 focus:outline-none focus:ring-2 focus:ring-blue-200 dark:border-gray-700 dark:bg-gray-950 dark:text-white" />

				<section v-if="target.trim()" data-testid="add-project-checks" aria-live="polite"
					class="mt-4 rounded-xl border border-gray-200 bg-gray-50 px-4 py-3 dark:border-gray-800 dark:bg-gray-950">
					<p class="text-xs font-semibold uppercase tracking-wide text-gray-500">
						{{ checking ? "Checking…" : error ? "Can't watch this" : result?.ok ? "Ready to go" : "Needs attention" }}
					</p>
					<p v-if="error" class="mt-2 text-sm text-red-700 dark:text-red-300">{{ error }}</p>
					<ul v-else-if="result" class="mt-2 space-y-1.5 text-sm">
						<li v-for="c in result.checks" :key="c.name" :data-check="c.name" :data-status="c.status"
							class="flex items-start gap-2">
							<CheckIcon :status="c.status" />
							<span class="text-gray-700 dark:text-gray-200">
								<b v-if="c.name !== 'fork'" class="font-medium text-gray-900 dark:text-white">{{ checkLabel(c.name) }}:</b>
								{{ c.message }}
								<router-link v-if="c.name === 'skills'" to="/system/skills"
									class="text-blue-600 hover:underline dark:text-blue-400">Manage skills</router-link>
							</span>
						</li>
					</ul>
				</section>

				<label for="add-project-agent" class="mt-4 block text-xs font-medium text-gray-700 dark:text-gray-300">
					Agent that owns it
				</label>
				<select id="add-project-agent" v-model="agent" data-field="agent"
					class="mt-1 w-full rounded-lg border border-gray-300 px-3 py-2 text-sm focus:border-blue-500 focus:outline-none dark:border-gray-700 dark:bg-gray-950 dark:text-white">
					<option v-if="!agentNames.length" value="" disabled>No agents configured</option>
					<option v-for="a in agentNames" :key="a" :value="a">{{ a }}</option>
				</select>
				<p v-if="owner" data-testid="add-project-owned" class="mt-2 text-sm text-amber-700 dark:text-amber-400">
					{{ normalized }} already belongs to agent {{ owner }}.
				</p>

				<p class="mt-4 flex flex-wrap gap-x-4 gap-y-1 text-sm text-gray-600 dark:text-gray-400">
					<span>Up to <b class="text-gray-900 dark:text-white">{{ settings.concurrency }}</b> jobs at once</span>
					<span>{{ settings.merge ? "Merges when CI passes" : "Leaves pull requests for you to merge" }}</span>
					<span>Runs on <b class="text-gray-900 dark:text-white">{{ runsOn }}</b></span>
				</p>
				<p v-if="startError" class="mt-3 text-sm text-red-700 dark:text-red-300">{{ startError }}</p>

				<div class="-mx-5 mt-5 flex flex-wrap items-center gap-2 border-t border-gray-200 px-5 py-4 dark:border-gray-800 sm:-mx-6 sm:px-6">
					<button type="button" class="text-sm text-blue-600 hover:underline disabled:opacity-50 dark:text-blue-400"
						:disabled="!canStart" @click="changeSettings">Change settings first</button>
					<div class="ml-auto flex gap-2">
						<button type="button"
							class="rounded-lg border border-gray-300 px-4 py-2 text-sm text-gray-700 hover:bg-gray-50 dark:border-gray-700 dark:text-gray-200 dark:hover:bg-gray-800"
							@click="emit('close')">Cancel</button>
						<button type="submit" :disabled="!canStart || starting"
							class="rounded-lg bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700 disabled:opacity-50">
							{{ starting ? "Starting…" : result && !result.ok ? "Start anyway" : "Start watching" }}
						</button>
					</div>
				</div>
			</form>
			<footer class="border-t border-gray-200 bg-gray-50 px-5 py-3 text-xs text-gray-500 dark:border-gray-800 dark:bg-gray-950 dark:text-gray-400 sm:px-6">
				Same from a terminal:
				<code class="rounded border border-gray-200 bg-white px-1.5 py-0.5 font-mono text-gray-800 dark:border-gray-700 dark:bg-gray-900 dark:text-gray-200">aviary project add {{ normalized || "owner/repo" }} --agent {{ agent || "AGENT" }}</code>
				<p class="mt-1">The project is saved to {{ agent || "the agent" }}'s projects in aviary.yaml and managed in its settings.</p>
			</footer>
		</div>
	</div>
</template>

<script setup lang="ts">
import { computed, h, onMounted, ref } from "vue";
import { useRouter } from "vue-router";
import {
	useAddProject,
	useProjectCheck,
} from "../../composables/useProjectSettings";
import { useProviderStatus } from "../../composables/useProviderAuth";
import {
	type CheckStatus,
	checkLabel,
	jobModels,
	projectOwner,
	settingsFromSection,
} from "../../lib/projectSettings";
import { useSettingsStore } from "../../stores/settings";

// agent preselects the owning agent, as when adding from its GitHub Projects.
const props = defineProps<{ initial?: string; agent?: string }>();
const emit = defineEmits<{
	close: [];
	added: [target: string, agent: string];
}>();

const router = useRouter();
const store = useSettingsStore();
// The CLI providers, so a model such as claude/opus shows as its own spec.
const cliStatus = useProviderStatus({ refreshOnMount: false });
const cliNames = computed(() =>
	(cliStatus.clis.value ?? []).map((cli) => cli.name),
);
// The probe is slow, so it runs here only when nothing is cached yet.
if (cliStatus.clis.value === null) void cliStatus.refresh();
const target = ref(props.initial ?? "");
const input = ref<HTMLInputElement | null>(null);
const { result, checking, error } = useProjectCheck(target);
const addProject = useAddProject();
const starting = ref(false);
const startError = ref("");

onMounted(async () => {
	input.value?.focus();
	if (!store.config) await store.fetchConfig();
	if (!agent.value || !agentNames.value.includes(agent.value))
		agent.value = agentNames.value[0] ?? "";
});

const agentNames = computed(() =>
	(store.config?.agents ?? []).map((a) => a.name).filter(Boolean),
);
const agent = ref(props.agent || agentNames.value[0] || "");

// The checks normalize the target (URLs become owner/repo).
const normalized = computed(() => result.value?.target || target.value.trim());
// The owning agent's projects settings, and the models its jobs run on.
const settings = computed(() =>
	settingsFromSection(
		(store.config?.agents ?? []).find((a) => a.name === agent.value)?.projects,
	),
);
const runsOn = computed(() => {
	if (!agent.value) return "no agent";
	const specs = jobModels(agent.value, store.config, cliNames.value).map(
		(m) => m.spec,
	);
	return specs.length
		? `${agent.value}: ${specs.join(", ")}`
		: `${agent.value} (no model)`;
});
// A project belongs to exactly one agent.
const owner = computed(() => projectOwner(store.config, normalized.value));
const canStart = computed(
	() =>
		!!target.value.trim() &&
		!!agent.value &&
		!owner.value &&
		!checking.value &&
		!error.value &&
		!!result.value,
);

async function start() {
	if (!canStart.value) return;
	starting.value = true;
	startError.value = "";
	try {
		await addProject(normalized.value, agent.value);
		// The config watcher reloads the server; refresh the copy here so the
		// board shows the new project's agent.
		await store.fetchConfig();
		emit("added", normalized.value, agent.value);
	} catch (e) {
		startError.value = e instanceof Error ? e.message : String(e);
	} finally {
		starting.value = false;
	}
}

function changeSettings() {
	emit("close");
	void router.push(
		`/agents/${encodeURIComponent(agent.value)}/projects?add=${encodeURIComponent(normalized.value)}`,
	);
}

const ICONS: Record<CheckStatus, { cls: string; d: string }> = {
	ok: { cls: "text-green-600", d: "M5 10.5l3.5 3.5L15 7" },
	warn: { cls: "text-amber-600", d: "M10 6v5m0 3h.01" },
	fail: { cls: "text-red-600", d: "M6.5 6.5l7 7m0-7l-7 7" },
};

function CheckIcon(p: { status: CheckStatus }) {
	const icon = ICONS[p.status];
	return h(
		"svg",
		{
			viewBox: "0 0 20 20",
			fill: "none",
			stroke: "currentColor",
			"stroke-width": 2,
			"stroke-linecap": "round",
			class: `mt-0.5 h-4 w-4 shrink-0 ${icon.cls}`,
			"aria-label": p.status,
		},
		[
			p.status === "ok"
				? null
				: h("circle", { cx: 10, cy: 10, r: 8, "stroke-width": 1.5 }),
			h("path", { d: icon.d }),
		],
	);
}
CheckIcon.props = ["status"];
</script>
