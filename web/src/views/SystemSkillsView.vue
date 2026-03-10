<template>
  <AppLayout>
    <div class="h-full overflow-y-auto bg-[radial-gradient(circle_at_top_left,_rgba(16,185,129,0.08),_transparent_28%),linear-gradient(to_bottom,_rgba(255,255,255,0.96),_rgba(249,250,251,1))] dark:bg-[radial-gradient(circle_at_top_left,_rgba(16,185,129,0.14),_transparent_26%),linear-gradient(to_bottom,_rgba(3,7,18,0.96),_rgba(3,7,18,1))]">
      <div class="mx-auto max-w-7xl px-4 py-6 sm:px-6">
        <div class="mb-6 flex flex-wrap items-start justify-between gap-4">
          <div>
            <h2 class="text-xl font-bold text-gray-900 dark:text-white">Skill Marketplace</h2>
            <p class="mt-1 max-w-3xl text-sm text-gray-500 dark:text-gray-400">
              Browse installed skills, filter by activation state, and enable or disable them without leaving the system menu.
            </p>
          </div>
          <button
            type="button"
            class="rounded-lg border border-gray-200 bg-white px-4 py-2 text-sm text-gray-700 hover:bg-gray-100 disabled:opacity-50 dark:border-gray-700 dark:bg-gray-900 dark:text-gray-300 dark:hover:bg-gray-800"
            :disabled="loading"
            @click="loadPage"
          >
            {{ loading ? "Refreshing…" : "Refresh" }}
          </button>
        </div>

        <div v-if="errorMessage" class="mb-4 rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700 dark:border-red-900 dark:bg-red-950 dark:text-red-300">
          {{ errorMessage }}
        </div>
        <div v-if="okMessage" class="mb-4 rounded-lg border border-emerald-200 bg-emerald-50 px-4 py-3 text-sm text-emerald-700 dark:border-emerald-900/50 dark:bg-emerald-950/20 dark:text-emerald-300">
          {{ okMessage }}
        </div>

        <div class="mb-6 grid gap-3 sm:grid-cols-3">
          <div class="rounded-2xl border border-gray-200 bg-white/90 p-4 backdrop-blur dark:border-gray-800 dark:bg-gray-900/90">
            <p class="text-[11px] font-semibold uppercase tracking-[0.18em] text-gray-400 dark:text-gray-500">Installed</p>
            <p class="mt-2 text-3xl font-semibold text-gray-900 dark:text-white">{{ installedSkills.length }}</p>
          </div>
          <div class="rounded-2xl border border-emerald-200 bg-emerald-50/80 p-4 backdrop-blur dark:border-emerald-900/50 dark:bg-emerald-950/20">
            <p class="text-[11px] font-semibold uppercase tracking-[0.18em] text-emerald-600 dark:text-emerald-400">Enabled</p>
            <p class="mt-2 text-3xl font-semibold text-emerald-700 dark:text-emerald-300">{{ enabledCount }}</p>
          </div>
          <div class="rounded-2xl border border-amber-200 bg-amber-50/80 p-4 backdrop-blur dark:border-amber-900/50 dark:bg-amber-950/20">
            <p class="text-[11px] font-semibold uppercase tracking-[0.18em] text-amber-600 dark:text-amber-400">Showing</p>
            <p class="mt-2 text-3xl font-semibold text-amber-700 dark:text-amber-300">{{ filteredSkills.length }}</p>
          </div>
        </div>

        <div class="mb-6 rounded-3xl border border-gray-200 bg-white/90 p-4 shadow-sm backdrop-blur dark:border-gray-800 dark:bg-gray-900/90">
          <div class="space-y-4">
            <input
              v-model="search"
              type="search"
              placeholder="Search installed skills"
              class="w-full min-w-0 rounded-xl border border-gray-200 bg-gray-50 px-4 py-2.5 text-sm text-gray-900 placeholder-gray-400 focus:border-emerald-500 focus:outline-none dark:border-gray-700 dark:bg-gray-950 dark:text-white dark:placeholder-gray-500"
            />

            <div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
              <div class="flex items-center gap-3">
                <span class="text-[11px] font-semibold uppercase tracking-[0.18em] text-gray-400 dark:text-gray-500">Status</span>
                <div class="inline-flex flex-nowrap rounded-full border border-gray-200 bg-gray-50 p-1 dark:border-gray-700 dark:bg-gray-950">
                  <button
                    v-for="filter in statusFilters"
                    :key="filter.value"
                    type="button"
                    :class="statusFilter === filter.value ? activeFilterClass : inactiveFilterClass"
                    @click="statusFilter = filter.value"
                  >
                    {{ filter.label }}
                  </button>
                </div>
              </div>

              <div class="flex items-center gap-3 sm:justify-end">
                <span class="text-[11px] font-semibold uppercase tracking-[0.18em] text-gray-400 dark:text-gray-500">Source</span>
                <div class="inline-flex flex-nowrap rounded-full border border-gray-200 bg-gray-50 p-1 dark:border-gray-700 dark:bg-gray-950">
                  <button
                    v-for="filter in sourceFilters"
                    :key="filter.value"
                    type="button"
                    :class="sourceFilter === filter.value ? activeFilterClass : inactiveFilterClass"
                    @click="sourceFilter = filter.value"
                  >
                    {{ filter.label }}
                  </button>
                </div>
              </div>
            </div>
          </div>
        </div>

        <div v-if="!filteredSkills.length" class="rounded-2xl border border-dashed border-gray-300 bg-white/80 px-5 py-10 text-center text-sm text-gray-500 dark:border-gray-700 dark:bg-gray-900/80 dark:text-gray-400">
          No skills match the current filters.
        </div>

        <section v-else class="grid gap-4 pb-8 xl:grid-cols-2">
          <article
            v-for="skill in filteredSkills"
            :key="skill.name"
            class="rounded-3xl border border-gray-200 bg-white/95 p-5 shadow-sm transition-colors dark:border-gray-800 dark:bg-gray-900/95"
          >
            <div class="flex flex-wrap items-start justify-between gap-4">
              <div class="min-w-0 flex-1">
                <div class="flex flex-wrap items-center gap-2">
                  <h3 class="text-base font-semibold text-gray-900 dark:text-white">{{ skill.name }}</h3>
                  <span class="rounded-full bg-gray-100 px-2.5 py-0.5 text-[11px] font-medium uppercase tracking-wide text-gray-600 dark:bg-gray-800 dark:text-gray-300">
                    {{ skill.source }}
                  </span>
                  <span
                    :class="isEnabled(skill.name) ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300' : 'bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-300'"
                    class="rounded-full px-2.5 py-0.5 text-[11px] font-medium uppercase tracking-wide"
                  >
                    {{ isEnabled(skill.name) ? "enabled" : "disabled" }}
                  </span>
                </div>
                <p class="mt-2 text-sm leading-6 text-gray-600 dark:text-gray-300">
                  {{ skill.description?.trim() || "No description available for this skill." }}
                </p>
              </div>

              <button
                type="button"
                class="min-w-28 rounded-xl px-4 py-2 text-sm font-semibold transition-colors disabled:opacity-50"
                :class="isEnabled(skill.name)
                  ? 'bg-gray-900 text-white hover:bg-gray-700 dark:bg-white dark:text-gray-900 dark:hover:bg-gray-200'
                  : 'bg-emerald-600 text-white hover:bg-emerald-500'"
                :disabled="Boolean(savingByName[skill.name])"
                @click="toggleSkill(skill.name, !isEnabled(skill.name))"
              >
                {{ savingByName[skill.name] ? "Saving…" : isEnabled(skill.name) ? "Disable" : "Enable" }}
              </button>
            </div>

            <div class="mt-4 grid gap-3 rounded-2xl border border-gray-100 bg-gray-50/80 p-4 text-sm dark:border-gray-800 dark:bg-gray-950/70">
              <div class="flex items-center justify-between gap-3">
                <span class="text-xs font-semibold uppercase tracking-[0.18em] text-gray-400 dark:text-gray-500">Location</span>
                <code class="truncate text-right text-[11px] text-gray-500 dark:text-gray-400">{{ skill.path }}</code>
              </div>
              <div class="flex flex-wrap items-center justify-between gap-3">
                <span class="text-xs font-semibold uppercase tracking-[0.18em] text-gray-400 dark:text-gray-500">State</span>
                <span class="text-sm text-gray-600 dark:text-gray-300">
                  {{ isEnabled(skill.name) ? "Available to agents when selected" : "Installed but not activated" }}
                </span>
              </div>
            </div>
          </article>
        </section>
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import AppLayout from "../components/AppLayout.vue";
import { useMCP } from "../composables/useMCP";
import type { AppConfig, SkillConfig } from "../stores/settings";
import { useSettingsStore } from "../stores/settings";

