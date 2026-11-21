<template>
	<div class="rounded-xl border border-gray-200 bg-white p-5 dark:border-gray-800 dark:bg-gray-900" data-testid="settings-agents">
		<div class="mb-3 flex items-center justify-between gap-3">
			<h3 class="text-sm font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">Agents</h3>
			<router-link to="/agents?add=1" data-testid="settings-add-agent"
				class="rounded-lg px-2 py-1 text-xs font-medium text-blue-600 hover:bg-blue-50 dark:text-blue-400 dark:hover:bg-blue-950/40">
				+ Add agent
			</router-link>
		</div>
		<p v-if="!agents.length" class="text-sm text-gray-500 dark:text-gray-400">No agents configured.</p>
		<ul v-else class="divide-y divide-gray-100 dark:divide-gray-800">
			<li v-for="(agent, i) in agents" :key="`${i}:${agent.name}`" data-testid="settings-agent-row" :data-agent="agent.name"
				class="flex items-center gap-3 py-2">
				<div class="min-w-0 flex-1">
					<router-link v-if="agent.name" :to="`/agents/${encodeURIComponent(agent.name)}/settings`"
						data-testid="settings-agent-name"
						class="block truncate text-sm font-medium text-gray-900 hover:underline dark:text-white">{{ agent.name }}</router-link>
					<span v-else class="block truncate text-sm italic text-gray-500 dark:text-gray-400">New agent (unsaved)</span>
					<span v-if="agent.model" class="block truncate text-xs text-gray-500 dark:text-gray-400">{{ agent.model }}</span>
				</div>
				<DropdownMenuRoot>
					<DropdownMenuTrigger asChild>
						<button type="button" data-testid="settings-agent-menu" :aria-label="`More actions for ${agent.name || 'agent'}`"
							class="inline-flex h-8 w-8 items-center justify-center rounded-lg text-gray-500 hover:bg-gray-100 hover:text-gray-900 dark:text-gray-400 dark:hover:bg-gray-800 dark:hover:text-white">
							<EllipsisHorizontalIcon aria-hidden="true" class="h-4 w-4" />
						</button>
					</DropdownMenuTrigger>
					<DropdownMenuContent align="end" :side-offset="4"
						class="z-50 min-w-40 rounded-xl border border-gray-200 bg-white p-1 shadow-lg dark:border-gray-700 dark:bg-gray-900">
						<DropdownMenuItem data-testid="settings-agent-rename"
							class="flex cursor-pointer items-center gap-2 rounded-lg px-3 py-2 text-sm text-gray-700 outline-none data-[highlighted]:bg-gray-100 dark:text-gray-200 dark:data-[highlighted]:bg-gray-800"
							@select="openRename(i)">
							<PencilSquareIcon aria-hidden="true" class="h-4 w-4" />
							Rename
						</DropdownMenuItem>
						<!-- A disabled item gets no pointer events, so its reason
						     shows from the wrapper's tooltip. -->
						<div :title="deleteBlockedReason">
							<DropdownMenuItem data-testid="settings-agent-delete" :disabled="!!deleteBlockedReason"
								class="flex cursor-pointer items-center gap-2 rounded-lg px-3 py-2 text-sm text-red-600 outline-none data-[disabled]:cursor-default data-[disabled]:opacity-40 data-[highlighted]:bg-red-50 dark:text-red-400 dark:data-[highlighted]:bg-red-950/40"
								@select="openDelete(i)">
								<TrashIcon aria-hidden="true" class="h-4 w-4" />
								Delete
							</DropdownMenuItem>
						</div>
					</DropdownMenuContent>
				</DropdownMenuRoot>
			</li>
		</ul>
	</div>

	<DialogRoot :open="renameIdx !== null" @update:open="(v) => { if (!v && !busy) renameIdx = null }">
		<DialogPortal>
			<DialogOverlay class="fixed inset-0 z-50 bg-black/50" />
			<DialogContent data-testid="rename-agent-dialog"
				class="fixed left-1/2 top-1/2 z-50 w-full max-w-md -translate-x-1/2 -translate-y-1/2 rounded-xl bg-white p-6 shadow-2xl dark:bg-gray-900">
				<form @submit.prevent="confirmRename">
					<DialogTitle class="text-base font-bold text-gray-900 dark:text-white">Rename agent</DialogTitle>
					<DialogDescription class="mt-2 text-sm text-gray-600 dark:text-gray-400">
						Its projects, scheduled tasks and sessions move with it.
					</DialogDescription>
					<label class="field-label mt-4" for="rename-agent-name">Name</label>
					<input id="rename-agent-name" v-model="renameValue" type="text" class="field-input"
						data-testid="rename-agent-input" autocomplete="off" />
					<p v-if="renameError" role="alert" data-testid="rename-agent-error" class="mt-2 text-sm text-red-600 dark:text-red-400">
						{{ renameError }}
					</p>
					<div class="mt-6 flex justify-end gap-3">
						<DialogClose type="button" :disabled="busy"
							class="rounded-lg border border-gray-200 px-4 py-2 text-sm text-gray-600 hover:bg-gray-50 disabled:opacity-50 dark:border-gray-700 dark:text-gray-400 dark:hover:bg-gray-800">
							Cancel
						</DialogClose>
						<button type="submit" :disabled="busy" data-testid="rename-agent-confirm"
							class="rounded-lg bg-blue-600 px-4 py-2 text-sm font-semibold text-white hover:bg-blue-500 disabled:opacity-50">
							{{ busy ? "Renaming…" : "Rename" }}
						</button>
					</div>
				</form>
			</DialogContent>
		</DialogPortal>
	</DialogRoot>

	<AlertDialogRoot :open="deleteIdx !== null" @update:open="(v) => { if (!v && !busy) deleteIdx = null }">
		<AlertDialogPortal>
			<AlertDialogOverlay class="fixed inset-0 z-50 bg-black/50" />
			<AlertDialogContent data-testid="delete-agent-dialog"
				class="fixed left-1/2 top-1/2 z-50 w-full max-w-lg -translate-x-1/2 -translate-y-1/2 rounded-xl bg-white p-6 shadow-2xl dark:bg-gray-900">
				<AlertDialogTitle class="text-base font-bold text-gray-900 dark:text-white">
					Delete {{ deleteTarget?.name || "this agent" }}?
				</AlertDialogTitle>
				<AlertDialogDescription class="mt-2 space-y-2 text-sm text-gray-600 dark:text-gray-400">
					<span class="block">
						Aviary removes <b class="font-medium text-gray-900 dark:text-white">{{ deleteTarget?.name || "this agent" }}</b>
						and all of its settings from aviary.yaml. This cannot be undone.
					</span>
					<span v-if="dependents.length" data-testid="delete-agent-dependents" class="block text-amber-700 dark:text-amber-400">
						It still has {{ dependents.join(", ") }}, which stop working with it.
					</span>
				</AlertDialogDescription>
				<p v-if="deleteError" role="alert" class="mt-3 text-sm text-red-600 dark:text-red-400">{{ deleteError }}</p>
				<div class="mt-6 flex justify-end gap-3">
					<AlertDialogCancel :disabled="busy"
						class="rounded-lg border border-gray-200 px-4 py-2 text-sm text-gray-600 hover:bg-gray-50 disabled:opacity-50 dark:border-gray-700 dark:text-gray-400 dark:hover:bg-gray-800">
						Cancel
					</AlertDialogCancel>
					<button type="button" :disabled="busy" data-testid="delete-agent-confirm"
						class="rounded-lg bg-red-600 px-4 py-2 text-sm font-semibold text-white hover:bg-red-500 disabled:opacity-50"
						@click="confirmDelete">{{ busy ? "Deleting…" : "Delete agent" }}</button>
				</div>
			</AlertDialogContent>
		</AlertDialogPortal>
	</AlertDialogRoot>
