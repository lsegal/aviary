<template>
  <AppLayout>
    <template #header>
      <p class="truncate">
        Browse the provider/model pairs Aviary accepts for agent and default model selection: the built-in catalog plus the models of every installed CLI provider.
      </p>
    </template>
    <template #header-actions>
      <SystemSettingsLink />
    </template>
    <div class="min-h-0 flex-1 overflow-y-auto bg-[radial-gradient(circle_at_top_left,_rgba(14,165,233,0.08),_transparent_28%),linear-gradient(to_bottom,_rgba(255,255,255,0.96),_rgba(249,250,251,1))] dark:bg-[radial-gradient(circle_at_top_left,_rgba(14,165,233,0.15),_transparent_26%),linear-gradient(to_bottom,_rgba(3,7,18,0.96),_rgba(3,7,18,1))]">
      <div class="mx-auto max-w-7xl px-4 py-6 sm:px-6">
        <div class="mb-6 grid gap-3 sm:grid-cols-3">
          <div class="rounded-2xl border border-gray-200 bg-white/90 p-4 backdrop-blur dark:border-gray-800 dark:bg-gray-900/90">
            <p class="text-[11px] font-semibold uppercase tracking-[0.18em] text-gray-400 dark:text-gray-500">Models</p>
            <p data-testid="models-count" class="mt-2 text-3xl font-semibold text-gray-900 dark:text-white">{{ allModels.length }}</p>
          </div>
          <div class="rounded-2xl border border-sky-200 bg-sky-50/80 p-4 backdrop-blur dark:border-sky-900/50 dark:bg-sky-950/20">
            <p class="text-[11px] font-semibold uppercase tracking-[0.18em] text-sky-600 dark:text-sky-400">Providers</p>
            <p data-testid="providers-count" class="mt-2 text-3xl font-semibold text-sky-700 dark:text-sky-300">{{ providers.length }}</p>
          </div>
          <div class="rounded-2xl border border-amber-200 bg-amber-50/80 p-4 backdrop-blur dark:border-amber-900/50 dark:bg-amber-950/20">
            <p class="text-[11px] font-semibold uppercase tracking-[0.18em] text-amber-600 dark:text-amber-400">Text + Image</p>
            <p class="mt-2 text-3xl font-semibold text-amber-700 dark:text-amber-300">{{ multimodalCount }}</p>
          </div>
        </div>

        <div class="mb-6 rounded-3xl border border-gray-200 bg-white/90 p-4 shadow-sm backdrop-blur dark:border-gray-800 dark:bg-gray-900/90">
          <div class="space-y-4">
            <input
              v-model="search"
              type="search"
              placeholder="Search provider/model pairs"
              class="w-full min-w-0 rounded-xl border border-gray-200 bg-gray-50 px-4 py-2.5 text-sm text-gray-900 placeholder-gray-400 focus:border-sky-500 focus:outline-none dark:border-gray-700 dark:bg-gray-950 dark:text-white dark:placeholder-gray-500"
            />

            <div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
              <div class="flex flex-wrap items-center gap-3">
                <span class="text-[11px] font-semibold uppercase tracking-[0.18em] text-gray-400 dark:text-gray-500">Provider</span>
                <div class="inline-flex flex-wrap rounded-full border border-gray-200 bg-gray-50 p-1 dark:border-gray-700 dark:bg-gray-950">
                  <button
                    v-for="filter in providerFilters"
                    :key="filter.value"
                    type="button"
                    :data-provider-filter="filter.value"
                    :class="providerFilter === filter.value ? activeFilterClass : inactiveFilterClass"
                    @click="providerFilter = filter.value"
                  >
                    {{ filter.label }}
                  </button>
                </div>
              </div>
              <p class="text-xs text-gray-500 dark:text-gray-400">
                Showing {{ filteredModels.length }} models
              </p>
            </div>
          </div>
        </div>

        <div v-if="!filteredModels.length" class="rounded-2xl border border-dashed border-gray-300 bg-white/80 px-5 py-10 text-center text-sm text-gray-500 dark:border-gray-700 dark:bg-gray-900/80 dark:text-gray-400">
          No models match the current filters.
        </div>

        <section v-else class="grid gap-4 pb-8 md:grid-cols-2 xl:grid-cols-3">
          <article
            v-for="entry in filteredModels"
            :key="entry.id"
            :data-model="entry.id"
            class="rounded-3xl border border-gray-200 bg-white/95 p-5 shadow-sm transition-colors dark:border-gray-800 dark:bg-gray-900/95"
          >
            <div class="flex flex-wrap items-center gap-2">
              <span class="rounded-full bg-sky-100 px-2.5 py-0.5 text-[11px] font-medium uppercase tracking-wide text-sky-700 dark:bg-sky-900/30 dark:text-sky-300">
                {{ entry.provider }}
              </span>
              <span
                v-if="entry.cli"
                class="rounded-full bg-violet-100 px-2.5 py-0.5 text-[11px] font-medium uppercase tracking-wide text-violet-700 dark:bg-violet-900/30 dark:text-violet-300"
              >
                via CLI
              </span>
              <span
                v-else-if="!entry.supportsImageInput"
                class="rounded-full bg-gray-100 px-2.5 py-0.5 text-[11px] font-medium uppercase tracking-wide text-gray-600 dark:bg-gray-800 dark:text-gray-300"
              >
                text only
              </span>
            </div>
            <h3 class="mt-3 break-all font-mono text-sm font-semibold text-gray-900 dark:text-white">
              {{ entry.id }}
            </h3>
            <p class="mt-2 text-sm text-gray-600 dark:text-gray-300">
              Model name: <span class="font-mono">{{ entry.model }}</span>
            </p>
            <p v-if="entry.cli" class="mt-4 text-sm text-gray-500 dark:text-gray-400">
              Reached through the installed <span class="font-mono">{{ entry.provider }}</span> CLI{{ entry.note ? ` (${entry.note})` : "" }}.
            </p>
            <dl v-else class="mt-4 grid grid-cols-2 gap-3 text-sm">
              <div class="rounded-2xl bg-gray-50 px-3 py-2 dark:bg-gray-950/70">
                <dt class="text-[11px] font-semibold uppercase tracking-[0.18em] text-gray-400 dark:text-gray-500">Input</dt>
                <dd class="mt-1 font-semibold text-gray-900 dark:text-white">{{ entry.inputTokensLabel }}</dd>
              </div>
              <div class="rounded-2xl bg-gray-50 px-3 py-2 dark:bg-gray-950/70">
                <dt class="text-[11px] font-semibold uppercase tracking-[0.18em] text-gray-400 dark:text-gray-500">Output</dt>
                <dd class="mt-1 font-semibold text-gray-900 dark:text-white">{{ entry.outputTokensLabel }}</dd>
              </div>
            </dl>
          </article>
        </section>
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, ref } from "vue";
import AppLayout from "../components/AppLayout.vue";
import SystemSettingsLink from "../components/SystemSettingsLink.vue";
import { useProviderStatus } from "../composables/useProviderAuth";
import {
	formatTokenCount,
	MODEL_CATALOG,
	modelNameOf,
	providerOf,
} from "../constants/models";

