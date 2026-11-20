<template>
  <div class="space-y-6 pb-8">
    <div class="flex flex-wrap items-start justify-between gap-4">
      <p class="max-w-3xl text-sm leading-6 text-gray-600 dark:text-gray-300">
        These skills do the work in Projects. Aviary decides when to run one and in which CLI, then the skill handles the rest:
        branch, pull request, tests, changelog, CI and merge. They're installed into each CLI, so they work without Aviary too.
      </p>
      <div class="flex flex-wrap items-center gap-3">
        <span class="text-xs text-gray-500 dark:text-gray-400">
          Source: <code class="font-mono text-gray-700 dark:text-gray-300">{{ status?.source ?? "lsegal/aviary" }}</code> on skills.sh
        </span>
        <button
          type="button"
          class="rounded-lg border border-gray-200 bg-white px-4 py-2 text-sm text-gray-700 hover:bg-gray-100 disabled:opacity-50 dark:border-gray-700 dark:bg-gray-900 dark:text-gray-300 dark:hover:bg-gray-800"
          :disabled="loading"
          @click="load"
        >
          {{ loading ? "Checking..." : "Check for updates" }}
        </button>
        <button
          type="button"
          data-testid="coding-skills-update-all"
          class="rounded-lg bg-blue-600 px-4 py-2 text-sm font-semibold text-white hover:bg-blue-500 disabled:opacity-50"
          :disabled="!status?.outdated || busy !== ''"
          @click="install({ update_only: true }, 'all')"
        >
          {{ busy === "all" ? "Updating..." : `Update all (${status?.outdated ?? 0})` }}
        </button>
      </div>
    </div>

    <div
      v-if="errorMessage"
      class="rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700 dark:border-red-900 dark:bg-red-950 dark:text-red-300"
    >
      {{ errorMessage }}
    </div>
    <div
      v-if="okMessage"
      class="rounded-lg border border-emerald-200 bg-emerald-50 px-4 py-3 text-sm text-emerald-700 dark:border-emerald-900/50 dark:bg-emerald-950/20 dark:text-emerald-300"
    >
      {{ okMessage }}
    </div>
    <p
      v-if="status && !status.release"
      class="rounded-lg border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-800 dark:border-amber-900/50 dark:bg-amber-950/20 dark:text-amber-300"
    >
      This is a development build, so skills install from the copies bundled with it and are not updated automatically.
    </p>

    <Skeleton v-if="loading && !status" class="h-64 w-full rounded-2xl" />

    <div
      v-else-if="status && !status.clis.length"
      class="rounded-2xl border border-dashed border-gray-300 bg-white/80 px-5 py-10 text-center text-sm text-gray-500 dark:border-gray-700 dark:bg-gray-900/80 dark:text-gray-400"
    >
      No CLI providers were found on the PATH. Install Claude Code, Codex, Gemini CLI or another supported CLI, then check again.
    </div>

    <div
      v-else-if="status"
      class="overflow-x-auto rounded-2xl border border-gray-200 bg-white shadow-sm dark:border-gray-800 dark:bg-gray-900"
    >
      <table class="w-full min-w-[640px] text-left text-sm">
        <thead class="border-b border-gray-200 bg-gray-50 text-[11px] font-semibold uppercase tracking-[0.12em] text-gray-500 dark:border-gray-800 dark:bg-gray-950 dark:text-gray-400">
          <tr>
            <th class="px-4 py-3">Skill</th>
            <th v-for="cli in status.clis" :key="cli.id" class="px-4 py-3">{{ cli.name }}</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-gray-200 dark:divide-gray-800">
          <tr v-for="skill in status.skills" :key="skill.name" :data-testid="`coding-skill-${skill.name}`" class="align-top">
            <td class="max-w-md px-4 py-4">
              <div class="flex flex-wrap items-center gap-2">
                <code class="font-mono text-[15px] font-semibold text-gray-900 dark:text-white">{{ skill.name }}</code>
                <span
                  v-if="skill.run_by_projects"
                  class="rounded-full bg-blue-50 px-2 py-0.5 text-[11px] font-medium text-blue-700 dark:bg-blue-950/50 dark:text-blue-300"
                >run by Projects</span>
                <span
                  v-else
                  class="rounded-full bg-gray-100 px-2 py-0.5 text-[11px] font-medium text-gray-600 dark:bg-gray-800 dark:text-gray-300"
                >you run it</span>
              </div>
              <p class="mt-1.5 leading-6 text-gray-600 dark:text-gray-300">{{ skill.summary }}</p>
            </td>
            <td
              v-for="entry in skill.installs"
              :key="entry.cli"
              class="px-4 py-4"
              :data-testid="`coding-skill-${skill.name}-${entry.cli}`"
            >
              <template v-if="entry.installed">
                <span
                  class="inline-flex items-center gap-1.5"
                  :class="entry.current ? 'text-emerald-700 dark:text-emerald-400' : 'text-amber-700 dark:text-amber-400'"
                  :title="entry.path"
                >
                  <span class="h-2 w-2 rounded-full" :class="entry.current ? 'bg-emerald-600' : 'bg-amber-600'" />
                  {{ entry.version || "Older copy" }}
                </span>
                <button
                  v-if="!entry.current"
                  type="button"
                  class="mt-2 block rounded-md border border-gray-300 bg-white px-3 py-1.5 text-xs text-gray-800 hover:bg-gray-100 disabled:opacity-50 dark:border-gray-700 dark:bg-gray-900 dark:text-gray-200 dark:hover:bg-gray-800"
                  :disabled="busy !== ''"
                  @click="install({ skill: skill.name, cli: entry.cli }, cellKey(skill.name, entry.cli))"
                >
                  {{ busy === cellKey(skill.name, entry.cli) ? "Updating..." : `Update to ${status.version}` }}
                </button>
              </template>
              <template v-else>
                <span class="text-gray-500 dark:text-gray-400">Not installed</span>
                <button
                  type="button"
                  class="mt-2 block rounded-md border border-gray-300 bg-white px-3 py-1.5 text-xs text-gray-800 hover:bg-gray-100 disabled:opacity-50 dark:border-gray-700 dark:bg-gray-900 dark:text-gray-200 dark:hover:bg-gray-800"
                  :disabled="busy !== ''"
                  @click="install({ skill: skill.name, cli: entry.cli }, cellKey(skill.name, entry.cli))"
                >
                  {{ busy === cellKey(skill.name, entry.cli) ? "Installing..." : "Install" }}
                </button>
              </template>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <div class="grid gap-4 lg:grid-cols-2">
      <section class="rounded-2xl border border-gray-200 bg-white p-5 shadow-sm dark:border-gray-800 dark:bg-gray-900">
        <h3 class="text-base font-semibold text-gray-900 dark:text-white">Keeping them current</h3>
        <label class="mt-3 flex items-start gap-3 text-sm text-gray-700 dark:text-gray-300">
          <input
            type="checkbox"
            class="mt-0.5 h-4 w-4 accent-blue-600"
            data-testid="coding-skills-auto-update"
            :checked="autoUpdate"
            :disabled="savingSettings"
            @change="saveSetting('auto_update', ($event.target as HTMLInputElement).checked)"
          />
          <span>Update skills whenever Aviary updates, so each release runs with the skills it was tested against</span>
        </label>
        <label class="mt-3 flex items-start gap-3 text-sm text-gray-700 dark:text-gray-300">
          <input
            type="checkbox"
            class="mt-0.5 h-4 w-4 accent-blue-600"
            data-testid="coding-skills-install-new"
            :checked="installNewCLIs"
            :disabled="savingSettings"
            @change="saveSetting('install_new_clis', ($event.target as HTMLInputElement).checked)"
          />
          <span>Install for any CLI provider I set up later</span>
        </label>
        <p class="mt-3 text-xs leading-5 text-gray-500 dark:text-gray-400">
          Running jobs keep the version they started with.
        </p>
      </section>

      <section class="rounded-2xl border border-gray-200 bg-white p-5 shadow-sm dark:border-gray-800 dark:bg-gray-900">
        <h3 class="text-base font-semibold text-gray-900 dark:text-white">Use them without Aviary</h3>
        <p class="mt-3 text-sm text-gray-600 dark:text-gray-300">Install once:</p>
        <pre class="mt-2 overflow-x-auto rounded-lg border border-gray-200 bg-gray-50 px-3 py-2.5 font-mono text-xs text-gray-800 dark:border-gray-800 dark:bg-gray-950 dark:text-gray-200">{{ installCommand }}</pre>
        <p class="mt-3 text-sm text-gray-600 dark:text-gray-300">Then, in the CLI:</p>
        <pre class="mt-2 overflow-x-auto rounded-lg border border-gray-200 bg-gray-50 px-3 py-2.5 font-mono text-xs text-gray-800 dark:border-gray-800 dark:bg-gray-950 dark:text-gray-200">/gh-fix owner/repo#123</pre>
      </section>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { useMCP } from "../../composables/useMCP";
