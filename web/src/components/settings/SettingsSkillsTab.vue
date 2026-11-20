<template>
				<section v-show="activeTab === 'skills'" class="space-y-5 pb-8">
					<section aria-labelledby="skills-in-providers">
						<h3 id="skills-in-providers" class="mb-3 text-sm font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">
							Installed in providers</h3>
						<CodingSkillsPanel v-if="activeTab === 'skills'" />
					</section>

					<section aria-labelledby="skills-for-agents" class="space-y-5">
					<div class="flex items-center justify-between">
						<div>
							<h3 id="skills-for-agents" class="text-sm font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">
								Available to Aviary agents</h3>
							<p class="mt-1 text-xs text-gray-500 dark:text-gray-400">Bundled skills come from the Aviary binary.
								Disk-installed skills come from <code class="font-mono">AVIARY_CONFIG_BASE_DIR/skills</code> and
								<code class="font-mono">~/.agents/skills</code>.</p>
							<p class="mt-2 text-xs text-gray-500 dark:text-gray-400">Search with <code class="font-mono">npx skills find</code>
								or browse <a href="https://skills.sh/" target="_blank" rel="noreferrer"
									class="underline underline-offset-2">skills.sh</a>. Example install:
								<code class="font-mono">npx skills add --global -a universal 4ier/notion-cli</code>.</p>
						</div>
						<button type="button"
							class="rounded-lg border border-gray-200 px-3 py-1.5 text-xs text-gray-700 hover:bg-gray-100 dark:border-gray-700 dark:text-gray-300 dark:hover:bg-gray-800"
							:disabled="skillsLoading" @click="loadInstalledSkills">
							{{ skillsLoading ? "Loading…" : "Refresh Skills" }}
						</button>
					</div>

					<div v-if="installedSkills.length" data-testid="skill-filters"
						class="flex flex-col gap-3 rounded-xl border border-gray-200 bg-white p-4 dark:border-gray-800 dark:bg-gray-900">
						<input v-model="skillSearch" type="search" placeholder="Search installed skills" class="field-input" />
						<div class="flex flex-wrap items-center justify-between gap-3 text-xs">
							<div class="flex items-center gap-1" role="group" aria-label="Status">
								<button v-for="f in skillStatusFilters" :key="f.value" type="button" :aria-pressed="skillStatusFilter === f.value"
									:class="skillStatusFilter === f.value ? SKILL_FILTER_ON : SKILL_FILTER_OFF" @click="skillStatusFilter = f.value">
									{{ f.label }}
								</button>
							</div>
							<div class="flex items-center gap-1" role="group" aria-label="Source">
								<button v-for="f in skillSourceFilters" :key="f.value" type="button" :aria-pressed="skillSourceFilter === f.value"
									:class="skillSourceFilter === f.value ? SKILL_FILTER_ON : SKILL_FILTER_OFF" @click="skillSourceFilter = f.value">
									{{ f.label }}
								</button>
							</div>
							<span class="text-gray-500 dark:text-gray-400">{{ enabledSkillCount }} of {{ installedSkills.length }} enabled · showing {{ filteredSkills.length }}</span>
						</div>
					</div>

					<div v-if="!filteredSkills.length"
						class="rounded-xl border border-gray-200 bg-white px-4 py-3 text-sm text-gray-500 dark:border-gray-800 dark:bg-gray-900 dark:text-gray-400">
						{{ installedSkills.length ? "No skills match." : "No installed skills found." }}
					</div>

					<div v-for="skill in filteredSkills" :key="skill.name"
						class="space-y-4 rounded-xl border border-gray-200 bg-white p-5 dark:border-gray-800 dark:bg-gray-900">
						<div class="grid gap-3 md:grid-cols-[minmax(0,1fr)_auto] md:items-start">
							<div class="min-w-0">
								<div class="flex flex-wrap items-center gap-2">
									<h4 class="text-base font-semibold text-gray-900 dark:text-white">{{ skill.name }}</h4>
									<span
										class="rounded-full bg-gray-100 px-2 py-0.5 text-[11px] font-medium uppercase tracking-wide text-gray-600 dark:bg-gray-800 dark:text-gray-300">{{ skill.source }}</span>
									<span
										:class="skillConfig(skill.name).enabled ? 'bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-300' : 'bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-300'"
										class="rounded-full px-2 py-0.5 text-[11px] font-medium uppercase tracking-wide">
										{{ skillConfig(skill.name).enabled ? "enabled" : "disabled" }}
									</span>
								</div>
								<p v-if="skill.description" class="mt-1 text-sm text-gray-600 dark:text-gray-400">
									{{ skill.description }}
								</p>
								<p class="mt-1 font-mono text-[11px] text-gray-400 dark:text-gray-500">{{ skill.path }}</p>
							</div>

							<label
								class="inline-flex cursor-pointer items-center gap-2 self-start pt-0.5 text-sm text-gray-700 md:justify-self-end dark:text-gray-300">
								<input v-model="skillConfig(skill.name).enabled" type="checkbox"
									class="h-4 w-4 rounded border-gray-300 text-blue-600 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-800" />
								Enabled
							</label>
						</div>

						<div v-if="skillSettingEntries(skill).length" class="grid gap-4 lg:grid-cols-2">
							<div v-for="[key, schema] in skillSettingEntries(skill)" :key="`${skill.name}-${key}`">
								<label class="field-label">{{ skillSettingLabel(key, schema) }}</label>
								<input v-if="skillSettingInputKind(schema) === 'string'" :value="skillStringSetting(skill.name, key)"
									type="text" class="field-input" :placeholder="skillSettingPlaceholder(schema)"
									@input="setSkillStringSetting(skill.name, key, $event)" />
								<input v-else :value="skillArraySetting(skill.name, key)" type="text" class="field-input"
									:placeholder="skillSettingPlaceholder(schema)"
									@input="setSkillArraySetting(skill.name, key, $event)" />
								<p v-if="typeof schema.description === 'string' && schema.description"
									class="mt-1 text-xs text-gray-400 dark:text-gray-500">
									{{ schema.description }}
								</p>
							</div>
						</div>
					</div>
					</section>
				</section>