interface InstalledSkill {
	name: string;
	description: string;
	path: string;
	source: string;
	enabled: boolean;
}

type StatusFilter = "all" | "enabled" | "disabled";
type SourceFilter = "all" | "builtin" | "disk";

const activeFilterClass =
	"whitespace-nowrap rounded-full bg-emerald-600 px-3 py-1.5 text-xs font-semibold text-white";
const inactiveFilterClass =
	"whitespace-nowrap rounded-full px-3 py-1.5 text-xs font-medium text-gray-600 hover:bg-white dark:text-gray-300 dark:hover:bg-gray-900";

const statusFilters: Array<{ label: string; value: StatusFilter }> = [
	{ label: "All", value: "all" },
	{ label: "Enabled", value: "enabled" },
	{ label: "Disabled", value: "disabled" },
];

const sourceFilters: Array<{ label: string; value: SourceFilter }> = [
	{ label: "Any Source", value: "all" },
	{ label: "Built-in", value: "builtin" },
	{ label: "Disk", value: "disk" },
];

const { callTool } = useMCP();
const store = useSettingsStore();

const loading = ref(false);
const errorMessage = ref("");
const okMessage = ref("");
const search = ref("");
const statusFilter = ref<StatusFilter>("all");
const sourceFilter = ref<SourceFilter>("all");
const installedSkills = ref<InstalledSkill[]>([]);
const currentConfig = ref<AppConfig | null>(null);
const savingByName = ref<Record<string, boolean>>({});