import type { AppConfig, CodingSkillsConfig } from "../../stores/settings";
import { useSettingsStore } from "../../stores/settings";
import { Skeleton } from "../ui/skeleton";

interface CLIProvider {
	id: string;
	name: string;
	target: string;
}

interface CodingSkillInstall {
	cli: string;
	installed: boolean;
	current: boolean;
	version?: string;
	source?: string;
	path?: string;
}

interface CodingSkill {
	name: string;
	summary: string;
	run_by_projects: boolean;
	installs: CodingSkillInstall[];
}

interface CodingSkillsStatus {
	version: string;
	source: string;
	release: boolean;
	clis: CLIProvider[];
	skills: CodingSkill[];
	outdated: number;
}

interface InstallArgs {
	skill?: string;
	cli?: string;
	update_only?: boolean;
}

const { callTool } = useMCP();
const store = useSettingsStore();

const status = ref<CodingSkillsStatus | null>(null);
const loading = ref(false);
const busy = ref("");
const savingSettings = ref(false);
const errorMessage = ref("");
const okMessage = ref("");

const autoUpdate = computed(
	() => store.config?.coding_skills?.auto_update !== false,
);
const installNewCLIs = computed(
	() => store.config?.coding_skills?.install_new_clis !== false,
);

const installCommand = computed(() => {
	const target = status.value?.clis[0]?.target ?? "claude-code";
	return `npx skills add ${status.value?.source ?? "lsegal/aviary"}@gh-fix --global --agent ${target} -y`;
});

