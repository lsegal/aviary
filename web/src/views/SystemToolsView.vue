<template>
  <AppLayout>
    <div class="h-full overflow-y-auto bg-gray-50/60 dark:bg-gray-950">
      <div class="mx-auto max-w-7xl px-4 py-6 sm:px-6">
        <div class="mb-6 flex flex-wrap items-start justify-between gap-4">
          <div>
            <h2 class="text-xl font-bold text-gray-900 dark:text-white">System Tools</h2>
            <p class="mt-1 max-w-3xl text-sm text-gray-500 dark:text-gray-400">
              Read-only catalog of MCP tool groups, individual tools, and currently activated skills.
            </p>
          </div>
          <button
            type="button"
            class="rounded-lg border border-gray-200 bg-white px-4 py-2 text-sm text-gray-700 hover:bg-gray-100 disabled:opacity-50 dark:border-gray-700 dark:bg-gray-900 dark:text-gray-300 dark:hover:bg-gray-800"
            :disabled="loading"
            @click="loadCatalog"
          >
            {{ loading ? "Refreshing…" : "Refresh" }}
          </button>
        </div>

        <div v-if="errorMessage" class="mb-4 rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700 dark:border-red-900 dark:bg-red-950 dark:text-red-300">
          {{ errorMessage }}
        </div>

        <div class="mb-6 grid gap-3 sm:grid-cols-3">
          <div class="rounded-2xl border border-gray-200 bg-white p-4 dark:border-gray-800 dark:bg-gray-900">
            <p class="text-[11px] font-semibold uppercase tracking-[0.18em] text-gray-400 dark:text-gray-500">Tool Groups</p>
            <p class="mt-2 text-3xl font-semibold text-gray-900 dark:text-white">{{ groupedTools.length }}</p>
          </div>
          <div class="rounded-2xl border border-gray-200 bg-white p-4 dark:border-gray-800 dark:bg-gray-900">
            <p class="text-[11px] font-semibold uppercase tracking-[0.18em] text-gray-400 dark:text-gray-500">MCP Tools</p>
            <p class="mt-2 text-3xl font-semibold text-gray-900 dark:text-white">{{ availableTools.length }}</p>
          </div>
          <div class="rounded-2xl border border-emerald-200 bg-emerald-50 p-4 dark:border-emerald-900/50 dark:bg-emerald-950/20">
            <p class="text-[11px] font-semibold uppercase tracking-[0.18em] text-emerald-600 dark:text-emerald-400">Activated Skills</p>
            <p class="mt-2 text-3xl font-semibold text-emerald-700 dark:text-emerald-300">{{ enabledSkills.length }}</p>
          </div>
        </div>

        <section class="space-y-5 pb-8">
          <div
            v-for="group in groupedTools"
            :key="group.key"
            class="overflow-hidden rounded-2xl border border-gray-200 bg-white dark:border-gray-800 dark:bg-gray-900"
          >
            <div class="border-b border-gray-100 px-5 py-4 dark:border-gray-800">
              <div class="flex flex-wrap items-center gap-3">
                <h3 class="text-sm font-semibold uppercase tracking-[0.18em] text-gray-500 dark:text-gray-400">{{ group.label }}</h3>
                <span class="rounded-full bg-gray-100 px-2.5 py-1 text-[11px] font-medium text-gray-600 dark:bg-gray-800 dark:text-gray-300">
                  {{ group.tools.length }} {{ group.tools.length === 1 ? "tool" : "tools" }}
                </span>
              </div>
            </div>

            <div class="divide-y divide-gray-100 dark:divide-gray-800">
              <div v-for="tool in group.tools" :key="tool.name" class="grid gap-3 px-5 py-4 md:grid-cols-[220px_minmax(0,1fr)]">
                <div class="space-y-1">
                  <code class="text-sm font-semibold text-gray-900 dark:text-white">{{ tool.name }}</code>
                  <p class="text-xs text-gray-400 dark:text-gray-500">{{ group.label }}</p>
                </div>
                <p class="text-sm leading-6 text-gray-600 dark:text-gray-300">
                  {{ tool.description?.trim() || "No description available from the MCP server." }}
                </p>
              </div>
            </div>
          </div>

          <div class="overflow-hidden rounded-2xl border border-emerald-200 bg-white dark:border-emerald-900/50 dark:bg-gray-900">
            <div class="border-b border-emerald-100 px-5 py-4 dark:border-emerald-900/40">
              <div class="flex flex-wrap items-center gap-3">
                <h3 class="text-sm font-semibold uppercase tracking-[0.18em] text-emerald-700 dark:text-emerald-300">Activated Skills</h3>
                <span class="rounded-full bg-emerald-100 px-2.5 py-1 text-[11px] font-medium text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300">
                  {{ enabledSkills.length }} active
                </span>
              </div>
            </div>

            <div v-if="enabledSkills.length" class="divide-y divide-gray-100 dark:divide-gray-800">
              <div v-for="skill in enabledSkills" :key="skill.name" class="grid gap-3 px-5 py-4 md:grid-cols-[220px_minmax(0,1fr)]">
                <div class="space-y-1">
                  <div class="flex flex-wrap items-center gap-2">
                    <span class="text-sm font-semibold text-gray-900 dark:text-white">{{ skill.name }}</span>
                    <span class="rounded-full bg-gray-100 px-2 py-0.5 text-[11px] uppercase tracking-wide text-gray-600 dark:bg-gray-800 dark:text-gray-300">
                      {{ skill.source }}
                    </span>
                  </div>
                  <p class="text-xs text-gray-400 dark:text-gray-500">{{ skill.path }}</p>
                </div>
                <p class="text-sm leading-6 text-gray-600 dark:text-gray-300">
                  {{ skill.description?.trim() || "No description available for this skill." }}
                </p>
              </div>
            </div>
            <div v-else class="px-5 py-6 text-sm text-gray-500 dark:text-gray-400">
              No skills are enabled in the current configuration.
            </div>
          </div>
        </section>
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import AppLayout from "../components/AppLayout.vue";
import { type MCPToolInfo, useMCP } from "../composables/useMCP";

