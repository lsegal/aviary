<template>
  <AppLayout>
    <div class="px-6 py-6">
      <!-- Header -->
      <div class="mb-6 flex items-center justify-between">
        <h2 class="text-xl font-bold text-gray-900 dark:text-white">Agents</h2>
        <div class="flex gap-2">
          <button
            class="rounded-lg bg-gray-100 px-4 py-2 text-sm text-gray-700 hover:bg-gray-200 dark:bg-gray-800 dark:text-gray-300 dark:hover:bg-gray-700"
            @click="store.fetchAgents()">Refresh</button>
          <button class="rounded-lg bg-blue-600 px-4 py-2 text-sm font-semibold text-white hover:bg-blue-500"
            @click="openAdd()">+ Add Agent</button>
        </div>
      </div>

      <!-- States -->
      <div v-if="store.loading" class="text-sm text-gray-500 dark:text-gray-400">Loading…</div>
      <div v-else-if="store.error" class="text-sm text-red-500 dark:text-red-400">Error: {{ store.error }}</div>

      <!-- Empty -->
      <div v-else-if="!store.agents.length" class="flex flex-col items-center gap-4 py-16 text-center">
        <p class="text-gray-500 dark:text-gray-400">No agents configured.</p>
        <button class="rounded-lg bg-blue-600 px-4 py-2 text-sm font-semibold text-white hover:bg-blue-500"
          @click="openAdd()">Add your first agent</button>
      </div>

      <!-- Agent cards -->
      <div v-else class="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        <div v-for="agent in store.agents" :key="agent.id"
          class="flex flex-col rounded-xl border border-gray-200 bg-white p-5 dark:border-gray-800 dark:bg-gray-900">
          <!-- Name + state -->
          <div class="mb-2 flex items-center gap-2">
            <span class="font-semibold text-gray-900 dark:text-white">{{ agent.name }}</span>
            <span :class="stateBadge(agent.state)"
              class="rounded-full px-2 py-0.5 text-xs font-medium">{{ agent.state }}</span>
          </div>
          <!-- Fields -->
          <dl class="mb-4 grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-xs">
            <dt class="font-medium text-gray-500 dark:text-gray-400">Model</dt>
            <dd class="truncate text-gray-800 dark:text-gray-200">{{ agent.model || '—' }}</dd>
            <template v-if="agent.fallbacks?.length">
              <dt class="font-medium text-gray-500 dark:text-gray-400">Fallbacks</dt>
              <dd class="truncate text-gray-800 dark:text-gray-200">{{ agent.fallbacks.join(', ') }}</dd>
            </template>
            <dt class="font-medium text-gray-500 dark:text-gray-400">Tasks</dt>
            <dd class="text-gray-800 dark:text-gray-200">{{ taskSummary(agent.name) }}</dd>
          </dl>
          <ul v-if="tasksByAgent[agent.name]?.length" class="mb-4 space-y-1 text-xs text-gray-500 dark:text-gray-400">
            <li v-for="task in tasksByAgent[agent.name]" :key="`${agent.name}:${task.name}`" class="truncate">
              {{ task.name }} ·
              {{ task.schedule ? `schedule: ${task.schedule}` : task.watch ? `watch: ${task.watch}` : 'trigger unset' }}
            </li>
          </ul>
          <!-- Actions -->
          <div class="mt-auto flex gap-2">
            <button
              class="flex-1 rounded-lg border border-gray-200 py-1.5 text-xs font-medium text-gray-700 hover:bg-gray-50 dark:border-gray-700 dark:text-gray-300 dark:hover:bg-gray-800"
              @click="openEdit(agent)">Edit</button>
            <button v-if="confirmDelete !== agent.name"
              class="flex-1 rounded-lg border border-red-200 py-1.5 text-xs font-medium text-red-600 hover:bg-red-50 dark:border-red-900 dark:text-red-400 dark:hover:bg-red-950"
              @click="confirmDelete = agent.name">Delete</button>
            <template v-else>
              <button
                class="flex-1 rounded-lg bg-red-600 py-1.5 text-xs font-medium text-white hover:bg-red-500 disabled:opacity-50"
                :disabled="saving" @click="doDelete(agent.name)">Confirm</button>
              <button
                class="flex-1 rounded-lg border border-gray-200 py-1.5 text-xs font-medium text-gray-500 hover:bg-gray-50 dark:border-gray-700 dark:hover:bg-gray-800"
                @click="confirmDelete = null">Cancel</button>
            </template>
          </div>
        </div>
      </div>
    </div>

    <!-- Add / Edit modal -->
    <Teleport to="body">
      <div v-if="modal" class="fixed inset-0 z-50 flex items-center justify-center bg-black/50 px-4"
        @click.self="closeModal()">
        <div class="w-full max-w-md rounded-xl bg-white p-6 shadow-2xl dark:bg-gray-900">
          <h3 class="mb-4 text-base font-bold text-gray-900 dark:text-white">
            {{ modal.mode === 'add' ? 'Add Agent' : 'Edit Agent' }}
          </h3>
          <div class="space-y-4">
            <div>
              <label class="mb-1 block text-xs font-medium text-gray-600 dark:text-gray-400">Name</label>
              <input v-model="modal.name" type="text" :disabled="modal.mode === 'edit'" placeholder="assistant"
                class="w-full rounded-lg border border-gray-300 bg-white px-3 py-2 text-sm text-gray-900 placeholder-gray-400 focus:border-blue-500 focus:outline-none disabled:opacity-60 dark:border-gray-700 dark:bg-gray-800 dark:text-white" />
            </div>
            <div>
              <label class="mb-1 block text-xs font-medium text-gray-600 dark:text-gray-400">Model</label>
              <ModelSelector v-model="modal.model" placeholder="Select a model…" />
            </div>
            <div>
              <label class="mb-1 block text-xs font-medium text-gray-600 dark:text-gray-400">Fallbacks</label>
              <ModelSelector v-model="modal.fallbacks" multiple placeholder="Add fallbacks…" />
            </div>
          </div>
          <p v-if="modalError" class="mt-3 text-xs text-red-500 dark:text-red-400">{{ modalError }}</p>
          <div class="mt-6 flex justify-end gap-3">
            <button
              class="rounded-lg border border-gray-200 px-4 py-2 text-sm text-gray-600 hover:bg-gray-50 dark:border-gray-700 dark:text-gray-400 dark:hover:bg-gray-800"
              @click="closeModal()">Cancel</button>
            <button
              class="rounded-lg bg-blue-600 px-4 py-2 text-sm font-semibold text-white hover:bg-blue-500 disabled:opacity-50"
              :disabled="saving || !modal.name" @click="saveModal()">{{ saving ? 'Saving…' : 'Save' }}</button>
          </div>
        </div>
      </div>
    </Teleport>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import AppLayout from "../components/AppLayout.vue";
