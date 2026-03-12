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
              <div v-for="tool in group.tools" :key="tool.name" class="grid gap-3 px-5 py-4 md:grid-cols-[220px_minmax(0,1fr)_auto]">
                <div class="space-y-1">
                  <code class="text-sm font-semibold text-gray-900 dark:text-white">{{ tool.name }}</code>
                  <p class="text-xs text-gray-400 dark:text-gray-500">{{ group.label }}</p>
                </div>
                <p class="text-sm leading-6 text-gray-600 dark:text-gray-300">
                  {{ tool.description?.trim() || "No description available from the MCP server." }}
                </p>
                <div class="flex items-start justify-end">
                  <button
                    type="button"
                    :data-testid="`run-tool-${tool.name}`"
                    class="rounded-lg border border-blue-200 bg-blue-50 px-3 py-1.5 text-xs font-semibold text-blue-700 hover:bg-blue-100 dark:border-blue-900/60 dark:bg-blue-950/30 dark:text-blue-300 dark:hover:bg-blue-950/50"
                    @click="openRunModal(tool)"
                  >
                    Run
                  </button>
                </div>
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

    <Teleport to="body">
      <div
        v-if="runModal"
        class="fixed inset-0 z-50 flex items-center justify-center bg-black/50 px-4 py-6"
        @click.self="closeRunModal"
      >
        <div class="flex max-h-[88vh] w-full max-w-3xl flex-col overflow-hidden rounded-xl border border-gray-200 bg-white shadow-2xl dark:border-gray-800 dark:bg-gray-900">
          <div class="flex items-start justify-between gap-4 border-b border-gray-200 px-5 py-4 dark:border-gray-800">
            <div>
              <h3 class="text-sm font-semibold text-gray-900 dark:text-white">Run Tool</h3>
              <p class="mt-1 font-mono text-xs text-gray-500 dark:text-gray-400">{{ runModal.tool.name }}</p>
            </div>
            <button
              type="button"
              class="rounded-lg border border-gray-200 px-3 py-1.5 text-xs text-gray-700 hover:bg-gray-100 dark:border-gray-700 dark:text-gray-300 dark:hover:bg-gray-800"
              @click="closeRunModal"
            >Close</button>
          </div>

          <div class="space-y-5 overflow-y-auto p-5">
            <p class="text-sm leading-6 text-gray-600 dark:text-gray-300">
              {{ runModal.tool.description?.trim() || "No description available from the MCP server." }}
            </p>

            <div class="space-y-4">
              <div v-if="runModal.fields.length" class="grid gap-4 md:grid-cols-2">
                <div
                  v-for="field in runModal.fields"
                  :key="field.name"
                  class="space-y-2"
                >
                  <div class="flex items-center gap-2">
                    <label class="text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">
                      {{ field.name }}
                    </label>
                    <span
                      v-if="field.required"
                      class="rounded-full bg-amber-100 px-2 py-0.5 text-[10px] font-medium uppercase tracking-wide text-amber-700 dark:bg-amber-950/60 dark:text-amber-300"
                    >
                      Required
                    </span>
                  </div>

                  <select
                    v-if="field.kind === 'boolean'"
                    v-model="runModal.values[field.name]"
                    class="w-full rounded-lg border border-gray-300 bg-white px-3 py-2 text-sm text-gray-900 focus:border-blue-500 focus:outline-none dark:border-gray-700 dark:bg-gray-800 dark:text-white"
                  >
                    <option value="">{{ field.placeholder }}</option>
                    <option value="true">true</option>
                    <option value="false">false</option>
                  </select>

                  <select
                    v-else-if="field.kind === 'enum'"
                    v-model="runModal.values[field.name]"
                    class="w-full rounded-lg border border-gray-300 bg-white px-3 py-2 text-sm text-gray-900 focus:border-blue-500 focus:outline-none dark:border-gray-700 dark:bg-gray-800 dark:text-white"
                  >
                    <option value="">{{ field.placeholder }}</option>
                    <option
                      v-for="choice in field.enumOptions"
                      :key="choice.value"
                      :value="choice.value"
                    >
                      {{ choice.label }}
                    </option>
                  </select>

                  <textarea
                    v-else-if="field.kind === 'json'"
                    v-model="runModal.values[field.name]"
                    :placeholder="field.placeholder"
                    rows="5"
                    class="w-full rounded-lg border border-gray-300 bg-white px-3 py-2 font-mono text-xs text-gray-900 placeholder-gray-400 focus:border-blue-500 focus:outline-none dark:border-gray-700 dark:bg-gray-800 dark:text-white"
                  />

                  <input
                    v-else
                    v-model="runModal.values[field.name]"
                    type="text"
                    :placeholder="field.placeholder"
                    class="w-full rounded-lg border border-gray-300 bg-white px-3 py-2 text-sm text-gray-900 placeholder-gray-400 focus:border-blue-500 focus:outline-none dark:border-gray-700 dark:bg-gray-800 dark:text-white"
                  />

                  <p v-if="field.helpText" class="text-xs leading-5 text-gray-500 dark:text-gray-400">
                    {{ field.helpText }}
                  </p>
                </div>
              </div>

              <div v-else class="rounded-xl border border-dashed border-gray-300 bg-gray-50 px-4 py-5 text-sm text-gray-500 dark:border-gray-700 dark:bg-gray-950/50 dark:text-gray-400">
                This tool does not declare any input arguments.
              </div>
            </div>

            <div v-if="runModal.errorMessage" class="rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700 dark:border-red-900 dark:bg-red-950 dark:text-red-300">
              {{ runModal.errorMessage }}
            </div>

            <div class="space-y-2">
              <div class="flex items-center justify-between gap-3">
                <h4 class="text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">Output</h4>
                <button
                  type="button"
                  class="rounded-lg bg-blue-600 px-4 py-2 text-sm font-semibold text-white hover:bg-blue-500 disabled:opacity-50"
                  :disabled="runModal.running"
                  @click="submitToolRun"
                >
                  {{ runModal.running ? "Running…" : "Run Tool" }}
                </button>
              </div>
              <pre data-testid="tool-run-output" class="min-h-[220px] overflow-auto rounded-xl bg-gray-950 px-4 py-3 text-xs leading-5 text-gray-100">{{ runModal.output || "Run the tool to see its output." }}</pre>
            </div>
          </div>
        </div>
      </div>
    </Teleport>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import AppLayout from "../components/AppLayout.vue";