</template>

<script lang="ts">
import { computed, defineComponent, inject, ref } from "vue";
import CodingSkillsPanel from "../skills/CodingSkillsPanel.vue";
import { settingsViewContextKey } from "./context";

interface Skill {
	name: string;
	description?: string;
	path: string;
	source: string;
}

type StatusFilter = "all" | "enabled" | "disabled";
type SourceFilter = "all" | "builtin" | "disk";

export default defineComponent({
	name: "SettingsSkillsTab",
	components: { CodingSkillsPanel },
	setup() {
		const settings = inject(settingsViewContextKey);
		if (!settings) {
			throw new Error("Settings view context is not available.");
		}
		const skillSearch = ref("");
		const skillStatusFilter = ref<StatusFilter>("all");
		const skillSourceFilter = ref<SourceFilter>("all");
		const enabled = (name: string) =>
			Boolean(settings.skillConfig(name).enabled);
		const enabledSkillCount = computed(
			() =>
				(settings.installedSkills as Skill[]).filter((s) => enabled(s.name))
					.length,
		);
		// Enabled skills first, then by name, narrowed by the search and filters.
		const filteredSkills = computed(() => {
			const term = skillSearch.value.trim().toLowerCase();
			return (settings.installedSkills as Skill[])
				.filter((skill) => {
					if (skillStatusFilter.value !== "all") {
						if (enabled(skill.name) !== (skillStatusFilter.value === "enabled"))
							return false;
					}
					if (
						skillSourceFilter.value !== "all" &&
						skill.source !== skillSourceFilter.value
					)
						return false;
					return (
						!term ||
						[skill.name, skill.description ?? "", skill.path].some((v) =>
							v.toLowerCase().includes(term),
						)
					);
				})
				.sort((a, b) =>
					enabled(a.name) !== enabled(b.name)
						? enabled(a.name)
							? -1
							: 1
						: a.name.localeCompare(b.name),
				);
		});
		return {
			...settings,
			skillSearch,
			skillStatusFilter,
			skillSourceFilter,
			skillStatusFilters: [
				{ label: "All", value: "all" },
				{ label: "Enabled", value: "enabled" },
				{ label: "Disabled", value: "disabled" },
			] as { label: string; value: StatusFilter }[],
			skillSourceFilters: [
				{ label: "Any Source", value: "all" },
				{ label: "Built-in", value: "builtin" },
				{ label: "Disk", value: "disk" },
			] as { label: string; value: SourceFilter }[],
			enabledSkillCount,
			filteredSkills,
			SKILL_FILTER_ON:
				"rounded-full bg-blue-600 px-3 py-1 font-semibold text-white",
			SKILL_FILTER_OFF:
				"rounded-full px-3 py-1 font-medium text-gray-600 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-gray-800",
		};
	},
});
</script>