</template>

<script setup lang="ts">
import EllipsisHorizontalIcon from "@heroicons/vue/24/outline/EllipsisHorizontalIcon";
import PencilSquareIcon from "@heroicons/vue/24/outline/PencilSquareIcon";
import TrashIcon from "@heroicons/vue/24/outline/TrashIcon";
import {
	AlertDialogCancel,
	AlertDialogContent,
	AlertDialogDescription,
	AlertDialogOverlay,
	AlertDialogPortal,
	AlertDialogRoot,
	AlertDialogTitle,
	DialogClose,
	DialogContent,
	DialogDescription,
	DialogOverlay,
	DialogPortal,
	DialogRoot,
	DialogTitle,
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuRoot,
	DropdownMenuTrigger,
} from "radix-vue";
import { computed, inject, ref } from "vue";
import { useMCP } from "../../composables/useMCP";
import type { AgentEntry } from "../../stores/settings";
import { settingsViewContextKey } from "./context";

const settings = inject(settingsViewContextKey);
if (!settings) throw new Error("Settings view context is not available.");
const { callTool } = useMCP();

const agents = computed<AgentEntry[]>(() => settings.draft.agents);
const busy = ref(false);

// Aviary needs an agent to run anything, so the last one stays.
const deleteBlockedReason = computed(() =>
	agents.value.length <= 1
		? "The last agent can't be deleted. Add another agent first."
		: "",
);