type ModelEntry = {
	id: string;
	provider: string;
	model: string;
	/** Reached through a CLI provider, which reports no catalog data. */
	cli: boolean;
	note: string;
	inputTokensLabel: string;
	outputTokensLabel: string;
	supportsImageInput: boolean;
};

const activeFilterClass =
	"whitespace-nowrap rounded-full bg-sky-600 px-3 py-1.5 text-xs font-semibold text-white";
const inactiveFilterClass =
	"whitespace-nowrap rounded-full px-3 py-1.5 text-xs font-medium text-gray-600 hover:bg-white dark:text-gray-300 dark:hover:bg-gray-900";

const search = ref("");
const providerFilter = ref("all");

// Installed CLI providers' models are the CLI pairs the model pickers offer
// (useAvailableModels), so the page lists exactly what can be picked.
const cliStatus = useProviderStatus();

const catalogModels: ModelEntry[] = MODEL_CATALOG.map((entry) => ({
	id: entry.id,
	provider: providerOf(entry.id),
	model: modelNameOf(entry.id),
	cli: false,
	note: "",
	inputTokensLabel: formatTokenCount(entry.input_tokens),
	outputTokensLabel: formatTokenCount(entry.output_tokens),
	supportsImageInput: entry.supports_image_input,
}));

const cliModels = computed<ModelEntry[]>(() =>
	(cliStatus.clis.value ?? [])
		.filter((cli) => cli.installed)
		.flatMap((cli) =>
			cli.models.map((id) => ({
				id,
				provider: providerOf(id) || cli.name,
				model: modelNameOf(id),
				cli: true,
				note: cli.modelNote,
				inputTokensLabel: "",
				outputTokensLabel: "",
				supportsImageInput: false,
			})),
		),
);

const allModels = computed(() => {
	const seen = new Set<string>();
	return [...catalogModels, ...cliModels.value].filter((entry) => {
		if (seen.has(entry.id)) return false;
		seen.add(entry.id);
		return true;
	});
});

const providers = computed(() =>
	Array.from(new Set(allModels.value.map((entry) => entry.provider))).filter(
		Boolean,
	),
);

const providerFilters = computed(() => [
	{ label: "All", value: "all" },
	...providers.value.map((provider) => ({ label: provider, value: provider })),
]);

const multimodalCount = computed(
	() => MODEL_CATALOG.filter((entry) => entry.supports_image_input).length,
);

const filteredModels = computed(() => {
	const term = search.value.trim().toLowerCase();
	return allModels.value.filter((entry) => {
		if (
			providerFilter.value !== "all" &&
			entry.provider !== providerFilter.value
		) {
			return false;
		}
		if (!term) return true;
		return (
			entry.id.toLowerCase().includes(term) ||
			entry.provider.toLowerCase().includes(term) ||
			entry.model.toLowerCase().includes(term) ||
			entry.inputTokensLabel.toLowerCase().includes(term) ||
			entry.outputTokensLabel.toLowerCase().includes(term) ||
			(entry.cli && "via cli".includes(term)) ||
			(!entry.cli && !entry.supportsImageInput && "text only".includes(term))
		);
	});
});
</script>
