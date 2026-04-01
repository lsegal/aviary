<template>
				<section v-show="activeTab === 'sessions'" class="space-y-5 pb-8">
					<div class="rounded-xl border border-gray-200 bg-white p-5 dark:border-gray-800 dark:bg-gray-900">
						<h3 class="mb-4 text-sm font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">Sessions
						</h3>
						<div class="grid gap-3 lg:grid-cols-[280px_auto_auto]">
							<div>
								<label class="field-label">Agent</label>
								<select v-model="sessionAgent" class="field-input">
									<option value="">Select agent</option>
									<option v-for="agent in draft.agents" :key="`sess-${agent.name}`" :value="agent.name">{{ agent.name }}
									</option>
								</select>
							</div>
							<div class="flex items-end">
								<button type="button"
									class="rounded-lg border border-gray-200 px-3 py-2 text-xs text-gray-700 hover:bg-gray-100 dark:border-gray-700 dark:text-gray-300 dark:hover:bg-gray-800"
									:disabled="!sessionAgent || sessionLoading"
									@click="loadSessions">{{ sessionLoading ? 'Loading…' : 'Refresh Sessions' }}</button>
							</div>
							<div class="flex items-end">
								<button type="button"
									class="rounded-lg bg-blue-600 px-3 py-2 text-xs font-semibold text-white hover:bg-blue-500 disabled:opacity-50"
									:disabled="!sessionAgent || sessionLoading" @click="createSession">+ Create Session</button>
							</div>
						</div>

						<div class="mt-4 overflow-hidden rounded-lg border border-gray-200 dark:border-gray-700">
							<table v-if="sessions.length" class="w-full text-sm">
								<thead>
									<tr
										class="border-b border-gray-200 text-left text-xs font-medium text-gray-500 dark:border-gray-700 dark:text-gray-400">
										<th class="px-3 py-2">Name</th>
										<th class="px-3 py-2">ID</th>
										<th class="px-3 py-2">Updated</th>
										<th class="px-3 py-2">Actions</th>
									</tr>
								</thead>
								<tbody>
									<tr v-for="s in sessions" :key="s.id"
										class="border-b border-gray-100 text-gray-700 dark:border-gray-800 dark:text-gray-300">
										<td class="px-3 py-2">{{ s.name || '—' }}</td>
										<td class="px-3 py-2 font-mono text-xs text-gray-500 dark:text-gray-400">{{ s.id.slice(-10) }}</td>
										<td class="px-3 py-2 text-xs">{{ formatDate(s.updated_at) }}</td>
										<td class="px-3 py-2">
											<div class="flex items-center gap-2">
												<button type="button" class="danger-btn" :disabled="!s.is_processing"
													:class="!s.is_processing ? 'opacity-40 cursor-not-allowed' : ''"
													@click="stopSession(s.id)">Stop</button>
												<button type="button" class="danger-btn"
													@click="removeTarget = s; removeTargetOpen = true">Remove</button>
											</div>
										</td>
									</tr>
								</tbody>
							</table>
							<div v-else class="px-3 py-3 text-xs text-gray-500 dark:text-gray-400">No sessions found.</div>
						</div>
					</div>
				</section>
</template>

<script lang="ts">
import { defineComponent, inject } from "vue";
import { settingsViewContextKey } from "./context";

export default defineComponent({
	name: "SettingsSessionsTab",
	setup() {
		const settings = inject(settingsViewContextKey);
		if (!settings) {
			throw new Error("Settings view context is not available.");
		}
		return settings;
	},
});
</script>

