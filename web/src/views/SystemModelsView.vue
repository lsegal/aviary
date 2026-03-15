<template>
  <AppLayout>
    <div class="h-full overflow-y-auto bg-[radial-gradient(circle_at_top_left,_rgba(14,165,233,0.08),_transparent_28%),linear-gradient(to_bottom,_rgba(255,255,255,0.96),_rgba(249,250,251,1))] dark:bg-[radial-gradient(circle_at_top_left,_rgba(14,165,233,0.15),_transparent_26%),linear-gradient(to_bottom,_rgba(3,7,18,0.96),_rgba(3,7,18,1))]">
      <div class="mx-auto max-w-7xl px-4 py-6 sm:px-6">
        <div class="mb-6 flex flex-wrap items-start justify-between gap-4">
          <div>
            <h2 class="text-xl font-bold text-gray-900 dark:text-white">Supported Models</h2>
            <p class="mt-1 max-w-3xl text-sm text-gray-500 dark:text-gray-400">
              Browse the built-in provider/model catalog Aviary accepts for agent and default model selection.
            </p>
          </div>
        </div>

        <div class="mb-6 grid gap-3 sm:grid-cols-3">
          <div class="rounded-2xl border border-gray-200 bg-white/90 p-4 backdrop-blur dark:border-gray-800 dark:bg-gray-900/90">
            <p class="text-[11px] font-semibold uppercase tracking-[0.18em] text-gray-400 dark:text-gray-500">Models</p>
            <p class="mt-2 text-3xl font-semibold text-gray-900 dark:text-white">{{ MODEL_CATALOG.length }}</p>
          </div>
          <div class="rounded-2xl border border-sky-200 bg-sky-50/80 p-4 backdrop-blur dark:border-sky-900/50 dark:bg-sky-950/20">
            <p class="text-[11px] font-semibold uppercase tracking-[0.18em] text-sky-600 dark:text-sky-400">Providers</p>
            <p class="mt-2 text-3xl font-semibold text-sky-700 dark:text-sky-300">{{ SUPPORTED_PROVIDERS.length }}</p>
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
            class="rounded-3xl border border-gray-200 bg-white/95 p-5 shadow-sm transition-colors dark:border-gray-800 dark:bg-gray-900/95"
          >
            <div class="flex flex-wrap items-center gap-2">
              <span class="rounded-full bg-sky-100 px-2.5 py-0.5 text-[11px] font-medium uppercase tracking-wide text-sky-700 dark:bg-sky-900/30 dark:text-sky-300">
                {{ entry.provider }}
              </span>
              <span
                v-if="!entry.supportsImageInput"
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
            <dl class="mt-4 grid grid-cols-2 gap-3 text-sm">
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
import {
	formatTokenCount,
	MODEL_CATALOG,
	modelNameOf,
	providerOf,
	SUPPORTED_PROVIDERS,
} from "../constants/models";

type ProviderFilter = "all" | (typeof SUPPORTED_PROVIDERS)[number];

const activeFilterClass =
	"whitespace-nowrap rounded-full bg-sky-600 px-3 py-1.5 text-xs font-semibold text-white";
const inactiveFilterClass =
	"whitespace-nowrap rounded-full px-3 py-1.5 text-xs font-medium text-gray-600 hover:bg-white dark:text-gray-300 dark:hover:bg-gray-900";

const search = ref("");
const providerFilter = ref<ProviderFilter>("all");

const providerFilters = [
	{ label: "All", value: "all" as const },
	...SUPPORTED_PROVIDERS.map((provider) => ({
		label: provider,
		value: provider,
	})),
];

const multimodalCount = computed(
	() => MODEL_CATALOG.filter((entry) => entry.supports_image_input).length,
);

const filteredModels = computed(() => {
	const term = search.value.trim().toLowerCase();
	return MODEL_CATALOG.map((entry) => {
		const provider = providerOf(entry.id);
		return {
			id: entry.id,
			provider,
			model: modelNameOf(entry.id),
			inputTokensLabel: formatTokenCount(entry.input_tokens),
			outputTokensLabel: formatTokenCount(entry.output_tokens),
			supportsImageInput: entry.supports_image_input,
		};
	}).filter((entry) => {
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
			(!entry.supportsImageInput && "text only".includes(term))
		);
	});
});
</script>