function cloneConfig(config: AppConfig): AppConfig {
	return JSON.parse(JSON.stringify(config)) as AppConfig;
}

function emptyConfig(): AppConfig {
	return {
		server: {
			port: 16677,
			tls: { cert: "", key: "" },
			external_access: false,
			no_tls: false,
		},
		agents: [],
		models: { providers: {}, defaults: { model: "", fallbacks: [] } },
		browser: { binary: "", cdp_port: 0 },
		search: { web: { brave_api_key: "" } },
		scheduler: { concurrency: "" },
		skills: {},
	};
}

function skillConfig(name: string): SkillConfig {
	if (!currentConfig.value) {
		currentConfig.value = emptyConfig();
	}
	if (!currentConfig.value.skills[name]) {
		currentConfig.value.skills[name] = {};
	}
	return currentConfig.value.skills[name];
}

function isEmptySkillConfig(skill: SkillConfig | undefined): boolean {
	if (!skill) return true;
	return (
		!skill.enabled &&
		!(skill.binary ?? "").trim() &&
		(skill.allowed_commands?.length ?? 0) === 0 &&
		!(skill.timeout ?? "").trim() &&
		Object.keys(skill.env ?? {}).length === 0
	);
}

function isEnabled(name: string): boolean {
	return Boolean(skillConfig(name).enabled);
}

const enabledCount = computed(
	() => installedSkills.value.filter((skill) => isEnabled(skill.name)).length,
);

const filteredSkills = computed(() => {
	const term = search.value.trim().toLowerCase();
	return installedSkills.value
		.filter((skill) => {
			if (statusFilter.value === "enabled" && !isEnabled(skill.name))
				return false;
			if (statusFilter.value === "disabled" && isEnabled(skill.name))
				return false;
			if (sourceFilter.value !== "all" && skill.source !== sourceFilter.value) {
				return false;
			}
			if (!term) return true;
			return (
				skill.name.toLowerCase().includes(term) ||
				skill.description.toLowerCase().includes(term) ||
				skill.path.toLowerCase().includes(term)
			);
		})
		.sort((a, b) => {
			if (isEnabled(a.name) !== isEnabled(b.name)) {
				return isEnabled(a.name) ? -1 : 1;
			}
			return a.name.localeCompare(b.name);
		});
});

async function loadPage() {
	loading.value = true;
	errorMessage.value = "";
	okMessage.value = "";
	try {
		const [_, rawSkills] = await Promise.all([
			store.fetchConfig(),
			callTool("skills_list"),
		]);
		currentConfig.value = store.config
			? cloneConfig(store.config)
			: emptyConfig();
		installedSkills.value =
			(JSON.parse(rawSkills) as InstalledSkill[] | null)?.map((skill) => ({
				name: skill.name,
				description: skill.description ?? "",
				path: skill.path,
				source: skill.source,
				enabled: Boolean(skill.enabled),
			})) ?? [];
		for (const skill of installedSkills.value) {
			skillConfig(skill.name).enabled = Boolean(skill.enabled);
		}
	} catch (error) {
		errorMessage.value = error instanceof Error ? error.message : String(error);
		installedSkills.value = [];
	} finally {
		loading.value = false;
	}
}

async function toggleSkill(name: string, enabled: boolean) {
	if (!currentConfig.value) {
		await loadPage();
	}
	if (!currentConfig.value) return;

	const next = cloneConfig(currentConfig.value);
	next.skills[name] = {
		...(next.skills[name] ?? {}),
		enabled,
	};
	if (isEmptySkillConfig(next.skills[name])) {
		delete next.skills[name];
	}

	savingByName.value = { ...savingByName.value, [name]: true };
	errorMessage.value = "";
	okMessage.value = "";
	try {
		await store.saveConfig(next);
		currentConfig.value = cloneConfig(next);
		installedSkills.value = installedSkills.value.map((skill) =>
			skill.name === name ? { ...skill, enabled } : skill,
		);
		okMessage.value = `${name} ${enabled ? "enabled" : "disabled"}.`;
	} catch (error) {
		errorMessage.value = error instanceof Error ? error.message : String(error);
	} finally {
		savingByName.value = { ...savingByName.value, [name]: false };
	}
}

onMounted(() => {
	void loadPage();
});
</script>