import { type MCPToolInfo, useMCP } from "../composables/useMCP";
import { groupTools, toolCategoryLabel } from "../lib/toolPermissions";

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

interface ToolFieldOption {
	label: string;
	value: string;
}

interface ToolField {
	name: string;
	kind: "text" | "boolean" | "enum" | "json";
	placeholder: string;
	helpText: string;
	required: boolean;
	enumOptions: ToolFieldOption[];
	schema: Record<string, unknown>;
}

interface RunModalState {
	tool: MCPToolInfo;
	fields: ToolField[];
	values: Record<string, string>;
	output: string;
	errorMessage: string;
	running: boolean;
}

const { callTool, listTools } = useMCP();

const loading = ref(false);
const errorMessage = ref("");
const availableTools = ref<MCPToolInfo[]>([]);
const installedSkills = ref<InstalledSkill[]>([]);
const runModal = ref<RunModalState | null>(null);

const groupedTools = computed<ToolGroup[]>(() => {
	return groupTools(availableTools.value)
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

function toolSchemaProperties(
	tool: MCPToolInfo,
): Record<string, Record<string, unknown>> {
	const properties = tool.inputSchema?.properties;
	return properties && typeof properties === "object" ? properties : {};
}

function toolFieldKind(schema: Record<string, unknown>): ToolField["kind"] {
	if (Array.isArray(schema.enum) && schema.enum.length > 0) return "enum";
	const rawType = schema.type;
	const types = Array.isArray(rawType) ? rawType : rawType ? [rawType] : [];
	const normalized = types.filter(
		(value): value is string => typeof value === "string" && value !== "null",
	);
	if (normalized.includes("boolean")) return "boolean";
	if (normalized.includes("object") || normalized.includes("array"))
		return "json";
	return "text";
}

function formatSchemaValue(value: unknown): string {
	if (typeof value === "string") return value;
	try {
		return JSON.stringify(value);
	} catch {
		return String(value);
	}
}

function toolFieldPlaceholder(
	name: string,
	schema: Record<string, unknown>,
): string {
	if (schema.default !== undefined) {
		return `Default: ${formatSchemaValue(schema.default)}`;
	}
	if (Array.isArray(schema.enum) && schema.enum.length > 0) {
		return `Choose ${name}`;
	}
	const rawType = schema.type;
	const types = Array.isArray(rawType) ? rawType : rawType ? [rawType] : [];
	const normalized = types.filter(
		(value): value is string => typeof value === "string" && value !== "null",
	);
	if (normalized.includes("boolean")) return "Select true or false";
	if (normalized.includes("object") || normalized.includes("array")) {
		return `Enter ${normalized.includes("array") ? "a JSON array" : "a JSON object"}`;
	}
	if (
		typeof schema.description === "string" &&
		schema.description.trim().length > 0
	) {
		return schema.description.trim();
	}
	if (normalized.includes("integer")) return "Enter an integer";
	if (normalized.includes("number")) return "Enter a number";
	return `Enter ${name}`;
}

function toolFieldHelpText(schema: Record<string, unknown>): string {
	const parts: string[] = [];
	if (
		typeof schema.description === "string" &&
		schema.description.trim().length > 0
	) {
		parts.push(schema.description.trim());
	}
	const rawType = schema.type;
	const types = Array.isArray(rawType) ? rawType : rawType ? [rawType] : [];
	const normalized = types.filter(
		(value): value is string => typeof value === "string" && value !== "null",
	);
	if (normalized.length > 0) {
		parts.push(`Type: ${normalized.join(" | ")}`);
	}
	return parts.join(" ");
}

function buildToolFields(tool: MCPToolInfo): ToolField[] {
	const required = new Set(tool.inputSchema?.required ?? []);
	return Object.entries(toolSchemaProperties(tool))
		.sort((a, b) => a[0].localeCompare(b[0]))
		.map(([name, schema]) => ({
			name,
			kind: toolFieldKind(schema),
			placeholder: toolFieldPlaceholder(name, schema),
			helpText: toolFieldHelpText(schema),
			required: required.has(name),
			enumOptions: Array.isArray(schema.enum)
				? schema.enum.map((choice) => ({
						label: formatSchemaValue(choice),
						value: formatSchemaValue(choice),
					}))
				: [],
			schema,
		}));
}

function initialFieldValue(field: ToolField): string {
	if (field.schema.default === undefined) return "";
	if (field.kind === "json") {
		try {
			return JSON.stringify(field.schema.default, null, 2);
		} catch {
			return "";
		}
	}
	return formatSchemaValue(field.schema.default);
}

function openRunModal(tool: MCPToolInfo) {
	const fields = buildToolFields(tool);
	runModal.value = {
		tool,
		fields,
		values: Object.fromEntries(
			fields.map((field) => [field.name, initialFieldValue(field)]),
		),
		output: "",
		errorMessage: "",
		running: false,
	};
}

function closeRunModal() {
	runModal.value = null;
}

function parseToolFieldValue(field: ToolField, value: string): unknown {
	switch (field.kind) {
		case "boolean":
			if (value === "true") return true;
			if (value === "false") return false;
			throw new Error(`${field.name} must be true or false.`);
		case "enum":
			return value;
		case "json":
			try {
				return JSON.parse(value);
			} catch (error) {
				throw new Error(
					`${field.name} must be valid JSON: ${error instanceof Error ? error.message : String(error)}`,
				);
			}
		case "text": {
			const rawType = field.schema.type;
			const types = Array.isArray(rawType) ? rawType : rawType ? [rawType] : [];
			const normalized = types.filter(
				(entry): entry is string =>
					typeof entry === "string" && entry !== "null",
			);
			if (normalized.includes("integer")) {
				const parsed = Number(value);
				if (!Number.isInteger(parsed)) {
					throw new Error(`${field.name} must be an integer.`);
				}
				return parsed;
			}
			if (normalized.includes("number")) {
				const parsed = Number(value);
				if (!Number.isFinite(parsed)) {
					throw new Error(`${field.name} must be a number.`);
				}
				return parsed;
			}
			return value;
		}
	}
}

async function submitToolRun() {
	if (!runModal.value) return;
	runModal.value.running = true;
	runModal.value.errorMessage = "";
	try {
		const args: Record<string, unknown> = {};
		for (const field of runModal.value.fields) {
			const rawValue = runModal.value.values[field.name]?.trim() ?? "";
			if (!rawValue) {
				if (field.required) {
					throw new Error(`${field.name} is required.`);
				}
				continue;
			}
			args[field.name] = parseToolFieldValue(field, rawValue);
		}
		runModal.value.output = await callTool(runModal.value.tool.name, args);
	} catch (error) {
		runModal.value.errorMessage =
			error instanceof Error ? error.message : String(error);
	} finally {
		runModal.value.running = false;
	}
}

async function loadCatalog() {
	loading.value = true;
	errorMessage.value = "";
	try {
		const tools = await listTools();
		const rawSkills = await callTool("skills_list");
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