const renameIdx = ref<number | null>(null);
const renameValue = ref("");
const renameError = ref("");

function openRename(i: number) {
	renameIdx.value = i;
	renameValue.value = agents.value[i]?.name ?? "";
	renameError.value = "";
}

/** Why a name can't be used for the agent at index i, or "" when it can. */
function nameProblem(i: number, name: string): string {
	if (!name) return "Enter a name.";
	if (name.includes("/")) return "The name can't contain '/'.";
	const taken = agents.value.some(
		(a, j) => j !== i && a.name.toLowerCase() === name.toLowerCase(),
	);
	return taken ? `An agent named "${name}" already exists.` : "";
}

async function confirmRename() {
	const i = renameIdx.value;
	if (i === null) return;
	const name = renameValue.value.trim();
	renameError.value = nameProblem(i, name);
	if (renameError.value) return;
	if (name === agents.value[i]?.name) {
		renameIdx.value = null;
		return;
	}
	busy.value = true;
	try {
		await settings.renameAgentEntry(i, name);
		renameIdx.value = null;
	} catch (e) {
		renameError.value = e instanceof Error ? e.message : String(e);
	} finally {
		busy.value = false;
	}
}

const deleteIdx = ref<number | null>(null);
const deleteError = ref("");
const sessionCount = ref(0);
const deleteTarget = computed(() =>
	deleteIdx.value === null ? undefined : agents.value[deleteIdx.value],
);

function plural(n: number, one: string, many: string): string {
	return `${n} ${n === 1 ? one : many}`;
}

/** What still depends on the agent being deleted. */
const dependents = computed(() => {
	const agent = deleteTarget.value;
	if (!agent) return [];
	const out: string[] = [];
	const projects = agent.projects?.length ?? 0;
	const tasks = agent.tasks?.length ?? 0;
	if (projects) out.push(plural(projects, "owned project", "owned projects"));
	if (tasks) out.push(plural(tasks, "scheduled task", "scheduled tasks"));
	if (sessionCount.value)
		out.push(plural(sessionCount.value, "session", "sessions"));
	return out;
});

async function openDelete(i: number) {
	if (deleteBlockedReason.value) return;
	deleteIdx.value = i;
	deleteError.value = "";
	sessionCount.value = 0;
	const name = agents.value[i]?.name;
	if (!name) return;
	try {
		const raw = await callTool("session_list", { agent: name });
		const list = JSON.parse(raw);
		if (deleteIdx.value === i && Array.isArray(list))
			sessionCount.value = list.length;
	} catch {
		// The count only informs the confirmation.
	}
}

async function confirmDelete() {
	const i = deleteIdx.value;
	if (i === null) return;
	busy.value = true;
	deleteError.value = "";
	try {
		await settings.deleteAgentEntry(i);
		deleteIdx.value = null;
	} catch (e) {
		deleteError.value = e instanceof Error ? e.message : String(e);
	} finally {
		busy.value = false;
	}
}
</script>