interface InstalledSkill {
	name: string;
	description: string;
	path: string;
	enabled: boolean;
	source: string;
}

interface ToolGroup {
	key: string;
	label: string;
	tools: MCPToolInfo[];
}

const { callTool, listTools } = useMCP();

const loading = ref(false);
const errorMessage = ref("");
const availableTools = ref<MCPToolInfo[]>([]);
const installedSkills = ref<InstalledSkill[]>([]);

const CATEGORY_LABELS: Record<string, string> = {
	agent: "Agent",
	auth: "Auth",
	browser: "Browser",
	job: "Jobs",
	memory: "Memory",
	search: "Search",
	server: "Server",
	session: "Sessions",
	task: "Tasks",
	usage: "Usage",
};

function toolCategory(name: string): string {
	if (
		name === "ping" ||
		name.startsWith("server_") ||
		name.startsWith("config_")
	) {
		return "server";
	}
	if (name.startsWith("web_")) return "search";
	return name.split("_")[0] ?? name;
}

function toolCategoryLabel(category: string): string {
	return (
		CATEGORY_LABELS[category] ??
		category.charAt(0).toUpperCase() + category.slice(1)
	);
}

const groupedTools = computed<ToolGroup[]>(() => {
	const groups = new Map<string, MCPToolInfo[]>();
	for (const tool of availableTools.value) {
		const category = toolCategory(tool.name);
		const bucket = groups.get(category) ?? [];
		bucket.push(tool);
		groups.set(category, bucket);
	}
	return [...groups.entries()]
		.sort((a, b) =>
			toolCategoryLabel(a[0]).localeCompare(toolCategoryLabel(b[0])),
		)
		.map(([key, tools]) => ({
			key,
			label: toolCategoryLabel(key),
			tools: [...tools].sort((a, b) => a.name.localeCompare(b.name)),
		}));
});

const enabledSkills = computed(() =>
	installedSkills.value
		.filter((skill) => skill.enabled)
		.sort((a, b) => a.name.localeCompare(b.name)),
);

async function loadCatalog() {
	loading.value = true;
	errorMessage.value = "";
	try {
		const [tools, rawSkills] = await Promise.all([
			listTools(),
			callTool("skills_list"),
		]);
		availableTools.value = tools;
		installedSkills.value =
			(JSON.parse(rawSkills) as InstalledSkill[] | null)?.map((skill) => ({
				name: skill.name,
				description: skill.description ?? "",
				path: skill.path,
				enabled: Boolean(skill.enabled),
				source: skill.source,
			})) ?? [];
	} catch (error) {
		errorMessage.value = error instanceof Error ? error.message : String(error);
		availableTools.value = [];
		installedSkills.value = [];
	} finally {
		loading.value = false;
	}
}

onMounted(() => {
	void loadCatalog();
});
</script>