function cellKey(skill: string, cli: string): string {
	return `${skill}/${cli}`;
}

async function load() {
	loading.value = true;
	errorMessage.value = "";
	try {
		const [raw] = await Promise.all([
			callTool("coding_skills_list"),
			store.config ? Promise.resolve() : store.fetchConfig(),
		]);
		status.value = JSON.parse(raw) as CodingSkillsStatus;
	} catch (error) {
		errorMessage.value = error instanceof Error ? error.message : String(error);
	} finally {
		loading.value = false;
	}
}

async function install(args: InstallArgs, key: string) {
	busy.value = key;
	errorMessage.value = "";
	okMessage.value = "";
	try {
		const raw = await callTool(
			"coding_skills_install",
			args as Record<string, unknown>,
		);
		const result = JSON.parse(raw) as { installed: string[] };
		okMessage.value = result.installed.length
			? `Installed ${result.installed.join(", ")}.`
			: "Everything is already up to date.";
	} catch (error) {
		errorMessage.value = error instanceof Error ? error.message : String(error);
	} finally {
		busy.value = "";
		await load();
	}
}

async function saveSetting(key: keyof CodingSkillsConfig, value: boolean) {
	if (!store.config) await store.fetchConfig();
	if (!store.config) return;
	const next = JSON.parse(JSON.stringify(store.config)) as AppConfig;
	next.coding_skills = { ...(next.coding_skills ?? {}), [key]: value };
	savingSettings.value = true;
	errorMessage.value = "";
	try {
		await store.saveConfig(next);
	} catch (error) {
		errorMessage.value = error instanceof Error ? error.message : String(error);
	} finally {
		savingSettings.value = false;
	}
}

onMounted(() => {
	void load();
});
</script>