import ModelSelector from "../components/ModelSelector.vue";
import { type Agent, useAgentsStore } from "../stores/agents";
import { useSettingsStore } from "../stores/settings";

const store = useAgentsStore();
const settingsStore = useSettingsStore();
const confirmDelete = ref<string | null>(null);
const saving = ref(false);
const modalError = ref("");

interface ModalState {
	mode: "add" | "edit";
	name: string;
	model: string;
	fallbacks: string[];
}
const modal = ref<ModalState | null>(null);

const tasksByAgent = computed(() =>
	Object.fromEntries(
		(settingsStore.config?.agents ?? []).map((agent) => [
			agent.name,
			agent.tasks ?? [],
		]),
	),
);

onMounted(() => {
	store.fetchAgents();
	settingsStore.fetchConfig();
});

function taskSummary(agentName: string): string {
	const count = tasksByAgent.value[agentName]?.length ?? 0;
	return count === 0 ? "none" : `${count} configured`;
}

function stateBadge(state: string) {
	if (state === "idle")
		return "bg-green-100 text-green-700 dark:bg-green-900 dark:text-green-300";
	if (state === "running")
		return "bg-yellow-100 text-yellow-700 dark:bg-yellow-900 dark:text-yellow-300";
	return "bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-400";
}

function openAdd() {
	modal.value = { mode: "add", name: "", model: "", fallbacks: [] };
	modalError.value = "";
}

function openEdit(agent: Agent) {
	modal.value = {
		mode: "edit",
		name: agent.name,
		model: agent.model ?? "",
		fallbacks: [...(agent.fallbacks ?? [])],
	};
	modalError.value = "";
}

function closeModal() {
	modal.value = null;
	modalError.value = "";
}

async function saveModal() {
	if (!modal.value) return;
	saving.value = true;
	modalError.value = "";
	try {
		const { mode, name, model, fallbacks: rawFallbacks } = modal.value;
		const fallbacks = rawFallbacks.map((s) => s.trim()).filter(Boolean);
		if (mode === "add") {
			await store.addAgent({ name, model, fallbacks });
		} else {
			await store.updateAgent({ name, model, fallbacks });
		}
		closeModal();
	} catch (e) {
		modalError.value = e instanceof Error ? e.message : String(e);
	} finally {
		saving.value = false;
	}
}

async function doDelete(name: string) {
	saving.value = true;
	try {
		await store.deleteAgent(name);
		confirmDelete.value = null;
	} catch (e) {
		console.error(e);
	} finally {
		saving.value = false;
	}
}
</script>
